package test

// 範圍變更（CIDR／狀態／排除範圍）原子性與預覽 token 回歸測試。
//
// 驗證方式：HTTP + 真實 MariaDB（testcontainers）+ TestClock，結果以 API（網段詳情、IP 紀錄列表、
// 稽核紀錄 API）檢查；僅「通知去重鍵」沒有對外 API，才直接查 notification_states。
// 故障注入一律用獨立 MariaDB 容器內的 trigger SIGNAL／RENAME TABLE（測試自行 DDL 建立與清除），
// 產品程式沒有任何故障開關。

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"
	"github.com/jeffhou369/wurzburg/backend/internal/audit"
	"github.com/jeffhou369/wurzburg/backend/internal/auth"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/config"
	"github.com/jeffhou369/wurzburg/backend/internal/database"
	"github.com/jeffhou369/wurzburg/backend/internal/iprecords"
	"github.com/jeffhou369/wurzburg/backend/internal/server"
	"github.com/jeffhou369/wurzburg/backend/internal/subnets"
	"github.com/jeffhou369/wurzburg/backend/internal/testutil"
	"github.com/jeffhou369/wurzburg/backend/migrations"
	"github.com/testcontainers/testcontainers-go"
	mariadbmod "github.com/testcontainers/testcontainers-go/modules/mariadb"
)

// 修正前程式碼在 secret 為空時使用的公開預設金鑰，用來證明「用它偽造 token」已不可行。
const saLegacyPublicKey = "wurzburg_preview_token_hmac_default"

// ---------------------------------------------------------------------------
// 介面斷言：新介面以 runtime 斷言取用，讓同一份測試在修正前也能編譯並以斷言失敗呈現 red。
// ---------------------------------------------------------------------------

type saLockSnapshotter interface {
	LockSnapshot(ctx context.Context, tx *sql.Tx) (*subnets.Snapshot, error)
}

type saTxLogger interface {
	LogTx(ctx context.Context, tx *sql.Tx, username, userRole, ipAddress string, action, targetType, targetID, summary string, details map[string]any) error
}

type saCIDRPreviewer interface {
	HandlePreviewCIDRChange(w http.ResponseWriter, r *http.Request)
}

// saCIDRPreviewRoute 是測試專用的路由註冊器：產品的 RegisterRoutes 尚未包含 CIDR 預覽入口
// （入口契約由主執行緒整合），所以測試自行把 HandlePreviewCIDRChange 掛在建議路徑上。
type saCIDRPreviewRoute struct{ svc *subnets.ScopeService }

func (c saCIDRPreviewRoute) RegisterRoutes(api chi.Router) {
	h, ok := any(c.svc).(saCIDRPreviewer)
	if !ok {
		return // 修正前沒有此處理器：路由不存在，預覽呼叫會得到 404/405，測試以此呈現 red
	}
	api.With(auth.RequireAdmin).Post("/subnets/{id}/cidr/preview", h.HandlePreviewCIDRChange)
}

// ---------------------------------------------------------------------------
// 測試環境
// ---------------------------------------------------------------------------

type saBase struct {
	db *sql.DB
	// rootDB 以 root 身分連線，只用來讀取 information_schema.INNODB_TRX
	// （應用程式帳號沒有 PROCESS 權限），不用於任何寫入或故障注入。
	rootDB  *sql.DB
	clk     *clock.TestClock
	seeder  *testutil.Seeder
	adminID int64
	chenID  int64
	wangID  int64
	swID    int64
	counter int
}

type saEnv struct {
	*saBase
	srv      *server.Server
	svc      *subnets.ScopeService
	resolver *subnets.Resolver
	router   http.Handler
}

func saStartDB(t *testing.T) *saBase {
	t.Helper()
	if testing.Short() {
		t.Skip("略過需啟動 Docker 容器的整合測試")
	}
	ctx := context.Background()
	c, err := mariadbmod.Run(ctx, "mariadb:11.4",
		mariadbmod.WithDatabase("wurzburg"),
		mariadbmod.WithUsername("wurzburg"),
		mariadbmod.WithPassword("wurzburg_test_pw"),
	)
	if err != nil {
		t.Fatalf("啟動 MariaDB 容器失敗: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })

	dsn, err := c.ConnectionString(ctx, "parseTime=true&loc=UTC")
	if err != nil {
		t.Fatalf("取得連線字串失敗: %v", err)
	}
	db, err := connectTestDB(t, ctx, dsn)
	if err != nil {
		t.Fatalf("連線資料庫失敗: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := database.RunMigrations(db, migrations.FS, "."); err != nil {
		t.Fatalf("遷移失敗: %v", err)
	}

	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("解析連線字串失敗: %v", err)
	}
	cfg.User = "root" // 容器的 root 密碼與應用程式帳號相同（mariadb 模組行為）
	rootDB, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatalf("開啟 root 連線失敗: %v", err)
	}
	t.Cleanup(func() { _ = rootDB.Close() })

	clk := clock.NewTestClock(time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC))
	seeder := testutil.NewSeeder(t, db, clk)
	b := &saBase{db: db, rootDB: rootDB, clk: clk, seeder: seeder}
	b.adminID = seeder.SeedAdmin("admin")
	b.chenID = seeder.SeedUser("chen", "user", "ad", "chen@example.com", "陳工程師")
	b.wangID = seeder.SeedUser("wang", "user", "ad", "wang@example.com", "王工程師")
	b.swID, _ = seeder.SeedSwitch("sa-sw-01", "10.255.255.1", "ios-xe", "v3")
	return b
}

// newEnv 在同一個資料庫上建立一組獨立的 ScopeService 實例（含路由），可重複呼叫以模擬多個實例。
func (b *saBase) newEnv(t *testing.T, secret string) *saEnv {
	t.Helper()
	srv := server.New(&config.Config{Port: 8080, CampusName: "Würzburg", AppEnv: "test"}, b.db, b.clk)
	svc := subnets.NewScopeService(b.db, b.clk, srv.Audit, secret)
	srv.Mount(svc, saCIDRPreviewRoute{svc: svc}, iprecords.NewHandler(b.db, b.clk, srv.Settings, srv.Audit), audit.NewHandler(b.db))
	return &saEnv{saBase: b, srv: srv, svc: svc, resolver: subnets.NewResolver(b.db), router: apiRouter(t, srv)}
}

func (e *saEnv) do(method, path string, body any) *httptest.ResponseRecorder {
	req := testutil.NewAuthRequest(method, path, body, e.adminID, "admin", "admin")
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func saDecode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("解析回應失敗: %v；內容: %s", err, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// 測試資料：每個情境各用獨立網段 10.<20+n>.0.0/24，避免互相干擾
// ---------------------------------------------------------------------------

type saFixture struct {
	subnetID int64
	cidr     string
	base     string // 例如 10.21.0
	recIDs   map[int]int64
	macs     map[int]string
	dedup    map[int][]string // 以 IP 末段分組的通知去重鍵
}

// 固定的紀錄末段：10（範圍外、對照組）、130（已登記）、140、150。
var saAllOctets = []int{10, 130, 140, 150}

func (b *saBase) seedFixture(t *testing.T) *saFixture {
	t.Helper()
	b.counter++
	o := 20 + b.counter
	f := &saFixture{
		base:   fmt.Sprintf("10.%d.0", o),
		recIDs: map[int]int64{},
		macs:   map[int]string{},
		dedup:  map[int][]string{},
	}
	f.cidr = f.base + ".0/24"
	f.subnetID = b.seeder.SeedSubnet(f.cidr, "managed", nil)

	exp := b.clk.Now().Add(90 * 24 * time.Hour)
	for _, last := range saAllOctets {
		f.macs[last] = fmt.Sprintf("02:00:00:00:%02x:%02x", o, last)
		opts := &testutil.IPRecordSeedOpts{Presence: "online", Registration: "unregistered"}
		if last == 130 {
			uid := b.chenID
			opts = &testutil.IPRecordSeedOpts{
				Presence: "online", Registration: "registered",
				OwnerUserID: &uid, OwnerMail: "chen@example.com", ExpiresAt: &exp,
			}
		}
		f.recIDs[last] = b.seeder.SeedIPRecord(fmt.Sprintf("%s.%d", f.base, last), f.macs[last], f.subnetID, opts)
	}

	addKey := func(last int, key, typ string) {
		b.seeder.SeedNotificationState(key, typ, true)
		f.dedup[last] = append(f.dedup[last], key)
	}
	addKey(10, fmt.Sprintf("unreg|%s.10|%s", f.base, f.macs[10]), "unregistered_digest")
	addKey(140, fmt.Sprintf("unreg|%s.140|%s", f.base, f.macs[140]), "unregistered_digest")
	addKey(150, fmt.Sprintf("unreg|%s.150|%s", f.base, f.macs[150]), "unregistered_digest")
	// 已登記者留有舊 MAC 的未登記通知鍵與到期催認鍵：前綴比對必須一併清掉
	addKey(130, fmt.Sprintf("unreg|%s.130|02:00:00:00:%02x:ff", f.base, o), "unregistered_digest")
	addKey(130, fmt.Sprintf("expiry|%s.130|2027-02-10|1", f.base), "expiry_reminder")
	return f
}

func (f *saFixture) ip(last int) string { return fmt.Sprintf("%s.%d", f.base, last) }

func (f *saFixture) remainingOctets(affected []int) []int {
	gone := map[int]bool{}
	for _, a := range affected {
		gone[a] = true
	}
	var out []int
	for _, l := range saAllOctets {
		if !gone[l] {
			out = append(out, l)
		}
	}
	return out
}

func (f *saFixture) dedupFor(octets []int) []string {
	var out []string
	for _, l := range octets {
		out = append(out, f.dedup[l]...)
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// 以 API 檢查結果
// ---------------------------------------------------------------------------

func (e *saEnv) ips(t *testing.T, f *saFixture) []string {
	t.Helper()
	rec := e.do(http.MethodGet, fmt.Sprintf("/api/v1/ip-records?subnet_id=%d&page_size=200", f.subnetID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("查詢 IP 紀錄失敗: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			IPAddress string `json:"ip_address"`
		} `json:"items"`
	}
	saDecode(t, rec, &resp)
	out := make([]string, 0, len(resp.Items))
	for _, it := range resp.Items {
		out = append(out, it.IPAddress)
	}
	sort.Strings(out)
	return out
}

func (f *saFixture) ipList(octets []int) []string {
	out := make([]string, 0, len(octets))
	for _, l := range octets {
		out = append(out, f.ip(l))
	}
	sort.Strings(out)
	return out
}

func (e *saEnv) assertIPs(t *testing.T, f *saFixture, octets []int, msg string) {
	t.Helper()
	got, want := e.ips(t, f), f.ipList(octets)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s：IP 紀錄應為 %v，實際 %v", msg, want, got)
	}
}

func (e *saEnv) dedupKeys(t *testing.T, f *saFixture) []string {
	t.Helper()
	rows, err := e.db.Query(
		`SELECT dedup_key FROM notification_states WHERE dedup_key LIKE ? OR dedup_key LIKE ? ORDER BY dedup_key`,
		"unreg|"+f.base+".%", "expiry|"+f.base+".%",
	)
	if err != nil {
		t.Fatalf("查詢去重鍵失敗: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatalf("掃描去重鍵失敗: %v", err)
		}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("迭代去重鍵失敗: %v", err)
	}
	return out
}

func (e *saEnv) assertDedup(t *testing.T, f *saFixture, octets []int, msg string) {
	t.Helper()
	got, want := e.dedupKeys(t, f), f.dedupFor(octets)
	if want == nil {
		want = []string{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s：通知去重鍵應為 %v，實際 %v", msg, want, got)
	}
}

type saSubnetView struct {
	State      string           `json:"state"`
	CIDR       string           `json:"cidr"`
	IPCount    int              `json:"ip_count"`
	Exclusions []map[string]any `json:"exclusions"`
}

func (e *saEnv) subnet(t *testing.T, f *saFixture) saSubnetView {
	t.Helper()
	rec := e.do(http.MethodGet, fmt.Sprintf("/api/v1/subnets/%d", f.subnetID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("查詢網段失敗: %d %s", rec.Code, rec.Body.String())
	}
	var v saSubnetView
	saDecode(t, rec, &v)
	return v
}

type saAuditItem struct {
	Action     string         `json:"action"`
	TargetType string         `json:"target_type"`
	Summary    string         `json:"summary"`
	Details    map[string]any `json:"details_json"`
}

func (e *saEnv) audits(t *testing.T) []saAuditItem {
	t.Helper()
	var all []saAuditItem
	for page := 1; ; page++ {
		rec := e.do(http.MethodGet, fmt.Sprintf("/api/v1/audit/logs?page=%d&page_size=200", page), nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("查詢稽核紀錄失敗: %d %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Items      []saAuditItem  `json:"items"`
			Pagination map[string]int `json:"pagination"`
		}
		saDecode(t, rec, &resp)
		all = append(all, resp.Items...)
		if page >= resp.Pagination["total_pages"] {
			return all
		}
	}
}

// auditsFor 回傳明細中 subnet_id 屬於該網段的稽核紀錄（本次修正後所有範圍相關稽核都會帶 subnet_id）。
func (e *saEnv) auditsFor(t *testing.T, f *saFixture) []saAuditItem {
	t.Helper()
	var out []saAuditItem
	for _, a := range e.audits(t) {
		if v, ok := a.Details["subnet_id"].(float64); ok && int64(v) == f.subnetID {
			out = append(out, a)
		}
	}
	return out
}

func saFindAudit(items []saAuditItem, action string) *saAuditItem {
	for i := range items {
		if items[i].Action == action {
			return &items[i]
		}
	}
	return nil
}

// assertUntouched 斷言該網段完全維持建立時的樣子：沒有任何 IP／去重鍵／範圍／稽核被改動。
func (e *saEnv) assertUntouched(t *testing.T, f *saFixture, msg string) {
	t.Helper()
	e.assertIPs(t, f, saAllOctets, msg)
	e.assertDedup(t, f, saAllOctets, msg)
	v := e.subnet(t, f)
	if v.State != "managed" || v.CIDR != f.cidr || len(v.Exclusions) != 0 {
		t.Errorf("%s：網段應維持 managed／%s／無排除範圍，實際 %+v", msg, f.cidr, v)
	}
	if got := e.auditsFor(t, f); len(got) != 0 {
		t.Errorf("%s：不應留下任何該網段的稽核紀錄，實際 %+v", msg, got)
	}
}

// ---------------------------------------------------------------------------
// 三種會刪除 IP 紀錄的範圍操作
// ---------------------------------------------------------------------------

type saPreviewResp struct {
	Token    string `json:"preview_token"`
	Total    int    `json:"total_ip_records"`
	Owned    int    `json:"owned_ip_records"`
	Requires bool   `json:"requires_confirm"`
}

type saOp struct {
	name          string
	detailAction  string // 刪除明細稽核
	summaryAction string // 範圍變更摘要稽核
	okStatus      int
	affected      []int
	preview       func(e *saEnv, f *saFixture) *httptest.ResponseRecorder
	confirm       func(e *saEnv, f *saFixture, token string) *httptest.ResponseRecorder
	scopeFaultDDL func(f *saFixture) string // 讓「範圍本體寫入」失敗的 trigger
	verifyApplied func(t *testing.T, e *saEnv, f *saFixture)
}

func saOps() []saOp {
	return []saOp{
		{
			name: "exclusion", detailAction: "exclusion.create_delete", summaryAction: "exclusion.create",
			okStatus: http.StatusCreated, affected: []int{130, 140, 150},
			preview: func(e *saEnv, f *saFixture) *httptest.ResponseRecorder {
				return e.do(http.MethodPost, fmt.Sprintf("/api/v1/subnets/%d/exclusions/preview", f.subnetID),
					map[string]any{"start_ip": f.ip(100), "end_ip": f.ip(199), "reason": "dhcp"})
			},
			confirm: func(e *saEnv, f *saFixture, token string) *httptest.ResponseRecorder {
				return e.do(http.MethodPost, fmt.Sprintf("/api/v1/subnets/%d/exclusions", f.subnetID),
					map[string]any{"start_ip": f.ip(100), "end_ip": f.ip(199), "reason": "dhcp", "preview_token": token})
			},
			scopeFaultDDL: func(f *saFixture) string {
				return fmt.Sprintf(`CREATE TRIGGER sa_fault_scope BEFORE INSERT ON exclusion_ranges FOR EACH ROW
					BEGIN IF NEW.subnet_id = %d THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'fault: exclusion insert'; END IF; END`, f.subnetID)
			},
			verifyApplied: func(t *testing.T, e *saEnv, f *saFixture) {
				if v := e.subnet(t, f); len(v.Exclusions) != 1 {
					t.Errorf("應新增 1 筆排除範圍，實際 %+v", v.Exclusions)
				}
			},
		},
		{
			name: "state", detailAction: "subnet.unmanage_delete", summaryAction: "subnet.state_change",
			okStatus: http.StatusOK, affected: []int{10, 130, 140, 150},
			preview: func(e *saEnv, f *saFixture) *httptest.ResponseRecorder {
				return e.do(http.MethodPost, fmt.Sprintf("/api/v1/subnets/%d/state/preview", f.subnetID),
					map[string]any{"state": "unmanaged"})
			},
			confirm: func(e *saEnv, f *saFixture, token string) *httptest.ResponseRecorder {
				return e.do(http.MethodPut, fmt.Sprintf("/api/v1/subnets/%d/state", f.subnetID),
					map[string]any{"state": "unmanaged", "exclude_reason": "dhcp", "preview_token": token})
			},
			scopeFaultDDL: func(f *saFixture) string {
				return fmt.Sprintf(`CREATE TRIGGER sa_fault_scope BEFORE UPDATE ON subnets FOR EACH ROW
					BEGIN IF NEW.id = %d AND NEW.state = 'unmanaged' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'fault: subnet state update'; END IF; END`, f.subnetID)
			},
			verifyApplied: func(t *testing.T, e *saEnv, f *saFixture) {
				if v := e.subnet(t, f); v.State != "unmanaged" {
					t.Errorf("網段應為 unmanaged，實際 %s", v.State)
				}
			},
		},
		{
			name: "cidr", detailAction: "subnet.cidr_change_delete", summaryAction: "subnet.update",
			okStatus: http.StatusOK, affected: []int{130, 140, 150},
			preview: func(e *saEnv, f *saFixture) *httptest.ResponseRecorder {
				return e.do(http.MethodPost, fmt.Sprintf("/api/v1/subnets/%d/cidr/preview", f.subnetID),
					map[string]any{"cidr": f.base + ".0/25"})
			},
			confirm: func(e *saEnv, f *saFixture, token string) *httptest.ResponseRecorder {
				return e.do(http.MethodPut, fmt.Sprintf("/api/v1/subnets/%d", f.subnetID),
					map[string]any{"cidr": f.base + ".0/25", "description": "修正後", "preview_token": token})
			},
			scopeFaultDDL: func(f *saFixture) string {
				return fmt.Sprintf(`CREATE TRIGGER sa_fault_scope BEFORE UPDATE ON subnets FOR EACH ROW
					BEGIN IF NEW.id = %d AND NEW.cidr = '%s.0/25' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'fault: subnet cidr update'; END IF; END`, f.subnetID, f.base)
			},
			verifyApplied: func(t *testing.T, e *saEnv, f *saFixture) {
				if v := e.subnet(t, f); v.CIDR != f.base+".0/25" {
					t.Errorf("網段 CIDR 應為 %s.0/25，實際 %s", f.base, v.CIDR)
				}
			},
		},
	}
}

// mustPreview 取得預覽並檢查筆數；回傳預覽結果。
func (op saOp) mustPreview(t *testing.T, e *saEnv, f *saFixture) saPreviewResp {
	t.Helper()
	rec := op.preview(e, f)
	if rec.Code != http.StatusOK {
		t.Fatalf("[%s] 預覽應回 200，實際 %d %s", op.name, rec.Code, rec.Body.String())
	}
	var p saPreviewResp
	saDecode(t, rec, &p)
	if p.Total != len(op.affected) || p.Owned != 1 || !p.Requires || p.Token == "" {
		t.Fatalf("[%s] 預覽內容不符（應 %d 筆、其中 1 筆已登記、需確認且有 token）: %+v", op.name, len(op.affected), p)
	}
	return p
}

func saLegacyToken(payload string) string {
	mac := hmac.New(sha256.New, []byte(saLegacyPublicKey))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// ---------------------------------------------------------------------------
// 一、預覽 token
// ---------------------------------------------------------------------------

func TestScopeAtomicPreviewTokens(t *testing.T) {
	b := saStartDB(t)
	// 空金鑰：每個實例使用各自的隨機金鑰
	envA := b.newEnv(t, "")
	envB := b.newEnv(t, "")

	t.Run("成功路徑：同一交易刪除 IP、未登記與到期去重鍵並寫入明細稽核", func(t *testing.T) {
		for _, op := range saOps() {
			t.Run(op.name, func(t *testing.T) {
				f := b.seedFixture(t)
				p := op.mustPreview(t, envA, f)
				rec := op.confirm(envA, f, p.Token)
				if rec.Code != op.okStatus {
					t.Fatalf("確認應回 %d，實際 %d %s", op.okStatus, rec.Code, rec.Body.String())
				}
				remaining := f.remainingOctets(op.affected)
				envA.assertIPs(t, f, remaining, "成功後")
				envA.assertDedup(t, f, remaining, "成功後（受影響 IP 的 unreg 與 expiry 去重鍵都要清掉，範圍外的保留）")
				op.verifyApplied(t, envA, f)

				audits := envA.auditsFor(t, f)
				detail := saFindAudit(audits, op.detailAction)
				if detail == nil {
					t.Fatalf("缺少明細稽核 %s；實際 %+v", op.detailAction, audits)
				}
				if detail.Details["deleted_count"] != float64(len(op.affected)) || detail.Details["owned_deleted_count"] != float64(1) {
					t.Errorf("明細稽核筆數不符: %+v", detail.Details)
				}
				raw, _ := json.Marshal(detail.Details["owned_deleted_details"])
				if !strings.Contains(string(raw), f.ip(130)) || !strings.Contains(string(raw), "chen@example.com") || !strings.Contains(string(raw), "chen") {
					t.Errorf("明細稽核缺少已登記者的 IP／擁有者／信箱: %s", raw)
				}
				if saFindAudit(audits, op.summaryAction) == nil {
					t.Errorf("缺少摘要稽核 %s；實際 %+v", op.summaryAction, audits)
				}
			})
		}
	})

	t.Run("舊公開預設金鑰偽造的 token 被拒絕", func(t *testing.T) {
		forge := map[string]func(f *saFixture, op saOp) string{
			"exclusion": func(f *saFixture, op saOp) string {
				return saLegacyToken(fmt.Sprintf("excl:%d:%s-%s:%d:%d", f.subnetID, f.ip(100), f.ip(199), 3, 1))
			},
			"state": func(f *saFixture, op saOp) string {
				return saLegacyToken(fmt.Sprintf("state:%d:unmanaged:%d:%d", f.subnetID, 4, 1))
			},
			"cidr": func(f *saFixture, op saOp) string {
				return saLegacyToken(fmt.Sprintf("cidr:%d:%s.0/25:%d:%d", f.subnetID, f.base, 3, 1))
			},
		}
		for _, op := range saOps() {
			t.Run(op.name, func(t *testing.T) {
				f := b.seedFixture(t)
				rec := op.confirm(envA, f, forge[op.name](f, op))
				if rec.Code != http.StatusConflict {
					t.Errorf("以舊公開金鑰偽造的 token 應回 409，實際 %d %s", rec.Code, rec.Body.String())
				}
				envA.assertUntouched(t, f, "偽造 token 被拒後")

				// 對照：同一環境的真實預覽 token 仍可使用，證明拒絕的是偽造而非環境壞掉
				p := op.mustPreview(t, envA, f)
				if rec := op.confirm(envA, f, p.Token); rec.Code != op.okStatus {
					t.Errorf("真實 token 應回 %d，實際 %d %s", op.okStatus, rec.Code, rec.Body.String())
				}
			})
		}
	})

	t.Run("跨實例預覽失效；明確設定同一金鑰的實例互通", func(t *testing.T) {
		for _, op := range saOps() {
			t.Run(op.name+"/空金鑰各實例獨立", func(t *testing.T) {
				f := b.seedFixture(t)
				p := op.mustPreview(t, envA, f)
				if rec := op.confirm(envB, f, p.Token); rec.Code != http.StatusConflict {
					t.Errorf("實例 B 不應接受實例 A 的預覽 token，實際 %d %s", rec.Code, rec.Body.String())
				}
				envA.assertUntouched(t, f, "跨實例被拒後")
				if rec := op.confirm(envA, f, p.Token); rec.Code != op.okStatus {
					t.Errorf("原實例仍應接受自己的 token，實際 %d %s", rec.Code, rec.Body.String())
				}
			})
		}
		t.Run("明確設定相同金鑰", func(t *testing.T) {
			envC := b.newEnv(t, "sa-shared-secret-for-two-instances")
			envD := b.newEnv(t, "sa-shared-secret-for-two-instances")
			op := saOps()[0]
			f := b.seedFixture(t)
			p := op.mustPreview(t, envC, f)
			if rec := op.confirm(envD, f, p.Token); rec.Code != op.okStatus {
				t.Errorf("同金鑰的另一實例應接受，實際 %d %s", rec.Code, rec.Body.String())
			}
		})
	})

	// 筆數相同、內容被換掉：token 必須綁定逐筆內容而非筆數
	swaps := []struct {
		name  string
		apply func(t *testing.T, b *saBase, f *saFixture)
	}{
		{"刪一筆再新增另一筆 IP", func(t *testing.T, b *saBase, f *saFixture) {
			if _, err := b.db.Exec(`DELETE FROM ip_records WHERE id = ?`, f.recIDs[150]); err != nil {
				t.Fatalf("換掉內容失敗: %v", err)
			}
			b.seeder.SeedIPRecord(f.ip(160), "02:00:00:00:ee:a0", f.subnetID, nil)
			delete(f.recIDs, 150)
		}},
		{"已登記者換擁有者", func(t *testing.T, b *saBase, f *saFixture) {
			if _, err := b.db.Exec(`UPDATE ip_records SET owner_user_id = ?, owner_mail = 'wang@example.com' WHERE id = ?`, b.wangID, f.recIDs[130]); err != nil {
				t.Fatalf("換掉內容失敗: %v", err)
			}
		}},
		{"擁有者信箱變更", func(t *testing.T, b *saBase, f *saFixture) {
			if _, err := b.db.Exec(`UPDATE ip_records SET owner_mail = 'other@example.com' WHERE id = ?`, f.recIDs[130]); err != nil {
				t.Fatalf("換掉內容失敗: %v", err)
			}
		}},
		{"MAC 變更", func(t *testing.T, b *saBase, f *saFixture) {
			if _, err := b.db.Exec(`UPDATE ip_records SET mac_address = '02:00:00:00:ee:b1' WHERE id = ?`, f.recIDs[140]); err != nil {
				t.Fatalf("換掉內容失敗: %v", err)
			}
		}},
		{"到期日變更", func(t *testing.T, b *saBase, f *saFixture) {
			if _, err := b.db.Exec(`UPDATE ip_records SET expires_at = ? WHERE id = ?`, b.clk.Now().Add(400*24*time.Hour), f.recIDs[130]); err != nil {
				t.Fatalf("換掉內容失敗: %v", err)
			}
		}},
	}
	t.Run("筆數相同但內容被換掉時 token 被拒絕", func(t *testing.T) {
		for _, op := range saOps() {
			for _, sw := range swaps {
				t.Run(op.name+"/"+sw.name, func(t *testing.T) {
					f := b.seedFixture(t)
					p := op.mustPreview(t, envA, f)
					sw.apply(t, b, f)
					ipsBefore := envA.ips(t, f)
					dedupBefore := envA.dedupKeys(t, f)

					rec := op.confirm(envA, f, p.Token)
					if rec.Code != http.StatusConflict {
						t.Errorf("預覽後內容已被換掉，確認應回 409，實際 %d %s", rec.Code, rec.Body.String())
					}
					if got := envA.ips(t, f); !reflect.DeepEqual(got, ipsBefore) {
						t.Errorf("被拒後 IP 紀錄不應變動：前 %v 後 %v", ipsBefore, got)
					}
					if got := envA.dedupKeys(t, f); !reflect.DeepEqual(got, dedupBefore) {
						t.Errorf("被拒後去重鍵不應變動：前 %v 後 %v", dedupBefore, got)
					}
					v := envA.subnet(t, f)
					if v.State != "managed" || v.CIDR != f.cidr || len(v.Exclusions) != 0 {
						t.Errorf("被拒後網段不應變動，實際 %+v", v)
					}

					// 重新預覽後用新 token 可以完成，證明拒絕的原因是內容已變
					p2 := op.mustPreview(t, envA, f)
					if rec := op.confirm(envA, f, p2.Token); rec.Code != op.okStatus {
						t.Errorf("重新預覽後應成功，實際 %d %s", rec.Code, rec.Body.String())
					}
				})
			}
		}
	})

	t.Run("token 綁定完整操作：換範圍、換原因、換目標 CIDR、跨操作都被拒絕", func(t *testing.T) {
		ops := saOps()
		excl, cidr := ops[0], ops[2]

		f := b.seedFixture(t)
		pe := excl.mustPreview(t, envA, f)
		cases := []struct {
			name string
			rec  func() *httptest.ResponseRecorder
		}{
			{"排除範圍結束位址不同（受影響集合相同）", func() *httptest.ResponseRecorder {
				return envA.do(http.MethodPost, fmt.Sprintf("/api/v1/subnets/%d/exclusions", f.subnetID),
					map[string]any{"start_ip": f.ip(100), "end_ip": f.ip(198), "reason": "dhcp", "preview_token": pe.Token})
			}},
			{"排除原因不同", func() *httptest.ResponseRecorder {
				return envA.do(http.MethodPost, fmt.Sprintf("/api/v1/subnets/%d/exclusions", f.subnetID),
					map[string]any{"start_ip": f.ip(100), "end_ip": f.ip(199), "reason": "wireless", "preview_token": pe.Token})
			}},
			{"排除範圍 token 拿去修正 CIDR（受影響集合相同）", func() *httptest.ResponseRecorder {
				return cidr.confirm(envA, f, pe.Token)
			}},
		}
		for _, c := range cases {
			if rec := c.rec(); rec.Code != http.StatusConflict {
				t.Errorf("%s：應回 409，實際 %d %s", c.name, rec.Code, rec.Body.String())
			}
		}
		envA.assertUntouched(t, f, "操作不符被拒後")

		pc := cidr.mustPreview(t, envA, f)
		if rec := envA.do(http.MethodPut, fmt.Sprintf("/api/v1/subnets/%d", f.subnetID),
			map[string]any{"cidr": f.base + ".0/26", "description": "x", "preview_token": pc.Token}); rec.Code != http.StatusConflict {
			t.Errorf("換目標 CIDR（/25 的 token 用於 /26）：應回 409，實際 %d %s", rec.Code, rec.Body.String())
		}
		if rec := excl.confirm(envA, f, pc.Token); rec.Code != http.StatusConflict {
			t.Errorf("CIDR token 拿去新增排除範圍：應回 409，實際 %d %s", rec.Code, rec.Body.String())
		}
		envA.assertUntouched(t, f, "CIDR 操作不符被拒後")
	})
}

// ---------------------------------------------------------------------------
// 二、LockSnapshot 與 LogTx
// ---------------------------------------------------------------------------

// saWaitLockWaits 等到至少 want 個交易處於 InnoDB 列鎖等待。
//
// 以 SHOW ENGINE INNODB STATUS 中「TRX HAS BEEN WAITING」的段落計數為準：MariaDB 11.4.13 在原生
// amd64／arm64 上，information_schema.INNODB_TRX 的 trx_state 與 INNODB_LOCK_WAITS 會漏報正在等待的
// 交易（INNODB STATUS 已顯示 LOCK WAIT，視圖仍為 RUNNING／0 筆），只在模擬執行等較慢的環境碰巧正確。
// 兩者取較大值，避免任一來源漏報造成誤判。
func saWaitLockWaits(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	last := ""
	for time.Now().Before(deadline) {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.INNODB_TRX WHERE trx_state = 'LOCK WAIT'`).Scan(&n); err != nil {
			t.Fatalf("查詢鎖等待失敗: %v", err)
		}
		var typ, name, status string
		if err := db.QueryRow(`SHOW ENGINE INNODB STATUS`).Scan(&typ, &name, &status); err != nil {
			t.Fatalf("查詢 InnoDB 狀態失敗: %v", err)
		}
		last = status
		if m := strings.Count(status, "TRX HAS BEEN WAITING"); m > n {
			n = m
		}
		if n >= want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Logf("最後一次 InnoDB 狀態：\n%s", last)
	t.Fatalf("等不到 %d 個鎖等待中的交易", want)
}

type saAsyncResult struct {
	done chan struct{}
	rec  *httptest.ResponseRecorder
}

func (e *saEnv) async(method, path string, body any) *saAsyncResult {
	r := &saAsyncResult{done: make(chan struct{})}
	go func() {
		defer close(r.done)
		r.rec = e.do(method, path, body)
	}()
	return r
}

func (r *saAsyncResult) finished() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

func (r *saAsyncResult) wait(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	select {
	case <-r.done:
		return r.rec
	case <-time.After(30 * time.Second):
		t.Fatal("等待非同步請求逾時")
		return nil
	}
}

func saMustLocker(t *testing.T, r *subnets.Resolver) saLockSnapshotter {
	t.Helper()
	l, ok := any(r).(saLockSnapshotter)
	if !ok {
		t.Fatal("Resolver 尚未提供 LockSnapshot(ctx, tx) (*Snapshot, error)")
	}
	return l
}

func TestScopeLockSnapshotAndLogTx(t *testing.T) {
	b := saStartDB(t)
	e := b.newEnv(t, "")
	ctx := context.Background()

	t.Run("鎖定當下讀到最新已提交的閘道與排除範圍（不被先前的一致性讀取視圖卡住）", func(t *testing.T) {
		locker := saMustLocker(t, e.resolver)
		f := b.seedFixture(t)

		tx, err := b.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("開啟交易失敗: %v", err)
		}
		defer func() { _ = tx.Rollback() }()
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM exclusion_ranges`).Scan(&n); err != nil { // 建立 REPEATABLE READ 視圖
			t.Fatalf("建立讀取視圖失敗: %v", err)
		}
		// 另一條連線在視圖建立後提交了新的排除範圍與閘道
		b.seeder.SeedExclusionRange(f.subnetID, f.ip(100), f.ip(199), "dhcp")
		b.seeder.SeedGateway(f.subnetID, f.ip(1), "Vlan1", b.swID)

		snap, err := locker.LockSnapshot(ctx, tx)
		if err != nil {
			t.Fatalf("LockSnapshot 失敗: %v", err)
		}
		if r, err := snap.ResolveIP(f.ip(130)); err != nil || !r.IsExcluded || r.ShouldHaveRecord {
			t.Errorf("快照應包含剛提交的排除範圍：%+v err=%v", r, err)
		}
		if r, err := snap.ResolveIP(f.ip(1)); err != nil || !r.IsGateway {
			t.Errorf("快照應包含剛提交的閘道：%+v err=%v", r, err)
		}
		if r, err := snap.ResolveIP(f.ip(10)); err != nil || r.SubnetID != f.subnetID || !r.ShouldHaveRecord {
			t.Errorf("範圍外的位址仍應屬於納管網段：%+v err=%v", r, err)
		}
	})

	t.Run("持鎖期間的範圍變更、新增網段（含重疊 CIDR）都等到交易結束才執行", func(t *testing.T) {
		locker := saMustLocker(t, e.resolver)
		f := b.seedFixture(t)

		tx, err := b.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("開啟交易失敗: %v", err)
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := locker.LockSnapshot(ctx, tx); err != nil {
			t.Fatalf("LockSnapshot 失敗: %v", err)
		}

		// 同 CIDR、僅改描述：必定會 UPDATE subnets（不需預覽確認），能穩定觀察到鎖等待
		stateReq := e.async(http.MethodPut, fmt.Sprintf("/api/v1/subnets/%d", f.subnetID), map[string]any{"cidr": f.cidr, "description": "鎖測試"})
		overlapReq := e.async(http.MethodPost, "/api/v1/subnets", map[string]any{"cidr": f.base + ".128/25", "description": "重疊網段"})
		saWaitLockWaits(t, b.rootDB, 2)
		if stateReq.finished() || overlapReq.finished() {
			t.Fatal("持鎖期間範圍變更與新增重疊網段不應完成")
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("提交失敗: %v", err)
		}
		if rec := stateReq.wait(t); rec.Code != http.StatusOK {
			t.Errorf("鎖釋放後狀態變更應成功，實際 %d %s", rec.Code, rec.Body.String())
		}
		if rec := overlapReq.wait(t); rec.Code != http.StatusCreated {
			t.Errorf("鎖釋放後新增網段應成功，實際 %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("nil 交易回傳錯誤", func(t *testing.T) {
		locker := saMustLocker(t, e.resolver)
		if snap, err := locker.LockSnapshot(ctx, nil); err == nil || snap != nil {
			t.Errorf("nil 交易應回傳錯誤且不回傳快照，實際 snap=%v err=%v", snap, err)
		}
	})

	t.Run("LogTx 隨交易回滾，提交後才出現，且遮蔽機敏欄位", func(t *testing.T) {
		logger, ok := any(e.srv.Audit).(saTxLogger)
		if !ok {
			t.Fatal("audit.Logger 尚未提供 LogTx(ctx, tx, ...) error")
		}
		details := map[string]any{
			"password":  "p@ss",
			"keep":      "visible",
			"nested":    map[string]any{"snmp_community": "public", "note": "ok"},
			"api_token": "tok",
		}

		tx, err := b.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("開啟交易失敗: %v", err)
		}
		if err := logger.LogTx(ctx, tx, "admin", "admin", "127.0.0.1", "sa.logtx.rollback", "test", "1", "應被回滾", details); err != nil {
			t.Fatalf("LogTx 失敗: %v", err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("回滾失敗: %v", err)
		}
		if found := saFindAudit(e.audits(t), "sa.logtx.rollback"); found != nil {
			t.Errorf("回滾後不應留下稽核紀錄: %+v", found)
		}

		tx, err = b.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("開啟交易失敗: %v", err)
		}
		if err := logger.LogTx(ctx, tx, "admin", "admin", "127.0.0.1", "sa.logtx.commit", "test", "2", "應被保留", details); err != nil {
			t.Fatalf("LogTx 失敗: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("提交失敗: %v", err)
		}
		got := saFindAudit(e.audits(t), "sa.logtx.commit")
		if got == nil {
			t.Fatal("提交後應能查到稽核紀錄")
		}
		if got.Details["password"] != "[REDACTED]" || got.Details["api_token"] != "[REDACTED]" || got.Details["keep"] != "visible" {
			t.Errorf("機敏欄位遮蔽不正確: %+v", got.Details)
		}
		nested, _ := got.Details["nested"].(map[string]any)
		if nested["snmp_community"] != "[REDACTED]" || nested["note"] != "ok" {
			t.Errorf("巢狀機敏欄位遮蔽不正確: %+v", got.Details["nested"])
		}

		// 稽核寫入失敗必須回傳錯誤（讓呼叫端回滾），而不是吞掉
		tx, err = b.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("開啟交易失敗: %v", err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("回滾失敗: %v", err)
		}
		if err := logger.LogTx(ctx, tx, "admin", "admin", "127.0.0.1", "sa.logtx.closed", "test", "3", "交易已結束", nil); err == nil {
			t.Error("交易已結束時 LogTx 應回傳錯誤")
		}
	})
}

// ---------------------------------------------------------------------------
// 三、故障注入：任一步失敗，IP／去重／範圍／稽核全部回滾
// ---------------------------------------------------------------------------

// installTrigger 建立故障 trigger，回傳可提前移除它的函式；測試結束一定會移除。
func (b *saBase) installTrigger(t *testing.T, name, ddl string) func() {
	t.Helper()
	if _, err := b.db.Exec(ddl); err != nil {
		t.Fatalf("建立故障 trigger 失敗: %v", err)
	}
	dropped := false
	drop := func() {
		if dropped {
			return
		}
		dropped = true
		if _, err := b.db.Exec("DROP TRIGGER IF EXISTS " + name); err != nil {
			t.Errorf("移除故障 trigger 失敗: %v", err)
		}
	}
	t.Cleanup(drop)
	return drop
}

func saAuditFaultDDL(action string) string {
	return fmt.Sprintf(`CREATE TRIGGER sa_fault_audit BEFORE INSERT ON audit_logs FOR EACH ROW
		BEGIN IF NEW.action = '%s' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'fault: audit insert'; END IF; END`, action)
}

func TestScopeAtomicRollbackOnFailure(t *testing.T) {
	b := saStartDB(t)
	e := b.newEnv(t, "")
	ctx := context.Background()

	// 空的 subnets 表：LockSnapshot 的全表鎖也要擋住「尚無任何網段」時的併發新增
	t.Run("空範圍表也受保護", func(t *testing.T) {
		locker := saMustLocker(t, e.resolver)
		tx, err := b.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("開啟交易失敗: %v", err)
		}
		defer func() { _ = tx.Rollback() }()
		snap, err := locker.LockSnapshot(ctx, tx)
		if err != nil || snap == nil {
			t.Fatalf("空表 LockSnapshot 應成功並回傳空快照: snap=%v err=%v", snap, err)
		}
		create := e.async(http.MethodPost, "/api/v1/subnets", map[string]any{"cidr": "172.30.0.0/24"})
		saWaitLockWaits(t, b.rootDB, 1)
		if create.finished() {
			t.Fatal("持鎖期間新增網段不應完成")
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("提交失敗: %v", err)
		}
		if rec := create.wait(t); rec.Code != http.StatusCreated {
			t.Errorf("鎖釋放後新增網段應成功，實際 %d %s", rec.Code, rec.Body.String())
		}
	})

	faults := []struct {
		name  string
		ddl   func(op saOp, f *saFixture) string
		name2 string
		drop  string
	}{
		{"範圍本體寫入失敗", func(op saOp, f *saFixture) string { return op.scopeFaultDDL(f) }, "範圍本體", "sa_fault_scope"},
		{"明細稽核寫入失敗", func(op saOp, f *saFixture) string { return saAuditFaultDDL(op.detailAction) }, "明細稽核", "sa_fault_audit"},
		{"摘要稽核寫入失敗", func(op saOp, f *saFixture) string { return saAuditFaultDDL(op.summaryAction) }, "摘要稽核", "sa_fault_audit"},
	}
	for _, op := range saOps() {
		for _, fault := range faults {
			t.Run(op.name+"/"+fault.name, func(t *testing.T) {
				f := b.seedFixture(t)
				p := op.mustPreview(t, e, f)
				drop := b.installTrigger(t, fault.drop, fault.ddl(op, f))

				rec := op.confirm(e, f, p.Token)
				if rec.Code != http.StatusInternalServerError {
					t.Errorf("%s時應回 500，實際 %d %s", fault.name, rec.Code, rec.Body.String())
				}
				e.assertUntouched(t, f, fault.name+"後")

				// 故障排除後，同一個 token 仍有效（資料沒被動過），重試即可完成
				drop()
				if rec := op.confirm(e, f, p.Token); rec.Code != op.okStatus {
					t.Fatalf("排除故障後以同一 token 重試應回 %d，實際 %d %s", op.okStatus, rec.Code, rec.Body.String())
				}
				remaining := f.remainingOctets(op.affected)
				e.assertIPs(t, f, remaining, "重試成功後")
				e.assertDedup(t, f, remaining, "重試成功後")
				op.verifyApplied(t, e, f)
			})
		}
	}

	t.Run("查詢錯誤不得被當成空集合", func(t *testing.T) {
		for _, op := range saOps() {
			t.Run(op.name, func(t *testing.T) {
				f := b.seedFixture(t)
				p := op.mustPreview(t, e, f)

				// 讓「擁有者帳號」查詢失敗（IP 紀錄的 FK 會跟著表名，不影響資料）
				if _, err := b.db.Exec(`RENAME TABLE users TO users_sa_hidden`); err != nil {
					t.Fatalf("製造查詢錯誤失敗: %v", err)
				}
				restored := false
				restore := func() {
					if restored {
						return
					}
					restored = true
					if _, err := b.db.Exec(`RENAME TABLE users_sa_hidden TO users`); err != nil {
						t.Errorf("還原 users 表失敗: %v", err)
					}
				}
				defer restore()

				if rec := op.preview(e, f); rec.Code != http.StatusInternalServerError {
					t.Errorf("查詢失敗時預覽應回 500（不可當成 0 筆），實際 %d %s", rec.Code, rec.Body.String())
				}
				if rec := op.confirm(e, f, p.Token); rec.Code != http.StatusInternalServerError {
					t.Errorf("查詢失敗時確認應回 500（不可當成 0 筆而直接變更範圍），實際 %d %s", rec.Code, rec.Body.String())
				}
				restore()
				e.assertUntouched(t, f, "查詢失敗後")
			})
		}
	})

	t.Run("LockSnapshot 遇到查詢錯誤或無法解析的資料時回傳錯誤", func(t *testing.T) {
		locker := saMustLocker(t, e.resolver)

		t.Run("閘道表查詢失敗", func(t *testing.T) {
			if _, err := b.db.Exec(`RENAME TABLE subnet_gateways TO subnet_gateways_sa_hidden`); err != nil {
				t.Fatalf("製造查詢錯誤失敗: %v", err)
			}
			defer func() {
				if _, err := b.db.Exec(`RENAME TABLE subnet_gateways_sa_hidden TO subnet_gateways`); err != nil {
					t.Errorf("還原 subnet_gateways 失敗: %v", err)
				}
			}()
			tx, err := b.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("開啟交易失敗: %v", err)
			}
			defer func() { _ = tx.Rollback() }()
			if snap, err := locker.LockSnapshot(ctx, tx); err == nil || snap != nil {
				t.Errorf("查詢失敗應回傳錯誤且不回傳快照，實際 snap=%v err=%v", snap, err)
			}
		})

		t.Run("資料庫內有無法解析的 CIDR", func(t *testing.T) {
			res, err := b.db.Exec(`INSERT INTO subnets (cidr, state, is_discovered, source_missing, created_at, updated_at) VALUES ('not-a-cidr', 'managed', FALSE, FALSE, NOW(), NOW())`)
			if err != nil {
				t.Fatalf("植入壞資料失敗: %v", err)
			}
			badID, _ := res.LastInsertId()
			defer func() { _, _ = b.db.Exec(`DELETE FROM subnets WHERE id = ?`, badID) }()

			tx, err := b.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("開啟交易失敗: %v", err)
			}
			defer func() { _ = tx.Rollback() }()
			if snap, err := locker.LockSnapshot(ctx, tx); err == nil || snap != nil {
				t.Errorf("無法解析的 CIDR 應回傳錯誤（fail-closed），實際 snap=%v err=%v", snap, err)
			}
			if snap, err := e.resolver.BuildSnapshot(ctx); err == nil || snap != nil {
				t.Errorf("BuildSnapshot 遇到無法解析的 CIDR 也應回傳錯誤，實際 snap=%v err=%v", snap, err)
			}
		})
	})
}
