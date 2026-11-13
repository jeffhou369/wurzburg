package test

// 本檔驗收「整輪輪詢」：真實的 Scheduler.RunCycle -> 管理範圍 (Scope) -> 狀態整併 (Reconciliation)。
//
// 重要限制（請勿誤讀量測結果）：
//   - 外部系統（SNMP 採集模組）以行程內的測試替身取代，呼叫立即回傳，不含任何網路延遲、
//     SNMP 逾時、重試等待或交換器回應時間；因此本檔量到的時間只代表「資料庫 + 後端處理」
//     在測試環境（單機 Docker 內的 MariaDB）的成本，不是實機 SNMP 的輪詢效能。
//   - 3 分鐘與 5 秒是依設計目標設的測試環境門檻，不是對實機效能的承諾。
//   - 全部產品狀態都經由真實 API 與真實排程產生，不以測試 SQL 預先塞入，也不使用 X-Test-* 身分標頭。

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/accounts"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/config"
	"github.com/jeffhou369/wurzburg/backend/internal/crypto"
	"github.com/jeffhou369/wurzburg/backend/internal/dashboard"
	"github.com/jeffhou369/wurzburg/backend/internal/database"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
	"github.com/jeffhou369/wurzburg/backend/internal/iprecords"
	"github.com/jeffhou369/wurzburg/backend/internal/oui"
	"github.com/jeffhou369/wurzburg/backend/internal/pipeline"
	"github.com/jeffhou369/wurzburg/backend/internal/reconciliation"
	"github.com/jeffhou369/wurzburg/backend/internal/scheduler"
	"github.com/jeffhou369/wurzburg/backend/internal/server"
	"github.com/jeffhou369/wurzburg/backend/internal/subnets"
	"github.com/jeffhou369/wurzburg/backend/internal/switches"
	"github.com/jeffhou369/wurzburg/backend/migrations"
	"github.com/testcontainers/testcontainers-go"
	mariadbmod "github.com/testcontainers/testcontainers-go/modules/mariadb"
)

const (
	scaleSecretKey = "0123456789abcdef0123456789abcdef"
	scaleAdminUser = "admin"
	scaleInitialPW = "InitialAdminPass123!"
	scaleChangedPW = "NewStrongAdminPassword2026!"
)

// =============================================================================
// 採集模組測試替身：只依 VRFReadInput 內的公開設定（管理 IP、VRF 名稱）回應，不猜資料庫自增 ID
// =============================================================================

type scaleKey struct {
	mgmtIP  string
	vrfName string
}

// scaleFixture 是一個 VRF 讀取設定在交換器端「看起來的樣子」。
type scaleFixture struct {
	key       scaleKey
	platform  string
	community string
	context   string
	ifAddr    interfaces.InterfaceAddress
	// hosts 是落在納管網段內、應該各產生一筆 IP 紀錄的主機。
	hosts []interfaces.ARPObservation
	// extra 是不應計入有效規模的觀測：閘道自身的 ARP 項目、範圍外雜訊。
	extra []interfaces.ARPObservation
}

type scalePhase int

const (
	// scalePhaseDiscover 只回報介面位址表（讓網段被發現），不回報 ARP。
	scalePhaseDiscover scalePhase = iota
	// scalePhasePoll 回報完整的介面位址、ARP 與介面名稱。
	scalePhasePoll
)

type scaleFailure struct {
	remaining int // 還要失敗幾次；負數代表永遠失敗
	err       error
}

type scaleCollector struct {
	clk *clock.TestClock

	mu         sync.Mutex
	phase      scalePhase
	fixtures   map[scaleKey]*scaleFixture
	failures   map[scaleKey]*scaleFailure
	garbage    map[scaleKey][]interfaces.ARPObservation
	badAddrs   map[scaleKey][]interfaces.InterfaceAddress
	silent     map[scaleKey]bool
	calls      map[scaleKey]int
	violations []string
}

func newScaleCollector(clk *clock.TestClock) *scaleCollector {
	return &scaleCollector{
		clk:      clk,
		fixtures: make(map[scaleKey]*scaleFixture),
		failures: make(map[scaleKey]*scaleFailure),
		garbage:  make(map[scaleKey][]interfaces.ARPObservation),
		badAddrs: make(map[scaleKey][]interfaces.InterfaceAddress),
		silent:   make(map[scaleKey]bool),
		calls:    make(map[scaleKey]int),
	}
}

func (c *scaleCollector) setPhase(p scalePhase) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.phase = p
}

func (c *scaleCollector) failVRF(key scaleKey, times int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures[key] = &scaleFailure{remaining: times, err: err}
}

func (c *scaleCollector) clearFailures() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures = make(map[scaleKey]*scaleFailure)
}

func (c *scaleCollector) addGarbage(key scaleKey, obs ...interfaces.ARPObservation) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.garbage[key] = append(c.garbage[key], obs...)
}

func (c *scaleCollector) addBadAddresses(key scaleKey, addrs ...interfaces.InterfaceAddress) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.badAddrs[key] = append(c.badAddrs[key], addrs...)
}

// setSilent 讓指定 VRF 讀取成功，但 ARP 表是空的（交換器真的看不到任何主機）。
func (c *scaleCollector) setSilent(key scaleKey, silent bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.silent[key] = silent
}

func (c *scaleCollector) resetCalls() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = make(map[scaleKey]int)
}

func (c *scaleCollector) callCount(key scaleKey) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[key]
}

func (c *scaleCollector) totalCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	total := 0
	for _, n := range c.calls {
		total += n
	}
	return total
}

func (c *scaleCollector) contractViolations() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.violations...)
}

// Collect 依輸入中的公開設定回應；輸入若與登錄的交換器設定不符，記錄為合約違反並回傳錯誤。
func (c *scaleCollector) Collect(_ context.Context, in interfaces.VRFReadInput) (*interfaces.PollOutput, error) {
	key := scaleKey{mgmtIP: in.MgmtIP, vrfName: in.VRFName}

	c.mu.Lock()
	c.calls[key]++
	fx, known := c.fixtures[key]
	if !known {
		c.violations = append(c.violations, fmt.Sprintf("未登錄的讀取設定 %s/%s", in.MgmtIP, in.VRFName))
		c.mu.Unlock()
		return nil, &interfaces.CollectError{Class: interfaces.ErrClassNoData, Summary: "未登錄的讀取設定"}
	}
	var problems []string
	if in.SwitchID <= 0 || in.VRFConfigID <= 0 {
		problems = append(problems, "缺少 SwitchID 或 VRFConfigID")
	}
	if in.Platform != fx.platform {
		problems = append(problems, fmt.Sprintf("平台 %q 與預期 %q 不符", in.Platform, fx.platform))
	}
	if in.SNMPVersion != "v2c" || in.SNMPPort != 161 {
		problems = append(problems, fmt.Sprintf("SNMP 版本或埠號不符: %s/%d", in.SNMPVersion, in.SNMPPort))
	}
	if in.SNMPCommunity != fx.community {
		problems = append(problems, "解密後的 Community 與建立時不一致")
	}
	if in.SNMPContext != fx.context {
		problems = append(problems, fmt.Sprintf("SNMP Context %q 與預期 %q 不符", in.SNMPContext, fx.context))
	}
	if len(problems) > 0 {
		c.violations = append(c.violations, fmt.Sprintf("%s/%s: %s", in.MgmtIP, in.VRFName, strings.Join(problems, "; ")))
		c.mu.Unlock()
		return nil, &interfaces.CollectError{Class: interfaces.ErrClassAuthFailed, Summary: "輸入違反採集合約"}
	}
	if f := c.failures[key]; f != nil && f.remaining != 0 {
		if f.remaining > 0 {
			f.remaining--
		}
		err := f.err
		c.mu.Unlock()
		return nil, err
	}
	phase := c.phase
	garbage := c.garbage[key]
	badAddrs := c.badAddrs[key]
	silent := c.silent[key]
	c.mu.Unlock()

	out := &interfaces.PollOutput{
		VRFConfigID:        in.VRFConfigID,
		SwitchID:           in.SwitchID,
		VRFName:            in.VRFName,
		CollectedAt:        c.clk.Now(),
		InterfaceAddresses: append([]interfaces.InterfaceAddress{fx.ifAddr}, badAddrs...),
		InterfaceNames:     map[int64]string{fx.ifAddr.IfIndex: fx.ifAddr.IfName},
	}
	if phase == scalePhasePoll && !silent {
		arp := make([]interfaces.ARPObservation, 0, len(fx.hosts)+len(fx.extra)+len(garbage))
		arp = append(arp, fx.hosts...)
		arp = append(arp, fx.extra...)
		arp = append(arp, garbage...)
		out.ARPObservations = arp
	}
	return out, nil
}

// =============================================================================
// 測試環境：全新 MariaDB 容器、真實 server / 排程 / Scope / Reconciliation
// =============================================================================

type scaleEnv struct {
	t      *testing.T
	ctx    context.Context
	db     *sql.DB
	clk    *clock.TestClock
	ciph   *crypto.Cipher
	srv    *server.Server
	router http.Handler
	scope  *subnets.ScopeService
	recon  *reconciliation.Service
	timers *scaleStepTimers
	sched  *scheduler.Scheduler
	col    *scaleCollector
	cookie *http.Cookie

	// slowestPage 記錄 ipPage 經手過最慢的一次請求，供規模測試回報逐頁讀取時最糟的一頁。
	slowestPage time.Duration
}

func newScaleEnv(t *testing.T) *scaleEnv {
	t.Helper()
	if testing.Short() {
		t.Skip("略過需啟動 Docker 容器的整輪輪詢測試")
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
		t.Fatalf("套用資料庫遷移失敗: %v", err)
	}

	clk := clock.NewTestClock(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC))
	ciph, err := crypto.NewCipher(scaleSecretKey)
	if err != nil {
		t.Fatalf("初始化 Cipher 失敗: %v", err)
	}
	col := newScaleCollector(clk)

	// AppEnv 刻意不是 "test"：伺服器會忽略 X-Test-* 身分標頭，所有請求都必須靠真實登入的工作階段。
	srv := server.New(&config.Config{
		Port:       8080,
		CampusName: "Würzburg",
		AppEnv:     "production",
		SecretKey:  scaleSecretKey,
	}, db, clk)

	ouiSvc := oui.NewService(db, clk, srv.Audit)
	_ = ouiSvc.EnsureLoaded(ctx)

	scopeSvc := subnets.NewScopeService(db, clk, srv.Audit, scaleSecretKey)
	resolver := subnets.NewResolver(db)
	reconSvc := reconciliation.NewService(db, clk, srv.Settings, resolver, ouiSvc)
	// 計時包裝只量測，不改變行為：呼叫的仍是真實的 Scope 與 Reconciliation。
	timers := &scaleStepTimers{scope: scopeSvc, recon: reconSvc}
	sched := scheduler.New(db, clk, ciph, srv.Settings, srv.Audit, col, pipeline.New(timers, timers))

	srv.Mount(
		accounts.NewHandler(db, clk, srv.Sessions, srv.Settings, srv.Audit, interfaces.NewFakeAuthSource()),
		scopeSvc,
		switches.NewHandler(db, clk, ciph, srv.Audit, col),
		sched,
		dashboard.NewHandler(db, clk, sched),
		iprecords.NewHandler(db, clk, srv.Settings, srv.Audit),
	)

	return &scaleEnv{
		t: t, ctx: ctx, db: db, clk: clk, ciph: ciph, srv: srv, router: apiRouter(t, srv),
		scope: scopeSvc, recon: reconSvc, timers: timers, sched: sched, col: col,
	}
}

// scaleStepTimers 同時實作 SubnetScopeProcessor 與 ReconcilerProcessor，
// 轉呼叫真實的 Scope / Reconciliation，並記下每一步最近一次的耗時。
type scaleStepTimers struct {
	scope *subnets.ScopeService
	recon *reconciliation.Service

	mu           sync.Mutex
	scopeElapsed time.Duration
	reconElapsed time.Duration
}

func (s *scaleStepTimers) ApplyInterfaceAddresses(ctx context.Context, outputs []*interfaces.PollOutput) error {
	start := time.Now()
	err := s.scope.ApplyInterfaceAddresses(ctx, outputs)
	s.mu.Lock()
	s.scopeElapsed = time.Since(start)
	s.mu.Unlock()
	return err
}

func (s *scaleStepTimers) ReconcileObservations(ctx context.Context, outputs []*interfaces.PollOutput, failedVRFConfigIDs []int64) error {
	start := time.Now()
	err := s.recon.ReconcileObservations(ctx, outputs, failedVRFConfigIDs)
	s.mu.Lock()
	s.reconElapsed = time.Since(start)
	s.mu.Unlock()
	return err
}

func (s *scaleStepTimers) last() (scope, recon time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scopeElapsed, s.reconElapsed
}

// request 送出一個帶 CSRF 標頭的 HTTP 請求；cookie 為 nil 時不帶工作階段。
func (e *scaleEnv) request(method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	var rdr *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rdr = bytes.NewReader(raw)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

// call 以管理員工作階段呼叫 API，狀態碼不符即讓測試失敗，並把回應本文解碼進 out（可為 nil）。
func (e *scaleEnv) call(method, path string, body any, wantStatus int, out any) {
	e.t.Helper()
	w := e.request(method, path, body, e.cookie)
	if w.Code != wantStatus {
		e.t.Fatalf("%s %s 應回傳 %d，實際 %d: %s", method, path, wantStatus, w.Code, w.Body.String())
	}
	if out != nil {
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			e.t.Fatalf("%s %s 回應不是預期的 JSON: %v", method, path, err)
		}
	}
}

// login 以真實登入端點取得工作階段 Cookie。
func (e *scaleEnv) login(password string) (*http.Cookie, bool) {
	e.t.Helper()
	w := e.request(http.MethodPost, "/api/v1/auth/login/local", map[string]string{
		"username": scaleAdminUser, "password": password,
	}, nil)
	if w.Code != http.StatusOK {
		e.t.Fatalf("登入失敗: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		User struct {
			RequiresPasswordChange bool `json:"requires_password_change"`
		} `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		e.t.Fatalf("登入回應不是預期的 JSON: %v", err)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "wurzburg_session" {
			return c, resp.User.RequiresPasswordChange
		}
	}
	e.t.Fatalf("登入成功但沒有收到工作階段 Cookie")
	return nil, false
}

// bootstrapAdmin 依正式流程：EnsureFirstAdmin -> 初始密碼登入 -> 強制改密碼 -> 以新密碼重新登入。
func (e *scaleEnv) bootstrapAdmin() {
	e.t.Helper()
	if err := accounts.EnsureFirstAdmin(e.ctx, e.db, e.clk, scaleAdminUser, scaleInitialPW); err != nil {
		e.t.Fatalf("建立初始系統管理員失敗: %v", err)
	}
	cookie, mustChange := e.login(scaleInitialPW)
	if !mustChange {
		e.t.Fatalf("第一次登入應標示 requires_password_change")
	}
	// 尚未改密碼前，其他 API 必須被擋下
	if w := e.request(http.MethodGet, "/api/v1/switches", nil, cookie); w.Code != http.StatusForbidden {
		e.t.Fatalf("改密碼前存取一般 API 應回傳 403，實際 %d", w.Code)
	}
	if w := e.request(http.MethodPost, "/api/v1/auth/change-password", map[string]string{
		"new_password": scaleChangedPW,
	}, cookie); w.Code != http.StatusOK {
		e.t.Fatalf("修改初始密碼失敗: %d %s", w.Code, w.Body.String())
	}
	e.relogin()
	if !e.requireNoTestHeaderBypass() {
		e.t.Fatalf("伺服器仍接受 X-Test-* 身分標頭，本測試不得在此模式下執行")
	}
}

// relogin 以新密碼重新登入（測試時鐘推進後用來避免工作階段逾時，仍是真實登入）。
func (e *scaleEnv) relogin() {
	e.t.Helper()
	cookie, mustChange := e.login(scaleChangedPW)
	if mustChange {
		e.t.Fatalf("改密碼後不應再要求修改密碼")
	}
	e.cookie = cookie
}

// requireNoTestHeaderBypass 確認 X-Test-* 標頭在此伺服器無法冒充身分。
func (e *scaleEnv) requireNoTestHeaderBypass() bool {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/switches", nil)
	req.Header.Set("X-Test-User-ID", "1")
	req.Header.Set("X-Test-Role", "admin")
	req.Header.Set("X-Test-Username", scaleAdminUser)
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w.Code == http.StatusUnauthorized
}

// advance 推進測試時鐘並重新登入。
func (e *scaleEnv) advance(d time.Duration) {
	e.t.Helper()
	e.clk.Advance(d)
	e.relogin()
}

// =============================================================================
// 拓樸：經 HTTP 建立交換器與 VRF 讀取設定，並登錄對應的採集替身
// =============================================================================

type scaleSpec struct {
	switches      int
	vrfsPerSwitch int // 含 default
	hostsPerVRF   int
}

type scaleVRF struct {
	configID int64
	name     string
	key      scaleKey
	cidr     string
	fixture  *scaleFixture
}

type scaleSwitch struct {
	index    int
	id       int64
	name     string
	mgmtIP   string
	platform string
	vrfs     []*scaleVRF
}

type scaleExpectedIP struct {
	mac        string
	cidr       string
	switchID   int64
	switchName string
	vrfName    string
	key        scaleKey
}

type scaleTopology struct {
	spec     scaleSpec
	switches []*scaleSwitch
	// expected 是 IP -> 預期內容；筆數即「有效規模」，只含納管網段內的主機。
	expected map[string]scaleExpectedIP
}

func (t *scaleTopology) vrfCount() int {
	n := 0
	for _, sw := range t.switches {
		n += len(sw.vrfs)
	}
	return n
}

func (t *scaleTopology) allVRFs() []*scaleVRF {
	var all []*scaleVRF
	for _, sw := range t.switches {
		all = append(all, sw.vrfs...)
	}
	return all
}

// buildTopology 建立 spec 描述的拓樸：一半 ios-xe、一半 nx-os；每個 VRF 有自己的 /24，
// 網段不重疊（ip_records 以 IP 全域唯一）。位址規劃：
//   - 管理 IP 172.31.{i/250}.{i%250+1}，不在任何納管網段內
//   - 第 i 台第 v 個 VRF 的網段 10.{i+1}.{v}.0/24，閘道 .254，主機 .1 ~ .hostsPerVRF
func (e *scaleEnv) buildTopology(spec scaleSpec) *scaleTopology {
	e.t.Helper()
	if spec.switches > 250 || spec.vrfsPerSwitch > 255 || spec.hostsPerVRF > 253 {
		e.t.Fatalf("拓樸規格超出位址規劃範圍: %+v", spec)
	}
	topo := &scaleTopology{spec: spec, expected: make(map[string]scaleExpectedIP)}

	for i := 0; i < spec.switches; i++ {
		sw := &scaleSwitch{
			index:    i,
			name:     fmt.Sprintf("sw-%03d", i+1),
			mgmtIP:   fmt.Sprintf("172.31.%d.%d", i/250, i%250+1),
			platform: "ios-xe",
		}
		if i%2 == 1 {
			sw.platform = "nx-os"
		}

		var created struct {
			ID         int64 `json:"id"`
			VRFConfigs []struct {
				ID      int64  `json:"id"`
				VRFName string `json:"vrf_name"`
			} `json:"vrf_configs"`
		}
		e.call(http.MethodPost, "/api/v1/switches", map[string]any{
			"name":           sw.name,
			"mgmt_ip":        sw.mgmtIP,
			"platform":       sw.platform,
			"snmp_version":   "v2c",
			"snmp_community": scaleCommunity(sw.mgmtIP, "default"),
			"role_label":     "核心",
		}, http.StatusCreated, &created)
		sw.id = created.ID
		if len(created.VRFConfigs) != 1 || created.VRFConfigs[0].VRFName != "default" {
			e.t.Fatalf("新增交換器 %s 應自動建立 default VRF 讀取設定: %+v", sw.name, created.VRFConfigs)
		}
		configIDs := map[string]int64{"default": created.VRFConfigs[0].ID}

		for v := 1; v < spec.vrfsPerSwitch; v++ {
			vrfName := fmt.Sprintf("vrf%d", v)
			var added struct {
				Items []struct {
					ID      int64  `json:"id"`
					VRFName string `json:"vrf_name"`
				} `json:"items"`
			}
			e.call(http.MethodPost, fmt.Sprintf("/api/v1/switches/%d/vrfs", sw.id), map[string]any{
				"vrf_name":       vrfName,
				"snmp_context":   scaleContext(vrfName),
				"snmp_community": scaleCommunity(sw.mgmtIP, vrfName),
			}, http.StatusCreated, &added)
			for _, it := range added.Items {
				configIDs[it.VRFName] = it.ID
			}
		}

		for v := 0; v < spec.vrfsPerSwitch; v++ {
			vrfName := "default"
			if v > 0 {
				vrfName = fmt.Sprintf("vrf%d", v)
			}
			if configIDs[vrfName] == 0 {
				e.t.Fatalf("交換器 %s 找不到 VRF %s 的讀取設定 ID", sw.name, vrfName)
			}
			fx := newScaleFixture(sw, vrfName, v, spec.hostsPerVRF)
			e.col.fixtures[fx.key] = fx
			vrf := &scaleVRF{
				configID: configIDs[vrfName],
				name:     vrfName,
				key:      fx.key,
				cidr:     fmt.Sprintf("10.%d.%d.0/24", i+1, v),
				fixture:  fx,
			}
			sw.vrfs = append(sw.vrfs, vrf)
			for _, h := range fx.hosts {
				topo.expected[h.IPAddress] = scaleExpectedIP{
					mac: h.MACAddress, cidr: vrf.cidr, switchID: sw.id, switchName: sw.name, vrfName: vrfName, key: fx.key,
				}
			}
		}
		topo.switches = append(topo.switches, sw)
	}
	return topo
}

func scaleCommunity(mgmtIP, vrfName string) string { return "comm-" + mgmtIP + "-" + vrfName }

func scaleContext(vrfName string) string {
	if vrfName == "default" {
		return ""
	}
	return "ctx-" + vrfName
}

func newScaleFixture(sw *scaleSwitch, vrfName string, vrfIndex, hosts int) *scaleFixture {
	i := sw.index
	fx := &scaleFixture{
		key:       scaleKey{mgmtIP: sw.mgmtIP, vrfName: vrfName},
		platform:  sw.platform,
		community: scaleCommunity(sw.mgmtIP, vrfName),
		context:   scaleContext(vrfName),
		ifAddr: interfaces.InterfaceAddress{
			IPAddress: fmt.Sprintf("10.%d.%d.254", i+1, vrfIndex),
			NetMask:   "255.255.255.0",
			IfIndex:   int64(1000 + vrfIndex),
			IfName:    fmt.Sprintf("Vlan%d", 100+vrfIndex),
		},
	}
	for h := 1; h <= hosts; h++ {
		fx.hosts = append(fx.hosts, interfaces.ARPObservation{
			IPAddress:  fmt.Sprintf("10.%d.%d.%d", i+1, vrfIndex, h),
			MACAddress: fmt.Sprintf("02:00:%02x:%02x:00:%02x", i+1, vrfIndex, h),
			IfIndex:    fx.ifAddr.IfIndex,
			IfName:     fx.ifAddr.IfName,
			ARPType:    "dynamic",
		})
	}
	// 閘道自己的 ARP 項目：位於納管網段內，但閘道位址不得產生 IP 紀錄
	fx.extra = append(fx.extra, interfaces.ARPObservation{
		IPAddress:  fx.ifAddr.IPAddress,
		MACAddress: fmt.Sprintf("02:ff:%02x:%02x:00:fe", i+1, vrfIndex),
		IfIndex:    fx.ifAddr.IfIndex,
		IfName:     fx.ifAddr.IfName,
		ARPType:    "static",
	})
	// 每台交換器的 default VRF 另帶一筆範圍外雜訊（TEST-NET-1），不屬於任何已知網段
	if vrfIndex == 0 {
		fx.extra = append(fx.extra, interfaces.ARPObservation{
			IPAddress:  fmt.Sprintf("192.0.2.%d", i%250+1),
			MACAddress: fmt.Sprintf("02:ee:%02x:00:00:01", i+1),
			IfIndex:    fx.ifAddr.IfIndex,
			IfName:     fx.ifAddr.IfName,
			ARPType:    "dynamic",
		})
	}
	return fx
}

// =============================================================================
// 紅燈回歸：管線（Scope / Reconciliation）錯誤必須由 RunCycle 往上傳回
// =============================================================================

var errScaleSentinel = errors.New("測試用管線哨兵錯誤")

type scaleFailingScope struct {
	err   error
	calls int
}

func (s *scaleFailingScope) ApplyInterfaceAddresses(_ context.Context, _ []*interfaces.PollOutput) error {
	s.calls++
	return s.err
}

type scaleRecordingReconciler struct {
	err   error
	calls int
}

func (r *scaleRecordingReconciler) ReconcileObservations(_ context.Context, _ []*interfaces.PollOutput, _ []int64) error {
	r.calls++
	return r.err
}

func TestScalePollCycle_RunCycleReturnsPipelineErrors(t *testing.T) {
	env := newScaleEnv(t)
	env.bootstrapAdmin()
	env.buildTopology(scaleSpec{switches: 1, vrfsPerSwitch: 1, hostsPerVRF: 3})
	env.col.setPhase(scalePhasePoll)

	newSched := func(p *pipeline.PollPipeline) *scheduler.Scheduler {
		return scheduler.New(env.db, env.clk, env.ciph, env.srv.Settings, env.srv.Audit, env.col, p)
	}

	t.Run("範圍步驟失敗時 RunCycle 回傳該錯誤且不進入整併", func(t *testing.T) {
		scope := &scaleFailingScope{err: errScaleSentinel}
		recon := &scaleRecordingReconciler{}
		sched := newSched(pipeline.New(scope, recon))

		err := sched.RunCycle(env.ctx)
		if !errors.Is(err, errScaleSentinel) {
			t.Fatalf("管線的範圍步驟失敗應由 RunCycle 回傳，實際得到 %v", err)
		}
		if scope.calls != 1 || recon.calls != 0 {
			t.Fatalf("範圍步驟失敗後不應進入整併: scope=%d recon=%d", scope.calls, recon.calls)
		}
		if inProgress, _, last, _ := sched.Status(); inProgress || last == nil {
			t.Fatalf("管線失敗後排程狀態應已收尾: inProgress=%v last=%v", inProgress, last)
		}
	})

	t.Run("整併步驟失敗時 RunCycle 回傳該錯誤", func(t *testing.T) {
		recon := &scaleRecordingReconciler{err: errScaleSentinel}
		sched := newSched(pipeline.New(env.scope, recon))

		err := sched.RunCycle(env.ctx)
		if !errors.Is(err, errScaleSentinel) {
			t.Fatalf("管線的整併步驟失敗應由 RunCycle 回傳，實際得到 %v", err)
		}
		if recon.calls != 1 {
			t.Fatalf("整併步驟應被呼叫一次，實際 %d", recon.calls)
		}
	})

	t.Run("對照：管線成功時 RunCycle 回傳 nil", func(t *testing.T) {
		recon := &scaleRecordingReconciler{}
		sched := newSched(pipeline.New(env.scope, recon))
		if err := sched.RunCycle(env.ctx); err != nil {
			t.Fatalf("管線成功時不應回傳錯誤: %v", err)
		}
		if recon.calls != 1 {
			t.Fatalf("整併步驟應被呼叫一次，實際 %d", recon.calls)
		}
	})

	if v := env.col.contractViolations(); len(v) > 0 {
		t.Fatalf("採集替身收到違反合約的輸入: %v", v)
	}
}

// =============================================================================
// 經由 API 觀察結果的輔助函式（驗收一律從公開介面讀，不直接查資料表）
// =============================================================================

type scaleSubnetRow struct {
	ID          int64  `json:"id"`
	CIDR        string `json:"cidr"`
	State       string `json:"state"`
	IPCount     int    `json:"ip_count"`
	OnlineCount int    `json:"online_count"`
}

type scaleIPRow struct {
	ID           int64    `json:"id"`
	IPAddress    string   `json:"ip_address"`
	MACAddress   string   `json:"mac_address"`
	SubnetID     int64    `json:"subnet_id"`
	SubnetCIDR   string   `json:"subnet_cidr"`
	SwitchNames  []string `json:"switch_names"`
	Presence     string   `json:"presence"`
	Registration string   `json:"registration"`
	HasConflict  bool     `json:"has_conflict"`
}

type scaleVRFView struct {
	ID               int64  `json:"id"`
	VRFName          string `json:"vrf_name"`
	LastStatus       string `json:"last_status"`
	LastErrorClass   string `json:"last_error_class"`
	LastLearnedCount int    `json:"last_learned_count"`
}

type scaleSwitchView struct {
	ID         int64          `json:"id"`
	Name       string         `json:"name"`
	MgmtIP     string         `json:"mgmt_ip"`
	VRFConfigs []scaleVRFView `json:"vrf_configs"`
}

type scaleStats struct {
	ManagedTotal       int  `json:"managed_total"`
	OnlineCount        int  `json:"online_count"`
	OfflineCount       int  `json:"offline_count"`
	UnregisteredOnline int  `json:"unregistered_online_count"`
	OpenConflicts      int  `json:"open_conflicts_count"`
	SwitchesCount      int  `json:"switches_count"`
	FailedVRFCount     int  `json:"failed_vrf_count"`
	ExpiredCount       int  `json:"expired_count"`
	SyncInProgress     bool `json:"sync_in_progress"`
}

type scalePage struct {
	Items      []scaleIPRow `json:"items"`
	Pagination struct {
		Page       int `json:"page"`
		PageSize   int `json:"page_size"`
		TotalItems int `json:"total_items"`
		TotalPages int `json:"total_pages"`
	} `json:"pagination"`
}

func (e *scaleEnv) listSubnets() (pending int, rows []scaleSubnetRow) {
	e.t.Helper()
	var resp struct {
		PendingCount int              `json:"pending_count"`
		Items        []scaleSubnetRow `json:"items"`
	}
	e.call(http.MethodGet, "/api/v1/subnets", nil, http.StatusOK, &resp)
	return resp.PendingCount, resp.Items
}

// markAllSubnetsManaged 經 API 把所有待檢視網段設為納管；回傳被設定的網段數。
func (e *scaleEnv) markAllSubnetsManaged() int {
	e.t.Helper()
	_, rows := e.listSubnets()
	n := 0
	for _, r := range rows {
		if r.State != "pending" {
			continue
		}
		e.call(http.MethodPut, fmt.Sprintf("/api/v1/subnets/%d/state", r.ID), map[string]string{"state": "managed"}, http.StatusOK, nil)
		n++
	}
	return n
}

// vrfViews 讀 /switches，回傳 (管理 IP, VRF 名稱) -> 讀取狀態。
func (e *scaleEnv) vrfViews() map[scaleKey]scaleVRFView {
	e.t.Helper()
	var resp struct {
		Items []scaleSwitchView `json:"items"`
	}
	e.call(http.MethodGet, "/api/v1/switches", nil, http.StatusOK, &resp)
	views := make(map[scaleKey]scaleVRFView)
	for _, sw := range resp.Items {
		for _, v := range sw.VRFConfigs {
			views[scaleKey{mgmtIP: sw.MgmtIP, vrfName: v.VRFName}] = v
		}
	}
	return views
}

func (e *scaleEnv) stats() scaleStats {
	e.t.Helper()
	var st scaleStats
	e.call(http.MethodGet, "/api/v1/dashboard/stats", nil, http.StatusOK, &st)
	return st
}

func (e *scaleEnv) ipPage(query string) scalePage {
	e.t.Helper()
	var pg scalePage
	start := time.Now()
	e.call(http.MethodGet, "/api/v1/ip-records?"+query, nil, http.StatusOK, &pg)
	if d := time.Since(start); d > e.slowestPage {
		e.slowestPage = d
	}
	return pg
}

// listAllIPRecords 以 page_size=200（API 上限）逐頁讀完所有 IP 紀錄，以 IP 為鍵；重複的 IP 直接讓測試失敗。
func (e *scaleEnv) listAllIPRecords() map[string]scaleIPRow {
	e.t.Helper()
	all := make(map[string]scaleIPRow)
	page := 1
	for {
		pg := e.ipPage(fmt.Sprintf("page=%d&page_size=200", page))
		for _, row := range pg.Items {
			if _, dup := all[row.IPAddress]; dup {
				e.t.Fatalf("分頁讀取出現重複的 IP %s（第 %d 頁）", row.IPAddress, page)
			}
			all[row.IPAddress] = row
		}
		if page >= pg.Pagination.TotalPages {
			if len(all) != pg.Pagination.TotalItems {
				e.t.Fatalf("逐頁讀到 %d 筆，但分頁資訊的 total_items 是 %d", len(all), pg.Pagination.TotalItems)
			}
			return all
		}
		page++
	}
}

// requireIPRecordsExactly 嚴格比對：API 讀到的 IP 紀錄必須與預期完全一致（不多、不少、內容正確）。
// presenceByKey 可指定某些 VRF 來源預期的在線狀態，沒指定的一律預期 online。
func (e *scaleEnv) requireIPRecordsExactly(topo *scaleTopology, got map[string]scaleIPRow, wantPresence func(scaleKey) string) {
	e.t.Helper()
	var missing, extra, wrong []string
	for ip, want := range topo.expected {
		row, ok := got[ip]
		if !ok {
			missing = append(missing, ip)
			continue
		}
		wp := "online"
		if wantPresence != nil {
			wp = wantPresence(want.key)
		}
		switch {
		case !strings.EqualFold(row.MACAddress, want.mac):
			wrong = append(wrong, fmt.Sprintf("%s: MAC %s != %s", ip, row.MACAddress, want.mac))
		case row.SubnetCIDR != want.cidr:
			wrong = append(wrong, fmt.Sprintf("%s: 網段 %s != %s", ip, row.SubnetCIDR, want.cidr))
		case row.Presence != wp:
			wrong = append(wrong, fmt.Sprintf("%s: 在線狀態 %s != %s（來源 %s/%s）", ip, row.Presence, wp, want.key.mgmtIP, want.key.vrfName))
		case row.Registration != "unregistered":
			wrong = append(wrong, fmt.Sprintf("%s: 登記狀態 %s != unregistered", ip, row.Registration))
		case row.HasConflict:
			wrong = append(wrong, fmt.Sprintf("%s: 不應有衝突", ip))
		case wp == "online" && (len(row.SwitchNames) != 1 || row.SwitchNames[0] != want.switchName):
			wrong = append(wrong, fmt.Sprintf("%s: 來源交換器 %v != [%s]", ip, row.SwitchNames, want.switchName))
		}
	}
	for ip := range got {
		if _, ok := topo.expected[ip]; !ok {
			extra = append(extra, ip)
		}
	}
	head := func(xs []string) []string {
		if len(xs) > 10 {
			return xs[:10]
		}
		return xs
	}
	if len(missing)+len(extra)+len(wrong) > 0 {
		e.t.Fatalf("IP 紀錄與預期不一致（預期 %d 筆，實際 %d 筆）：缺少 %d 筆 %v；多出 %d 筆 %v；內容錯誤 %d 筆 %v",
			len(topo.expected), len(got), len(missing), head(missing), len(extra), head(extra), len(wrong), head(wrong))
	}
}

// onlineByKey 統計每個讀取來源（交換器管理 IP + VRF）底下目前在線的 IP 數。
func onlineByKey(topo *scaleTopology, got map[string]scaleIPRow) map[scaleKey]int {
	out := make(map[scaleKey]int)
	for ip, want := range topo.expected {
		if row, ok := got[ip]; ok && row.Presence == "online" {
			out[want.key]++
		}
	}
	return out
}

// runCycle 執行並計時一整輪 RunCycle（含真實 Scope 與 Reconciliation）；任何錯誤都讓測試失敗。
func (e *scaleEnv) runCycle(label string) time.Duration {
	e.t.Helper()
	start := time.Now()
	err := e.sched.RunCycle(e.ctx)
	elapsed := time.Since(start)
	scopeD, reconD := e.timers.last()
	e.t.Logf("[量測] %s：RunCycle 共 %v（其中 Scope %v、Reconciliation %v）", label, elapsed.Round(time.Millisecond), scopeD.Round(time.Millisecond), reconD.Round(time.Millisecond))
	if err != nil {
		e.t.Fatalf("%s：RunCycle 不應回傳錯誤，實際: %v", label, err)
	}
	if v := e.col.contractViolations(); len(v) > 0 {
		e.t.Fatalf("%s：採集替身收到違反合約的輸入: %v", label, v)
	}
	return elapsed
}

// requireVRFStatus 以 /switches 的讀取狀態驗證單一 VRF。
func requireVRFStatus(t *testing.T, views map[scaleKey]scaleVRFView, key scaleKey, wantStatus, wantErrClass string) scaleVRFView {
	t.Helper()
	v, ok := views[key]
	if !ok {
		t.Fatalf("/switches 找不到 %s/%s", key.mgmtIP, key.vrfName)
	}
	if v.LastStatus != wantStatus || v.LastErrorClass != wantErrClass {
		t.Fatalf("%s/%s 讀取狀態應為 %q/%q，實際 %q/%q", key.mgmtIP, key.vrfName, wantStatus, wantErrClass, v.LastStatus, v.LastErrorClass)
	}
	return v
}

// =============================================================================
// 失敗隔離（小拓樸）：壞輸入與讀取失敗的 VRF 不得影響其他 VRF，也不得讓 IP 誤判離線
// =============================================================================

func TestScalePollCycle_FaultyVRFsDoNotAffectOthers(t *testing.T) {
	env := newScaleEnv(t)
	env.bootstrapAdmin()
	topo := env.buildTopology(scaleSpec{switches: 8, vrfsPerSwitch: 4, hostsPerVRF: 25})
	allVRFs := topo.allVRFs()
	if len(allVRFs) != 32 || len(topo.expected) != 800 {
		t.Fatalf("拓樸應為 32 個 VRF／800 個 IP，實際 %d／%d", len(allVRFs), len(topo.expected))
	}

	// 各種故障角色；刻意讓故障 VRF 與健康 VRF 同在一台交換器上，驗證同機隔離
	garbageVRF := topo.switches[0].vrfs[0] // 成功，但夾帶無法解析的觀測與無效的介面位址
	timeoutVRF := topo.switches[1].vrfs[1] // 持續逾時
	authVRF := topo.switches[2].vrfs[2]    // 持續認證失敗
	flakyVRF := topo.switches[3].vrfs[3]   // 只失敗第一次，重試成功
	silentVRF := topo.switches[4].vrfs[1]  // 讀取成功但 ARP 表真的是空的（對照組）
	if garbageVRF.name != "default" {
		t.Fatalf("測試前提: 壞輸入應放在 default VRF，實際 %s", garbageVRF.name)
	}

	garbageObs := []interfaces.ARPObservation{
		{IPAddress: "not-an-ip", MACAddress: "02:de:ad:00:00:01", IfIndex: 1, ARPType: "dynamic"},
		{IPAddress: "", MACAddress: "02:de:ad:00:00:02", IfIndex: 1, ARPType: "dynamic"},
		{IPAddress: "999.1.1.1", MACAddress: "02:de:ad:00:00:03", IfIndex: 1, ARPType: "dynamic"},
		{IPAddress: "10.1.0.300", MACAddress: "02:de:ad:00:00:04", IfIndex: 1, ARPType: "dynamic"},
		{IPAddress: "2001:db8::1", MACAddress: "02:de:ad:00:00:05", IfIndex: 1, ARPType: "dynamic"},
	}
	env.col.addGarbage(garbageVRF.key, garbageObs...)
	env.col.addBadAddresses(garbageVRF.key,
		interfaces.InterfaceAddress{IPAddress: "not-an-ip", NetMask: "255.255.255.0", IfIndex: 2001, IfName: "Vlan901"},
		interfaces.InterfaceAddress{IPAddress: "10.200.0.1", NetMask: "255.0.255.0", IfIndex: 2002, IfName: "Vlan902"},
		interfaces.InterfaceAddress{IPAddress: "10.200.1.1", NetMask: "255.255.255.255", IfIndex: 2003, IfName: "Vlan903"},
		interfaces.InterfaceAddress{IPAddress: "10.200.2.1", NetMask: "bad-mask", IfIndex: 2004, IfName: "Vlan904"},
	)

	// 第 1 輪（探索）：只有介面位址；32 個網段全是待檢視，尚無 IP 紀錄
	env.col.setPhase(scalePhaseDiscover)
	env.runCycle("探索輪")
	pending, subnetRows := env.listSubnets()
	if pending != 32 || len(subnetRows) != 32 {
		t.Fatalf("探索後應有 32 個待檢視網段（壞的介面位址不得產生網段），實際 pending=%d 總數=%d", pending, len(subnetRows))
	}
	if st := env.stats(); st.ManagedTotal != 0 {
		t.Fatalf("網段尚未納管前不應有 IP 紀錄，實際 %d", st.ManagedTotal)
	}
	if n := env.markAllSubnetsManaged(); n != 32 {
		t.Fatalf("應將 32 個網段設為納管，實際 %d", n)
	}

	// 第 2 輪：基準輪，全部健康（含壞輸入 VRF）
	env.advance(3 * time.Minute)
	env.col.setPhase(scalePhasePoll)
	env.col.resetCalls()
	env.runCycle("基準輪")
	got := env.listAllIPRecords()
	env.requireIPRecordsExactly(topo, got, nil)
	views := env.vrfViews()
	for _, v := range allVRFs {
		requireVRFStatus(t, views, v.key, "success", "")
	}
	wantLearned := len(garbageVRF.fixture.hosts) + len(garbageVRF.fixture.extra) + len(garbageObs)
	if gv := views[garbageVRF.key]; gv.LastLearnedCount != wantLearned {
		t.Fatalf("壞輸入 VRF 的讀取筆數應為 %d，實際 %d", wantLearned, gv.LastLearnedCount)
	}
	if _, rows := env.listSubnets(); len(rows) != 32 {
		t.Fatalf("壞輸入不得產生新網段，網段數應維持 32，實際 %d", len(rows))
	}

	// 第 3～6 輪：三種故障 + 一個對照組，連續 4 輪（超過寬限輪數預設值 2）
	env.col.failVRF(timeoutVRF.key, -1, &interfaces.CollectError{Class: interfaces.ErrClassTimeout, Summary: "SNMP 逾時"})
	env.col.failVRF(authVRF.key, -1, &interfaces.CollectError{Class: interfaces.ErrClassAuthFailed, Summary: "認證失敗"})
	env.col.failVRF(flakyVRF.key, 1, &interfaces.CollectError{Class: interfaces.ErrClassTimeout, Summary: "暫時性逾時"})
	env.col.setSilent(silentVRF.key, true)

	for round := 1; round <= 4; round++ {
		env.advance(3 * time.Minute)
		env.col.resetCalls()
		env.runCycle(fmt.Sprintf("故障第 %d 輪", round))

		for _, v := range allVRFs {
			want := 1
			switch v.key {
			case timeoutVRF.key, authVRF.key:
				want = 2 // 失敗後重試一次，總嘗試次數固定為 2
			case flakyVRF.key:
				if round == 1 {
					want = 2
				}
			}
			if n := env.col.callCount(v.key); n != want {
				t.Fatalf("故障第 %d 輪 %s/%s 應被讀取 %d 次，實際 %d", round, v.key.mgmtIP, v.key.vrfName, want, n)
			}
		}

		views = env.vrfViews()
		for _, v := range allVRFs {
			switch v.key {
			case timeoutVRF.key:
				requireVRFStatus(t, views, v.key, "failed", "timeout")
			case authVRF.key:
				requireVRFStatus(t, views, v.key, "failed", "auth_failed")
			default:
				requireVRFStatus(t, views, v.key, "success", "")
			}
		}
		if st := env.stats(); st.FailedVRFCount != 2 {
			t.Fatalf("故障第 %d 輪應有 2 個讀取失敗的 VRF，實際 %d", round, st.FailedVRFCount)
		}

		// 讀取失敗的 VRF：IP 一律維持在線，不論失敗多少輪
		online := onlineByKey(topo, env.listAllIPRecords())
		for _, bad := range []*scaleVRF{timeoutVRF, authVRF} {
			if online[bad.key] != 25 {
				t.Fatalf("故障第 %d 輪：讀取失敗的 %s/%s 其 IP 不得誤判離線，在線數應為 25，實際 %d", round, bad.key.mgmtIP, bad.key.vrfName, online[bad.key])
			}
		}
		// 其餘健康 VRF 不受影響；對照組在寬限輪數內仍在線，超過後才轉離線
		for _, v := range allVRFs {
			if v.key == silentVRF.key {
				continue
			}
			if online[v.key] != 25 {
				t.Fatalf("故障第 %d 輪：%s/%s 不應受其他 VRF 故障影響，在線數應為 25，實際 %d", round, v.key.mgmtIP, v.key.vrfName, online[v.key])
			}
		}
		if round == 4 && online[silentVRF.key] != 0 {
			t.Fatalf("對照組：成功讀取但連續 %d 輪看不到的 %s/%s 應已轉離線，實際在線 %d", round, silentVRF.key.mgmtIP, silentVRF.key.vrfName, online[silentVRF.key])
		}
	}

	// 第 7 輪：全部恢復；IP 紀錄仍是同樣 800 筆、沒有重複，且全部回到在線
	env.col.clearFailures()
	env.col.setSilent(silentVRF.key, false)
	env.advance(3 * time.Minute)
	env.col.resetCalls()
	env.runCycle("恢復輪")
	got = env.listAllIPRecords()
	env.requireIPRecordsExactly(topo, got, nil)
	views = env.vrfViews()
	for _, v := range allVRFs {
		requireVRFStatus(t, views, v.key, "success", "")
		if n := env.col.callCount(v.key); n != 1 {
			t.Fatalf("恢復輪 %s/%s 應只讀取 1 次，實際 %d", v.key.mgmtIP, v.key.vrfName, n)
		}
	}
	if st := env.stats(); st.FailedVRFCount != 0 || st.ManagedTotal != 800 || st.OnlineCount != 800 || st.OpenConflicts != 0 {
		t.Fatalf("恢復後統計不符: %+v", st)
	}
}

// =============================================================================
// 整輪規模驗收：50 台交換器 × 每台 4 個 VRF（含 default）= 200 個讀取設定 × 每個 125 台主機 = 25,000 筆 IP
// =============================================================================

const (
	// 這兩個門檻是「測試環境」的量測上限，不是對實機 SNMP 輪詢效能的承諾：
	// 採集替身不含網路延遲，量到的只是資料庫 + 後端處理的成本。
	scaleCycleBudget = 3 * time.Minute
	scaleQueryBudget = 5 * time.Second
)

// scaleTimedGet 以管理員工作階段計時一次 GET，並要求回應為 200。
func (e *scaleEnv) scaleTimedGet(path string, out any) time.Duration {
	e.t.Helper()
	start := time.Now()
	e.call(http.MethodGet, path, nil, http.StatusOK, out)
	return time.Since(start)
}

// TestScalePollCycle_FullCycle_50Switches200VRFs25kIPs 是設計目標規模的整輪驗收：
// 50 台（IOS-XE 與 NX-OS 各半）、每台 4 個 VRF（含 default）、共 200 個讀取設定、25,000 筆 IP。
func TestScalePollCycle_FullCycle_50Switches200VRFs25kIPs(t *testing.T) {
	scaleRunAcceptance(t, scaleSpec{switches: 50, vrfsPerSwitch: 4, hostsPerVRF: 125})
}

// TestScalePollCycle_FullCycle_SmallTopology 走同一套驗收流程，但拓樸小很多，
// 用來在不花整輪成本的情況下確認夾具與斷言本身是對的；它不代表規模驗收。
func TestScalePollCycle_FullCycle_SmallTopology(t *testing.T) {
	scaleRunAcceptance(t, scaleSpec{switches: 6, vrfsPerSwitch: 4, hostsPerVRF: 20})
}

func scaleRunAcceptance(t *testing.T, spec scaleSpec) {
	t.Helper()
	env := newScaleEnv(t)
	env.bootstrapAdmin()

	t.Logf("[環境] 測試環境量測，不含實機 SNMP 延遲：CPU=%d，GOMAXPROCS=%d，MariaDB=mariadb:11.4（單機 Docker 容器）", runtime.NumCPU(), runtime.GOMAXPROCS(0))

	// ---- 建立拓樸（全部經 HTTP API）----
	buildStart := time.Now()
	topo := env.buildTopology(spec)
	t.Logf("[量測] 經 HTTP 建立 %d 台交換器、%d 個 VRF 讀取設定：%v", len(topo.switches), topo.vrfCount(), time.Since(buildStart).Round(time.Millisecond))

	wantSwitches := spec.switches
	wantVRFs := spec.switches * spec.vrfsPerSwitch
	wantIPs := wantVRFs * spec.hostsPerVRF
	perSwitch := spec.vrfsPerSwitch * spec.hostsPerVRF
	if len(topo.switches) != wantSwitches || topo.vrfCount() != wantVRFs || len(topo.expected) != wantIPs {
		t.Fatalf("拓樸應為 %d 台／%d 個 VRF／%d 個 IP，實際 %d／%d／%d", wantSwitches, wantVRFs, wantIPs, len(topo.switches), topo.vrfCount(), len(topo.expected))
	}
	platforms := map[string]int{}
	for _, sw := range topo.switches {
		platforms[sw.platform]++
		if len(sw.vrfs) != spec.vrfsPerSwitch || sw.vrfs[0].name != "default" {
			t.Fatalf("交換器 %s 應有 %d 個 VRF 且第一個是 default，實際 %d 個", sw.name, spec.vrfsPerSwitch, len(sw.vrfs))
		}
	}
	if platforms["ios-xe"] != wantSwitches-wantSwitches/2 || platforms["nx-os"] != wantSwitches/2 {
		t.Fatalf("兩種平台應各占一半，實際 %v", platforms)
	}
	wantCIDRs := make(map[string]bool, wantVRFs)
	allVRFs := topo.allVRFs()
	for _, v := range allVRFs {
		wantCIDRs[v.cidr] = true
	}
	if len(wantCIDRs) != wantVRFs {
		t.Fatalf("%d 個 VRF 的網段不得重疊，實際只有 %d 個不同網段", wantVRFs, len(wantCIDRs))
	}

	// ---- 第一次探索輪：只回報介面位址，讓網段被發現 ----
	env.col.setPhase(scalePhaseDiscover)
	env.col.resetCalls()
	discoverD := env.runCycle(fmt.Sprintf("探索輪（%d 個 VRF，只有介面位址）", wantVRFs))
	if n := env.col.totalCalls(); n != wantVRFs {
		t.Fatalf("探索輪應讀取 %d 次，實際 %d", wantVRFs, n)
	}
	views := env.vrfViews()
	if len(views) != wantVRFs {
		t.Fatalf("/switches 應列出 %d 個 VRF 讀取設定，實際 %d", wantVRFs, len(views))
	}
	for _, v := range allVRFs {
		requireVRFStatus(t, views, v.key, "success", "")
	}
	pending, subnetRows := env.listSubnets()
	if pending != wantVRFs || len(subnetRows) != wantVRFs {
		t.Fatalf("探索後應有 %d 個待檢視網段，實際 pending=%d 總數=%d", wantVRFs, pending, len(subnetRows))
	}
	for _, r := range subnetRows {
		if !wantCIDRs[r.CIDR] || r.State != "pending" || r.IPCount != 0 {
			t.Fatalf("探索到的網段不符預期: %+v", r)
		}
	}
	if st := env.stats(); st.ManagedTotal != 0 {
		t.Fatalf("網段尚未納管前不得有 IP 紀錄，實際 %d", st.ManagedTotal)
	}

	// ---- 經 API 把網段設為納管 ----
	if n := env.markAllSubnetsManaged(); n != wantVRFs {
		t.Fatalf("應把 %d 個網段設為納管，實際 %d", wantVRFs, n)
	}

	// ---- 計時的整輪：RunCycle 內含真實 Scope -> Reconciliation，首次落地 25,000 筆 ----
	env.advance(3 * time.Minute)
	env.col.setPhase(scalePhasePoll)
	env.col.resetCalls()
	cycleD := env.runCycle(fmt.Sprintf("整輪（首次落地 %d 筆）", wantIPs))
	if cycleD > scaleCycleBudget {
		t.Errorf("整輪耗時 %v 超過測試環境門檻 %v", cycleD, scaleCycleBudget)
	}

	// 採集呼叫：每個 VRF 讀取設定剛好一次（全部成功，不需重試）
	if n := env.col.totalCalls(); n != wantVRFs {
		t.Fatalf("整輪應讀取 %d 次，實際 %d", wantVRFs, n)
	}
	for _, v := range allVRFs {
		if n := env.col.callCount(v.key); n != 1 {
			t.Fatalf("%s/%s 應只被讀取 1 次，實際 %d", v.key.mgmtIP, v.key.vrfName, n)
		}
	}

	// 每個來源（交換器 + VRF）的讀取結果：200 個全部成功，讀取筆數 = 主機 + 閘道 ARP（+ default 的範圍外雜訊）
	views = env.vrfViews()
	for _, v := range allVRFs {
		got := requireVRFStatus(t, views, v.key, "success", "")
		if want := len(v.fixture.hosts) + len(v.fixture.extra); got.LastLearnedCount != want {
			t.Fatalf("%s/%s 讀取筆數應為 %d，實際 %d", v.key.mgmtIP, v.key.vrfName, want, got.LastLearnedCount)
		}
	}

	// 全部 IP：逐頁（page_size=200，API 上限）讀回，嚴格比對不多、不少、內容正確
	env.slowestPage = 0
	listStart := time.Now()
	got := env.listAllIPRecords()
	t.Logf("[量測] 逐頁讀完全部 IP 紀錄（%d 頁，每頁 200 筆）：共 %v，最慢一頁 %v", (wantIPs+199)/200, time.Since(listStart).Round(time.Millisecond), env.slowestPage.Round(time.Millisecond))
	if len(got) != wantIPs {
		t.Fatalf("應有 %d 筆 IP 紀錄，實際 %d", wantIPs, len(got))
	}
	env.requireIPRecordsExactly(topo, got, nil)
	for _, v := range allVRFs {
		if _, ok := got[v.fixture.ifAddr.IPAddress]; ok {
			t.Fatalf("閘道位址 %s 不得產生 IP 紀錄", v.fixture.ifAddr.IPAddress)
		}
		for _, x := range v.fixture.extra {
			if _, ok := got[x.IPAddress]; ok {
				t.Fatalf("非主機項目 %s（閘道或範圍外雜訊）不得計入有效規模", x.IPAddress)
			}
		}
	}

	// 每個網段（= 每個讀取來源）：全部主機都有紀錄、全部在線
	pending, subnetRows = env.listSubnets()
	if pending != 0 || len(subnetRows) != wantVRFs {
		t.Fatalf("納管後應有 %d 個網段且無待檢視，實際 pending=%d 總數=%d", wantVRFs, pending, len(subnetRows))
	}
	for _, r := range subnetRows {
		if !wantCIDRs[r.CIDR] || r.State != "managed" || r.IPCount != spec.hostsPerVRF || r.OnlineCount != spec.hostsPerVRF {
			t.Fatalf("網段 %s 應為 managed 且 %d 筆全在線，實際 %+v", r.CIDR, spec.hostsPerVRF, r)
		}
	}

	// 每台交換器：VRF 數 × 每個 VRF 的主機數，經 switch_id 篩選
	for _, sw := range topo.switches {
		pg := env.ipPage(fmt.Sprintf("switch_id=%d&page=1&page_size=1", sw.id))
		if pg.Pagination.TotalItems != perSwitch {
			t.Fatalf("交換器 %s 應看到 %d 筆 IP，實際 %d", sw.name, perSwitch, pg.Pagination.TotalItems)
		}
	}

	// 儀表板統計
	st := env.stats()
	if st.ManagedTotal != wantIPs || st.OnlineCount != wantIPs || st.UnregisteredOnline != wantIPs ||
		st.OfflineCount != 0 || st.ExpiredCount != 0 || st.OpenConflicts != 0 ||
		st.SwitchesCount != wantSwitches || st.FailedVRFCount != 0 {
		t.Fatalf("儀表板統計不符預期: %+v", st)
	}

	// ---- 首頁查詢耗時（資料已有 25,000 筆；測試環境量測）----
	firstSw, firstVRF := topo.switches[0], topo.switches[0].vrfs[0]
	// 關鍵字搜尋取最後一台的第 3 個 VRF 網段前綴（10.{序號+1}.2.），應剛好命中該網段的主機
	lastSw := topo.switches[len(topo.switches)-1]
	searchPrefix := fmt.Sprintf("10.%d.2.", lastSw.index+1)
	lastPage := (wantIPs + 49) / 50
	queries := []struct {
		name      string
		path      string
		wantTotal int // <0 表示不檢查
		isIPPage  bool
	}{
		{"首頁 KPI（/dashboard/stats）", "/api/v1/dashboard/stats", -1, false},
		{"首頁 IP 表第 1 頁（預設排序，50 筆）", "/api/v1/ip-records?page=1&page_size=50", wantIPs, true},
		{"IP 表：在線且未登記", "/api/v1/ip-records?presence=online&registration=unregistered&page=1&page_size=50", wantIPs, true},
		{"IP 表：依最後見到時間遞減", "/api/v1/ip-records?sort_by=last_seen_at&sort_dir=desc&page=1&page_size=50", wantIPs, true},
		{"IP 表：關鍵字搜尋 " + searchPrefix, "/api/v1/ip-records?search=" + searchPrefix + "&page=1&page_size=50", spec.hostsPerVRF, true},
		{"IP 表：單一交換器篩選", fmt.Sprintf("/api/v1/ip-records?switch_id=%d&page=1&page_size=50", firstSw.id), perSwitch, true},
		{"IP 表：單一網段篩選", fmt.Sprintf("/api/v1/ip-records?subnet_id=%d&page=1&page_size=50", subnetIDByCIDR(subnetRows, firstVRF.cidr)), spec.hostsPerVRF, true},
		{"IP 表：最後一頁（偏移最大）", fmt.Sprintf("/api/v1/ip-records?page=%d&page_size=50", lastPage), wantIPs, true},
	}
	for _, q := range queries {
		var d time.Duration
		if q.isIPPage {
			var pg scalePage
			d = env.scaleTimedGet(q.path, &pg)
			if q.wantTotal >= 0 && pg.Pagination.TotalItems != q.wantTotal {
				t.Fatalf("%s：total_items 應為 %d，實際 %d", q.name, q.wantTotal, pg.Pagination.TotalItems)
			}
			if len(pg.Items) == 0 {
				t.Fatalf("%s：回傳的筆數不應為 0", q.name)
			}
		} else {
			d = env.scaleTimedGet(q.path, nil)
		}
		t.Logf("[量測] %s：%v", q.name, d.Round(time.Millisecond))
		if d > scaleQueryBudget {
			t.Errorf("%s 耗時 %v 超過測試環境門檻 %v", q.name, d, scaleQueryBudget)
		}
	}
	if env.slowestPage > scaleQueryBudget {
		t.Errorf("逐頁讀取時最慢一頁 %v 超過測試環境門檻 %v", env.slowestPage, scaleQueryBudget)
	}

	// ---- 穩態輪：資料已在，再跑一整輪（更新而非新增），數量與紀錄身分都不得變動 ----
	env.advance(3 * time.Minute)
	env.col.resetCalls()
	steadyD := env.runCycle(fmt.Sprintf("穩態輪（%d 筆已存在）", wantIPs))
	if steadyD > scaleCycleBudget {
		t.Errorf("穩態輪耗時 %v 超過測試環境門檻 %v", steadyD, scaleCycleBudget)
	}
	if n := env.col.totalCalls(); n != wantVRFs {
		t.Fatalf("穩態輪應讀取 %d 次，實際 %d", wantVRFs, n)
	}
	steady := env.listAllIPRecords()
	env.requireIPRecordsExactly(topo, steady, nil)
	for ip, row := range got {
		if steady[ip].ID != row.ID {
			t.Fatalf("穩態輪不得重建 IP 紀錄：%s 的 ID 由 %d 變成 %d", ip, row.ID, steady[ip].ID)
		}
	}
	if st := env.stats(); st.ManagedTotal != wantIPs || st.OnlineCount != wantIPs || st.OpenConflicts != 0 || st.FailedVRFCount != 0 {
		t.Fatalf("穩態輪後統計不符預期: %+v", st)
	}

	t.Logf("[摘要] 測試環境量測（不含實機 SNMP 延遲）：探索輪 %v；整輪（首次落地 %d 筆）%v（門檻 %v）；穩態輪 %v；首頁查詢門檻 %v",
		discoverD.Round(time.Millisecond), wantIPs, cycleD.Round(time.Millisecond), scaleCycleBudget, steadyD.Round(time.Millisecond), scaleQueryBudget)
}

func subnetIDByCIDR(rows []scaleSubnetRow, cidr string) int64 {
	for _, r := range rows {
		if r.CIDR == cidr {
			return r.ID
		}
	}
	return 0
}
