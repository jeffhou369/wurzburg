package test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/config"
	"github.com/jeffhou369/wurzburg/backend/internal/conflicts"
	"github.com/jeffhou369/wurzburg/backend/internal/database"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
	"github.com/jeffhou369/wurzburg/backend/internal/iprecords"
	"github.com/jeffhou369/wurzburg/backend/internal/reconciliation"
	"github.com/jeffhou369/wurzburg/backend/internal/server"
	"github.com/jeffhou369/wurzburg/backend/internal/subnets"
	"github.com/jeffhou369/wurzburg/backend/internal/testutil"
	"github.com/jeffhou369/wurzburg/backend/migrations"
	"github.com/testcontainers/testcontainers-go"
	mariadbmod "github.com/testcontainers/testcontainers-go/modules/mariadb"
)

// 本檔驗證開發規格 3.6：每輪先彙整同一位址的 MAC 集合，再逐 IP 決定採信與衝突。
// 接縫為 HTTP + MariaDB（Testcontainers）+ TestClock + 一輪觸發（ReconcileObservations）；
// 所有斷言只透過 GET /ip-records、/ip-records/{id}、/conflicts，不直接查詢資料庫。

const (
	rrMAC1 = "02:00:00:00:00:01"
	rrMAC2 = "02:00:00:00:00:02"
)

// rrRig 是整組回歸測試共用的 HTTP + 資料庫環境。
type rrRig struct {
	t       *testing.T
	ctx     context.Context
	db      *sql.DB // 僅供佈建測試資料（網段、交換器、使用者），不做斷言
	clk     *clock.TestClock
	svc     *reconciliation.Service
	router  http.Handler
	seeder  *testutil.Seeder
	adminID int64
	userID  int64
}

// rrSource 代表一個讀取來源（一台交換器的預設 VRF 讀取設定）。
type rrSource struct {
	vrfID int64
	swID  int64
	name  string
}

type rrSighting struct {
	SwitchName string `json:"switch_name"`
	MAC        string `json:"mac_address"`
}

type rrEvent struct {
	Type    string `json:"event_type"`
	Details string `json:"details"`
}

// rrIP 對應 GET /ip-records/{id} 的回應欄位。
type rrIP struct {
	ID            int64        `json:"id"`
	IPAddress     string       `json:"ip_address"`
	MAC           string       `json:"mac_address"`
	Presence      string       `json:"presence"`
	Registration  string       `json:"registration"`
	HasConflict   bool         `json:"has_conflict"`
	OwnerUsername string       `json:"owner_username"`
	Sightings     []rrSighting `json:"sightings"`
	Events        []rrEvent    `json:"events"`
}

func (r rrIP) countEvents(eventType string) int {
	n := 0
	for _, e := range r.Events {
		if e.Type == eventType {
			n++
		}
	}
	return n
}

// rrConflict 對應 GET /conflicts 的項目。
type rrConflict struct {
	ID         int64    `json:"id"`
	IPAddress  string   `json:"ip_address"`
	AdoptedMAC string   `json:"adopted_mac"`
	MACs       []string `json:"conflict_macs"`
	Status     string   `json:"status"`
	Resolution *string  `json:"resolution"`
}

func newRRRig(t *testing.T) *rrRig {
	t.Helper()
	if testing.Short() {
		t.Skip("略過需啟動 Docker 容器的整合測試")
	}

	ctx := context.Background()
	container, err := mariadbmod.Run(ctx,
		"mariadb:11.4",
		mariadbmod.WithDatabase("wurzburg"),
		mariadbmod.WithUsername("wurzburg"),
		mariadbmod.WithPassword("wurzburg_test_pw"),
	)
	if err != nil {
		t.Fatalf("啟動 MariaDB 容器失敗: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })

	dsn, err := container.ConnectionString(ctx, "parseTime=true&loc=UTC")
	if err != nil {
		t.Fatalf("取得連線字串失敗: %v", err)
	}
	db, err := database.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("連線資料庫失敗: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := database.RunMigrations(db, migrations.FS, "."); err != nil {
		t.Fatalf("遷移失敗: %v", err)
	}

	clk := clock.NewTestClock(time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC))
	srv := server.New(&config.Config{Port: 8080, CampusName: "Würzburg", AppEnv: "test"}, db, clk)
	scopeSvc := subnets.NewScopeService(db, clk, srv.Audit, "test_token_secret_32bytes_length!!")
	resolver := subnets.NewResolver(db)
	svc := reconciliation.NewService(db, clk, srv.Settings, resolver, interfaces.NewFakeOUILookup())
	srv.Mount(scopeSvc, conflicts.NewHandler(db, clk, srv.Audit, interfaces.NewFakeOUILookup()), iprecords.NewHandler(db, clk, srv.Settings, srv.Audit))

	seeder := testutil.NewSeeder(t, db, clk)
	return &rrRig{
		t:       t,
		ctx:     ctx,
		db:      db,
		clk:     clk,
		svc:     svc,
		router:  srv.Router(),
		seeder:  seeder,
		adminID: seeder.SeedAdmin("admin"),
		userID:  seeder.SeedUser("chen", "user", "ad", "chen@example.com", "陳工程師"),
	}
}

// with 回傳綁定到子測試 t 的副本，讓輔助函式的失敗只中止該子測試，不波及其他情境。
func (r *rrRig) with(t *testing.T) *rrRig {
	c := *r
	c.t = t
	return &c
}

func (r *rrRig) newSource(name string) rrSource {
	r.t.Helper()
	swID, vrfID := r.seeder.SeedSwitch(name, fmt.Sprintf("10.255.0.%d", r.nextSeq()), "ios-xe", "v3")
	return rrSource{vrfID: vrfID, swID: swID, name: name}
}

var rrSeq int

func (r *rrRig) nextSeq() int {
	rrSeq++
	return rrSeq
}

// obs 組出某來源本輪的輸出；每個 ip/mac 成對給定。
func (r *rrRig) obs(src rrSource, pairs ...string) *interfaces.PollOutput {
	r.t.Helper()
	if len(pairs)%2 != 0 {
		r.t.Fatalf("obs 參數必須成對 (ip, mac)")
	}
	out := &interfaces.PollOutput{VRFConfigID: src.vrfID, SwitchID: src.swID, VRFName: "default"}
	for i := 0; i < len(pairs); i += 2 {
		out.ARPObservations = append(out.ARPObservations, interfaces.ARPObservation{
			IPAddress: pairs[i], MACAddress: pairs[i+1], IfIndex: 1, IfName: "Vlan10", ARPType: "dynamic",
		})
	}
	return out
}

// round 觸發一輪整併，成功的輸出依給定順序傳入，failed 為讀取失敗的 VRF；結束後時鐘前進 3 分鐘。
func (r *rrRig) round(outs []*interfaces.PollOutput, failed ...rrSource) {
	r.t.Helper()
	var failedIDs []int64
	for _, f := range failed {
		failedIDs = append(failedIDs, f.vrfID)
	}
	if err := r.svc.ReconcileObservations(r.ctx, outs, failedIDs); err != nil {
		r.t.Fatalf("整併一輪失敗: %v", err)
	}
	r.clk.Advance(3 * time.Minute)
}

func (r *rrRig) do(method, path string, body any, userID int64, role, username string) *httptest.ResponseRecorder {
	r.t.Helper()
	rec := httptest.NewRecorder()
	r.router.ServeHTTP(rec, testutil.NewAuthRequest(method, path, body, userID, role, username))
	return rec
}

func (r *rrRig) asAdmin(method, path string, body any) *httptest.ResponseRecorder {
	r.t.Helper()
	return r.do(method, path, body, r.adminID, "admin", "admin")
}

func (r *rrRig) listIPs(ip string) []map[string]any {
	r.t.Helper()
	rec := r.asAdmin(http.MethodGet, "/api/v1/ip-records?page_size=200&search="+url.QueryEscape(ip), nil)
	if rec.Code != http.StatusOK {
		r.t.Fatalf("查詢 IP 紀錄清單失敗: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		r.t.Fatalf("解析 IP 紀錄清單失敗: %v", err)
	}
	var exact []map[string]any
	for _, it := range resp.Items {
		if it["ip_address"] == ip {
			exact = append(exact, it)
		}
	}
	return exact
}

// ip 取得單一位址的詳情；位址不存在時使測試失敗。
func (r *rrRig) ip(addr string) rrIP {
	r.t.Helper()
	items := r.listIPs(addr)
	if len(items) != 1 {
		r.t.Fatalf("位址 %s 應存在 1 筆 IP 紀錄，實際 %d 筆", addr, len(items))
	}
	id := int64(items[0]["id"].(float64))
	rec := r.asAdmin(http.MethodGet, fmt.Sprintf("/api/v1/ip-records/%d", id), nil)
	if rec.Code != http.StatusOK {
		r.t.Fatalf("取得 IP 詳情失敗: %d %s", rec.Code, rec.Body.String())
	}
	var detail rrIP
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		r.t.Fatalf("解析 IP 詳情失敗: %v", err)
	}
	return detail
}

// conflictsFor 取得某位址的全部衝突紀錄（含已解消的歷史）。
func (r *rrRig) conflictsFor(addr string) []rrConflict {
	r.t.Helper()
	rec := r.asAdmin(http.MethodGet, "/api/v1/conflicts?page_size=200", nil)
	if rec.Code != http.StatusOK {
		r.t.Fatalf("查詢衝突清單失敗: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []rrConflict `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		r.t.Fatalf("解析衝突清單失敗: %v", err)
	}
	var out []rrConflict
	for _, c := range resp.Items {
		if c.IPAddress == addr {
			out = append(out, c)
		}
	}
	return out
}

// activeConflict 回傳某位址目前唯一的 open／in_progress 衝突；沒有則回傳 nil，多於一筆使測試失敗。
func (r *rrRig) activeConflict(addr string) *rrConflict {
	r.t.Helper()
	var active []rrConflict
	for _, c := range r.conflictsFor(addr) {
		if c.Status == "open" || c.Status == "in_progress" {
			active = append(active, c)
		}
	}
	if len(active) > 1 {
		r.t.Fatalf("位址 %s 同時只應有一筆未解消衝突，實際 %d 筆: %+v", addr, len(active), active)
	}
	if len(active) == 0 {
		return nil
	}
	return &active[0]
}

func (r *rrRig) wantActiveConflict(addr, status, adopted string, macs ...string) {
	r.t.Helper()
	c := r.activeConflict(addr)
	if c == nil {
		r.t.Fatalf("位址 %s 應有未解消衝突 (%s)，實際沒有: %+v", addr, status, r.conflictsFor(addr))
	}
	if c.Status != status {
		r.t.Fatalf("位址 %s 衝突狀態應為 %s，實際 %s", addr, status, c.Status)
	}
	if c.AdoptedMAC != adopted {
		r.t.Fatalf("位址 %s 衝突採信 MAC 應為 %s，實際 %s", addr, adopted, c.AdoptedMAC)
	}
	got := append([]string(nil), c.MACs...)
	want := append([]string(nil), macs...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		r.t.Fatalf("位址 %s 衝突 MAC 集合應為 %v，實際 %v", addr, want, got)
	}
}

func (r *rrRig) wantNoActiveConflict(addr string) {
	r.t.Helper()
	if c := r.activeConflict(addr); c != nil {
		r.t.Fatalf("位址 %s 不應有未解消衝突，實際: %+v", addr, *c)
	}
}

func (r *rrRig) wantResolvedLast(addr, resolution string) {
	r.t.Helper()
	list := r.conflictsFor(addr)
	if len(list) == 0 {
		r.t.Fatalf("位址 %s 應留有衝突歷史，實際沒有", addr)
	}
	var resolved []rrConflict
	for _, c := range list {
		if c.Status == "resolved" {
			resolved = append(resolved, c)
		}
	}
	if len(resolved) != 1 || resolved[0].Resolution == nil || *resolved[0].Resolution != resolution {
		r.t.Fatalf("位址 %s 應有 1 筆 %s 已解消衝突，實際: %+v", addr, resolution, list)
	}
	if r.ip(addr).HasConflict {
		r.t.Fatalf("位址 %s 衝突已解消，has_conflict 應為 false", addr)
	}
}

// register 以一般使用者 chen 登記該位址（擁有者為帳號）。
func (r *rrRig) register(addr string) {
	r.t.Helper()
	id := r.ip(addr).ID
	rec := r.do(http.MethodPost, "/api/v1/ip-records/actions/register", map[string]any{
		"ip_record_ids": []int64{id},
		"owner_mail":    "chen@example.com",
		"hostname":      "rr-host",
	}, r.userID, "user", "chen")
	if rec.Code != http.StatusOK {
		r.t.Fatalf("登記 %s 失敗: %d %s", addr, rec.Code, rec.Body.String())
	}
	if got := r.ip(addr); got.Registration != "registered" || got.OwnerUsername != "chen" {
		r.t.Fatalf("登記後 %s 應為已登記且擁有者為 chen，實際: %+v", addr, got)
	}
}

func (r *rrRig) keepAdopted(addr string) {
	r.t.Helper()
	c := r.activeConflict(addr)
	if c == nil {
		r.t.Fatalf("位址 %s 沒有可裁決的衝突", addr)
	}
	rec := r.asAdmin(http.MethodPost, fmt.Sprintf("/api/v1/conflicts/%d/resolve", c.ID), map[string]string{
		"resolution": "keep_adopted",
		"note":       "回歸測試：維持原本 MAC",
	})
	if rec.Code != http.StatusOK {
		r.t.Fatalf("裁決 keep_adopted 失敗: %d %s", rec.Code, rec.Body.String())
	}
}

func (r *rrRig) wantSightings(addr string, n int) {
	r.t.Helper()
	if got := len(r.ip(addr).Sightings); got != n {
		r.t.Fatalf("位址 %s 應有 %d 筆來源觀測，實際 %d", addr, n, got)
	}
}

// TestReconciliationRoundRegression 以同一個資料庫容器跑完規格 3.6 的整輪決策回歸情境；
// 每個情境使用獨立網段與讀取來源，彼此互不干擾。
func TestReconciliationRoundRegression(t *testing.T) {
	rig := newRRRig(t)

	// 1. 未登記位址由兩個來源回報不同 MAC：採信 MAC 固定（排序後最小者），
	//    換 outputs 順序結果不變，第二輪起不得重複寫入 mac_changed。
	t.Run("UnregisteredTwoSourcesDifferentMAC_StableAdoption", func(t *testing.T) {
		rig := rig.with(t)
		rig.seeder.SeedSubnet("10.20.1.0/24", "managed", nil)
		a, b := rig.newSource("rr1-a"), rig.newSource("rr1-b")
		ipX, ipY, ipZ := "10.20.1.11", "10.20.1.12", "10.20.1.13"

		// 第 1 輪：順序 [A, B]；ipX、ipY 的 MAC 對調，確保結果與哪個來源先處理無關
		rig.round([]*interfaces.PollOutput{
			rig.obs(a, ipX, rrMAC1, ipY, rrMAC2),
			rig.obs(b, ipX, rrMAC2, ipY, rrMAC1),
		})
		for _, addr := range []string{ipX, ipY} {
			got := rig.ip(addr)
			if got.MAC != rrMAC1 {
				t.Fatalf("%s 首次看到多個 MAC，應穩定採信排序最小者 %s，實際 %s", addr, rrMAC1, got.MAC)
			}
			if n := got.countEvents("mac_changed"); n != 0 {
				t.Fatalf("%s 首輪不應有 mac_changed 事件，實際 %d 筆", addr, n)
			}
			if n := got.countEvents("first_seen"); n != 1 {
				t.Fatalf("%s 應只有 1 筆 first_seen，實際 %d 筆", addr, n)
			}
			if !got.HasConflict || len(got.Sightings) != 2 || got.Presence != "online" {
				t.Fatalf("%s 並存多 MAC 應為在線且有衝突、2 筆觀測，實際: %+v", addr, got)
			}
			rig.wantActiveConflict(addr, "open", rrMAC1, rrMAC1, rrMAC2)
		}

		// 第 2 輪：順序反轉 [B, A]；第 3 輪：回到 [A, B]
		rig.round([]*interfaces.PollOutput{
			rig.obs(b, ipX, rrMAC2, ipY, rrMAC1),
			rig.obs(a, ipX, rrMAC1, ipY, rrMAC2),
		})
		rig.round([]*interfaces.PollOutput{
			rig.obs(a, ipX, rrMAC1, ipY, rrMAC2),
			rig.obs(b, ipX, rrMAC2, ipY, rrMAC1),
		})
		for _, addr := range []string{ipX, ipY} {
			got := rig.ip(addr)
			if got.MAC != rrMAC1 {
				t.Fatalf("%s 換 outputs 順序後採信 MAC 不應改變，實際 %s", addr, got.MAC)
			}
			if n := got.countEvents("mac_changed"); n != 0 {
				t.Fatalf("%s 後續輪次不應重複寫 mac_changed，實際 %d 筆", addr, n)
			}
			rig.wantActiveConflict(addr, "open", rrMAC1, rrMAC1, rrMAC2)
		}

		// 新位址在反向順序 [B, A] 的輪次首次出現：初始採信同樣取排序最小者，且無 mac_changed
		rig.round([]*interfaces.PollOutput{
			rig.obs(b, ipX, rrMAC2, ipY, rrMAC1, ipZ, rrMAC2),
			rig.obs(a, ipX, rrMAC1, ipY, rrMAC2, ipZ, rrMAC1),
		})
		got := rig.ip(ipZ)
		if got.MAC != rrMAC1 || got.countEvents("mac_changed") != 0 {
			t.Fatalf("%s 反向順序首次出現應採信 %s 且無 mac_changed，實際: mac=%s events=%+v", ipZ, rrMAC1, got.MAC, got.Events)
		}
		rig.wantActiveConflict(ipZ, "open", rrMAC1, rrMAC1, rrMAC2)
	})

	// 2. 兩個來源回報同一 MAC：不是衝突。
	t.Run("TwoSourcesSameMAC_NotConflict", func(t *testing.T) {
		rig := rig.with(t)
		rig.seeder.SeedSubnet("10.20.2.0/24", "managed", nil)
		a, b := rig.newSource("rr2-a"), rig.newSource("rr2-b")
		addr := "10.20.2.11"

		for i := 0; i < 2; i++ {
			rig.round([]*interfaces.PollOutput{
				rig.obs(a, addr, rrMAC1),
				rig.obs(b, addr, rrMAC1),
			})
		}
		got := rig.ip(addr)
		if got.HasConflict || got.MAC != rrMAC1 || len(got.Sightings) != 2 || got.Presence != "online" {
			t.Fatalf("多台回報同 IP 同 MAC 不應為衝突，實際: %+v", got)
		}
		if got.countEvents("mac_changed") != 0 {
			t.Fatalf("同 MAC 不應有 mac_changed: %+v", got.Events)
		}
		if list := rig.conflictsFor(addr); len(list) != 0 {
			t.Fatalf("不應建立任何衝突紀錄: %+v", list)
		}
	})

	// 3. open 衝突在全部來源離線時保持 open；回到只剩採信 MAC 才自動解消。
	t.Run("OpenConflict_AllOffline_StaysOpen_ThenAutoVanishes", func(t *testing.T) {
		rig := rig.with(t)
		rig.seeder.SeedSubnet("10.20.3.0/24", "managed", nil)
		a, b := rig.newSource("rr3-a"), rig.newSource("rr3-b")
		addr := "10.20.3.11"

		rig.round([]*interfaces.PollOutput{rig.obs(a, addr, rrMAC1), rig.obs(b)})
		rig.register(addr)
		rig.round([]*interfaces.PollOutput{rig.obs(a, addr, rrMAC1), rig.obs(b, addr, rrMAC2)})
		rig.wantActiveConflict(addr, "open", rrMAC1, rrMAC1, rrMAC2)

		// 兩個來源連續 2 輪都成功讀取但不再有此位址 -> 觀測清空、轉離線，衝突仍須保持 open
		rig.round([]*interfaces.PollOutput{rig.obs(a), rig.obs(b)})
		rig.wantSightings(addr, 2)
		rig.round([]*interfaces.PollOutput{rig.obs(a), rig.obs(b)})
		rig.round([]*interfaces.PollOutput{rig.obs(a), rig.obs(b)})
		got := rig.ip(addr)
		if got.Presence != "offline" || len(got.Sightings) != 0 {
			t.Fatalf("全部來源超時未見應離線且無觀測，實際: %+v", got)
		}
		if got.Registration != "registered" || got.OwnerUsername != "chen" {
			t.Fatalf("離線不等於刪除，登記與擁有者必須保留，實際: %+v", got)
		}
		rig.wantActiveConflict(addr, "open", rrMAC1, rrMAC1, rrMAC2)

		// 只有非採信 MAC 回來：有擁有者，衝突仍開著、採信 MAC 與登記不變、不寫 mac_changed
		rig.round([]*interfaces.PollOutput{rig.obs(a), rig.obs(b, addr, rrMAC2)})
		got = rig.ip(addr)
		if got.MAC != rrMAC1 || got.Registration != "registered" || got.countEvents("mac_changed") != 0 {
			t.Fatalf("有擁有者的位址單一不同 MAC 不得更動採信 MAC 與登記，實際: %+v", got)
		}
		rig.wantActiveConflict(addr, "open", rrMAC1, rrMAC1, rrMAC2)

		// 採信 MAC 回來、非採信 MAC 仍在寬限內：集合仍含兩個 MAC，衝突保持 open
		rig.round([]*interfaces.PollOutput{rig.obs(a, addr, rrMAC1), rig.obs(b)})
		rig.wantActiveConflict(addr, "open", rrMAC1, rrMAC1, rrMAC2)

		// 非採信 MAC 過了寬限消失 -> 集合恰為採信 MAC，自動解消
		rig.round([]*interfaces.PollOutput{rig.obs(a, addr, rrMAC1), rig.obs(b)})
		rig.wantNoActiveConflict(addr)
		rig.wantResolvedLast(addr, "auto_vanished")
		if got := rig.ip(addr); got.Presence != "online" || got.MAC != rrMAC1 {
			t.Fatalf("衝突自動解消後應在線並維持採信 MAC，實際: %+v", got)
		}
	})

	// 4. in_progress 衝突：其他 MAC 消失達寬限才解消（解決方式 keep_adopted）。
	t.Run("InProgressConflict_OriginalRule", func(t *testing.T) {
		rig := rig.with(t)
		rig.seeder.SeedSubnet("10.20.4.0/24", "managed", nil)
		a, b := rig.newSource("rr4-a"), rig.newSource("rr4-b")
		stay, gone := "10.20.4.11", "10.20.4.12"

		rig.round([]*interfaces.PollOutput{rig.obs(a, stay, rrMAC1, gone, rrMAC1), rig.obs(b)})
		rig.register(stay)
		rig.register(gone)
		rig.round([]*interfaces.PollOutput{
			rig.obs(a, stay, rrMAC1, gone, rrMAC1),
			rig.obs(b, stay, rrMAC2, gone, rrMAC2),
		})
		rig.keepAdopted(stay)
		rig.keepAdopted(gone)
		rig.wantActiveConflict(stay, "in_progress", rrMAC1, rrMAC1, rrMAC2)

		// 另一 MAC 仍在：維持 in_progress
		rig.round([]*interfaces.PollOutput{
			rig.obs(a, stay, rrMAC1, gone, rrMAC1),
			rig.obs(b, stay, rrMAC2, gone, rrMAC2),
		})
		rig.wantActiveConflict(stay, "in_progress", rrMAC1, rrMAC1, rrMAC2)

		// stay：B 不再回報；gone：A、B 都不再回報。第 1 輪皆在寬限中，仍為 in_progress
		rig.round([]*interfaces.PollOutput{rig.obs(a, stay, rrMAC1), rig.obs(b)})
		rig.wantActiveConflict(stay, "in_progress", rrMAC1, rrMAC1, rrMAC2)
		rig.wantActiveConflict(gone, "in_progress", rrMAC1, rrMAC1, rrMAC2)

		// 第 2 輪達寬限 -> 兩者皆依原規則解消為 keep_adopted
		rig.round([]*interfaces.PollOutput{rig.obs(a, stay, rrMAC1), rig.obs(b)})
		for _, addr := range []string{stay, gone} {
			rig.wantNoActiveConflict(addr)
			rig.wantResolvedLast(addr, "keep_adopted")
		}
	})

	// 5. 讀取失敗的來源：觀測與未見輪數保留，不造成離線或衝突解消。
	t.Run("FailedSource_PreservesSightings", func(t *testing.T) {
		rig := rig.with(t)
		rig.seeder.SeedSubnet("10.20.5.0/24", "managed", nil)
		a, b := rig.newSource("rr5-a"), rig.newSource("rr5-b")
		shared, owned := "10.20.5.11", "10.20.5.12"

		rig.round([]*interfaces.PollOutput{
			rig.obs(a, shared, rrMAC1, owned, rrMAC1),
			rig.obs(b, shared, rrMAC1),
		})
		rig.register(owned)
		rig.round([]*interfaces.PollOutput{
			rig.obs(a, shared, rrMAC1, owned, rrMAC1),
			rig.obs(b, shared, rrMAC1, owned, rrMAC2),
		})
		rig.wantActiveConflict(owned, "open", rrMAC1, rrMAC1, rrMAC2)

		// B 連續 4 輪讀取失敗（超過寬限輪數）：A 照常回報，B 的觀測不得被清除
		for i := 0; i < 4; i++ {
			rig.round([]*interfaces.PollOutput{rig.obs(a, shared, rrMAC1, owned, rrMAC1)}, b)
		}
		rig.wantSightings(shared, 2)
		rig.wantSightings(owned, 2)
		rig.wantActiveConflict(owned, "open", rrMAC1, rrMAC1, rrMAC2)

		// 全部來源都讀取失敗 3 輪：仍在線、觀測保留
		for i := 0; i < 3; i++ {
			rig.round(nil, a, b)
		}
		for _, addr := range []string{shared, owned} {
			got := rig.ip(addr)
			if got.Presence != "online" || len(got.Sightings) != 2 {
				t.Fatalf("全部來源失敗的輪次不得使 %s 離線或清觀測，實際: %+v", addr, got)
			}
		}
		rig.wantActiveConflict(owned, "open", rrMAC1, rrMAC1, rrMAC2)

		// B 恢復成功但不再有此位址：失敗輪次不累計，所以要再 2 輪才清除
		rig.round([]*interfaces.PollOutput{rig.obs(a, shared, rrMAC1, owned, rrMAC1), rig.obs(b)})
		rig.wantSightings(shared, 2)
		rig.wantActiveConflict(owned, "open", rrMAC1, rrMAC1, rrMAC2)
		rig.round([]*interfaces.PollOutput{rig.obs(a, shared, rrMAC1, owned, rrMAC1), rig.obs(b)})
		rig.wantSightings(shared, 1)
		rig.wantSightings(owned, 1)
		rig.wantNoActiveConflict(owned)
		rig.wantResolvedLast(owned, "auto_vanished")
	})

	// 6. 單一不同 MAC：有擁有者開衝突、登記不變；未登記直接更新採信 MAC 且只寫一次事件。
	t.Run("SingleDifferentMAC_OwnedConflict_UnregisteredUpdates", func(t *testing.T) {
		rig := rig.with(t)
		rig.seeder.SeedSubnet("10.20.6.0/24", "managed", nil)
		a := rig.newSource("rr6-a")
		unreg, owned := "10.20.6.11", "10.20.6.12"

		rig.round([]*interfaces.PollOutput{rig.obs(a, unreg, rrMAC1, owned, rrMAC1)})
		rig.register(owned)

		for i := 0; i < 3; i++ {
			rig.round([]*interfaces.PollOutput{rig.obs(a, unreg, rrMAC2, owned, rrMAC2)})
		}

		got := rig.ip(unreg)
		if got.MAC != rrMAC2 || got.countEvents("mac_changed") != 1 || got.HasConflict {
			t.Fatalf("未登記位址換 MAC 應更新採信 MAC、只寫 1 筆 mac_changed 且無衝突，實際: %+v", got)
		}
		if list := rig.conflictsFor(unreg); len(list) != 0 {
			t.Fatalf("未登記位址換 MAC 不應開衝突: %+v", list)
		}

		got = rig.ip(owned)
		if got.MAC != rrMAC1 || got.Registration != "registered" || got.OwnerUsername != "chen" || got.countEvents("mac_changed") != 0 {
			t.Fatalf("有擁有者的位址換 MAC 不得更動採信 MAC 與登記，實際: %+v", got)
		}
		rig.wantActiveConflict(owned, "open", rrMAC1, rrMAC1, rrMAC2)
	})

	// 7. 未登記位址的衝突：只剩採信 MAC 才自動解消；採信 MAC 消失而只剩單一新 MAC 時改採信新 MAC。
	t.Run("UnregisteredConflict_ResolutionPaths", func(t *testing.T) {
		rig := rig.with(t)
		rig.seeder.SeedSubnet("10.20.7.0/24", "managed", nil)
		a, b := rig.newSource("rr7-a"), rig.newSource("rr7-b")
		keep, swap := "10.20.7.11", "10.20.7.12"

		both := func() []*interfaces.PollOutput {
			return []*interfaces.PollOutput{
				rig.obs(a, keep, rrMAC1, swap, rrMAC1),
				rig.obs(b, keep, rrMAC2, swap, rrMAC2),
			}
		}
		rig.round(both())
		rig.wantActiveConflict(keep, "open", rrMAC1, rrMAC1, rrMAC2)
		rig.wantActiveConflict(swap, "open", rrMAC1, rrMAC1, rrMAC2)

		// keep：B 不再回報；swap：A 不再回報（皆為第 1 輪寬限中，集合不變、衝突保持）
		rig.round([]*interfaces.PollOutput{
			rig.obs(a, keep, rrMAC1),
			rig.obs(b, swap, rrMAC2),
		})
		rig.wantActiveConflict(keep, "open", rrMAC1, rrMAC1, rrMAC2)
		rig.wantActiveConflict(swap, "open", rrMAC1, rrMAC1, rrMAC2)

		// 第 2 輪達寬限
		rig.round([]*interfaces.PollOutput{
			rig.obs(a, keep, rrMAC1),
			rig.obs(b, swap, rrMAC2),
		})

		rig.wantNoActiveConflict(keep)
		rig.wantResolvedLast(keep, "auto_vanished")
		if got := rig.ip(keep); got.MAC != rrMAC1 || got.countEvents("mac_changed") != 0 {
			t.Fatalf("keep 只剩採信 MAC，不應有 mac_changed，實際: %+v", got)
		}

		rig.wantNoActiveConflict(swap)
		rig.wantResolvedLast(swap, "auto_vanished")
		if got := rig.ip(swap); got.MAC != rrMAC2 || got.countEvents("mac_changed") != 1 {
			t.Fatalf("swap 採信 MAC 消失後應改採信唯一 MAC %s 並寫 1 筆 mac_changed，實際: %+v", rrMAC2, got)
		}
	})
}
