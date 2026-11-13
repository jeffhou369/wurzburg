package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/accounts"
	"github.com/jeffhou369/wurzburg/backend/internal/app"
	"github.com/jeffhou369/wurzburg/backend/internal/auth"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/config"
	"github.com/jeffhou369/wurzburg/backend/internal/server"
)

// 端到端測試環境。
//
// 規則（第 33、41 張工作項目）：從一套空的系統開始，所有狀態都由 API 的操作與時鐘的推進產生。
//   - 系統以正式環境的方式組裝與啟動：套用遷移、依設定建立初始系統管理員、app.Mount 掛載全部模組。
//     AppEnv 不是 test，測試身分標頭（X-Test-*）不會生效；每個請求都帶登入取得的 cookie。
//   - 外部系統是模擬伺服器，經過真正的用戶端模組：SNMP 採集（gosnmp → snmpsim）、
//     AD 驗證（go-ldap → OpenLDAP）、SMTP 寄信（net/smtp → Mailpit）。
//   - 排程到期的工作不靠背景迴圈的實際時間，而是推進測試時鐘後呼叫與背景迴圈相同的入口：
//     Scheduler.RunCycle、Notifications.Tick、Maintenance.Tick。
//   - 情境程式只透過 e2eSession 送 HTTP 請求、透過模擬伺服器改變外部世界、透過 e2eEnv 推進時間；
//     不直接讀寫資料庫，也不呼叫內部服務。驗證結果只看 API 回應、收到的信與匯出檔。
type e2eEnv struct {
	t      *testing.T
	ctx    context.Context
	clk    *clock.TestClock
	loc    *time.Location
	router http.Handler
	mods   *app.Modules

	snmp *snmpSim
	ldap *ldapSim
	mail *mailSim
}

const (
	e2eSecretKey       = "0123456789abcdef0123456789abcdef"
	e2eAdminUser       = "admin"
	e2eAdminInitialPW  = "InitialAdminPass#41"
	e2eAdminPW         = "ChangedAdminPass#2026"
	e2ePublicBaseURL   = "https://ip.campus.test"
	e2eAdminMailbox    = "netadmin@campus.test"
	e2eSenderMailbox   = "ipmgmt@campus.test"
	e2eIOSXECommunity  = "iosxe-ro-community"
	e2eNXOSCommunity   = "nxos-ro-community"
	e2eUser01Password  = "SimUserPass#01"
	e2eUser02Password  = "SimUserPass#02"
	e2eOutsiderPass    = "SimUserPass#99"
	e2eUser01Mail      = "user01@campus.test"
	e2eUser02Mail      = "user02@campus.test"
	e2eOutsiderMailbox = "outsider@campus.test"
)

// startE2E 啟動資料庫、三個模擬伺服器與一套以正式方式組裝的系統。
// snmpInitial 是 SNMP 模擬器啟動時的資料檔（community 或 context → snmprec）。
func startE2E(t *testing.T, start time.Time, snmpInitial map[string][]byte) *e2eEnv {
	t.Helper()
	ctx := context.Background()
	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		t.Fatalf("載入時區失敗: %v", err)
	}

	e := &e2eEnv{t: t, ctx: ctx, loc: loc, clk: clock.NewTestClock(start)}
	e.snmp = startSNMPSim(t, snmpInitial)
	e.ldap = startLDAPSim(t, []ldapSimUser{
		{Username: "user01", Password: e2eUser01Password, DisplayName: "王工程師", Mail: e2eUser01Mail, InGroup: true},
		{Username: "user02", Password: e2eUser02Password, DisplayName: "李工程師", Mail: e2eUser02Mail, InGroup: true},
		{Username: "outsider", Password: e2eOutsiderPass, DisplayName: "外部人員", Mail: e2eOutsiderMailbox, InGroup: false},
	})
	e.mail = startMailSim(t)

	// 以下與 cmd/wurzburg 的啟動流程相同：遷移（startContractDB 內）、建立初始管理員、組裝模組。
	db := startContractDB(t)
	cfg := &config.Config{
		Port: 8080, CampusName: "Würzburg", Timezone: "Asia/Taipei", AppEnv: "production",
		SecretKey:     e2eSecretKey,
		AdminUsername: e2eAdminUser,
		AdminPassword: useSecret(t, "初始管理員密碼", e2eAdminInitialPW),
	}
	if err := accounts.EnsureFirstAdmin(ctx, db, e.clk, cfg.AdminUsername, cfg.AdminPassword); err != nil {
		t.Fatalf("建立初始管理員失敗: %v", err)
	}
	srv := server.New(cfg, db, e.clk)
	e.mods = app.Mount(ctx, srv, db, e.clk)
	e.router = apiRouter(t, srv)

	// 模擬伺服器端的 Secret 不經由本系統的 API 建立，另外登錄到掃描器。
	for label, v := range map[string]string{
		"SNMPv3 驗證密碼": simV3AuthPass, "SNMPv3 加密密碼": simV3PrivPass,
		"LDAP 服務帳號密碼": ldapSimBindPassword, "LDAP 管理者密碼": ldapSimRootPassword,
		"SMTP 密碼":        mailSimPassword,
		"組織帳號密碼（user01）": e2eUser01Password, "組織帳號密碼（user02）": e2eUser02Password, "組織帳號密碼（outsider）": e2eOutsiderPass,
	} {
		useSecret(t, label, v)
	}
	return e
}

// at 回傳園區時區的時間點。
func (e *e2eEnv) at(year int, month time.Month, day, hour, min, sec int) time.Time {
	return time.Date(year, month, day, hour, min, sec, 0, e.loc)
}

// poll 推進 3 分鐘並執行一輪 SNMP 輪詢，與背景迴圈每 3 分鐘做的事相同。
func (e *e2eEnv) poll() {
	e.t.Helper()
	e.clk.Advance(3 * time.Minute)
	if err := e.mods.Scheduler.RunCycle(e.ctx); err != nil {
		e.t.Fatalf("輪詢週期無法執行: %v", err)
	}
}

// tick 依目前的時鐘觸發到期的通知與維護工作，與背景迴圈定時做的事相同。
func (e *e2eEnv) tick() {
	e.t.Helper()
	e.mods.Notifications.Tick(e.ctx)
	if err := e.mods.Maintenance.Tick(e.ctx); err != nil {
		e.t.Fatalf("每日維護失敗: %v", err)
	}
}

// advanceTo 把時鐘推進到指定時間並觸發到期的工作。時間只能往前。
func (e *e2eEnv) advanceTo(tm time.Time) {
	e.t.Helper()
	if tm.Before(e.clk.Now()) {
		e.t.Fatalf("時鐘不可倒退：目前 %s，目標 %s", e.clk.Now().In(e.loc), tm.In(e.loc))
	}
	e.clk.Set(tm)
	e.tick()
}

// e2eSession 是一個登入中的使用者。請求一律帶登入取得的 cookie。
type e2eSession struct {
	env      *e2eEnv
	org      bool // true：組織帳號（AD）；false：本機系統管理員
	username string
	password string
	cookie   *http.Cookie
}

func (e *e2eEnv) rawRequest(method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			e.t.Fatalf("編碼請求本文失敗: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

// loginAttempt 送出登入請求並回傳回應；成功時回傳工作階段 cookie。
func (e *e2eEnv) loginAttempt(org bool, username, password string) (*httptest.ResponseRecorder, *http.Cookie) {
	path := "/api/v1/auth/login/local"
	if org {
		path = "/api/v1/auth/login/org"
	}
	rec := e.rawRequest(http.MethodPost, path, map[string]string{"username": username, "password": password}, nil)
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			return rec, c
		}
	}
	return rec, nil
}

// login 登入並回傳工作階段；登入失敗時測試中止。
func (e *e2eEnv) login(org bool, username, password string) *e2eSession {
	e.t.Helper()
	s := &e2eSession{env: e, org: org, username: username, password: password}
	s.relogin()
	return s
}

func (s *e2eSession) relogin() {
	s.env.t.Helper()
	rec, cookie := s.env.loginAttempt(s.org, s.username, s.password)
	if rec.Code != http.StatusOK || cookie == nil {
		s.env.t.Fatalf("%s 登入失敗: %d %s", s.username, rec.Code, rec.Body.String())
	}
	s.cookie = cookie
}

// do 送出請求。時鐘大幅推進後工作階段會逾時：收到 401 時重新登入一次再重送，和使用者在瀏覽器上被要求重新登入相同。
func (s *e2eSession) do(method, path string, body any) *httptest.ResponseRecorder {
	s.env.t.Helper()
	rec := s.env.rawRequest(method, path, body, s.cookie)
	if rec.Code == http.StatusUnauthorized {
		s.relogin()
		rec = s.env.rawRequest(method, path, body, s.cookie)
	}
	return rec
}

// must 送出請求並要求指定的狀態碼，回傳解析後的 JSON 物件（沒有本文時為 nil）。
func (s *e2eSession) must(method, path string, body any, want int) map[string]any {
	s.env.t.Helper()
	rec := s.do(method, path, body)
	if rec.Code != want {
		s.env.t.Fatalf("%s %s：預期 %d，實際 %d %s", method, path, want, rec.Code, rec.Body.String())
	}
	if rec.Body.Len() == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		s.env.t.Fatalf("%s %s：回應不是 JSON 物件: %v", method, path, err)
	}
	return out
}

// e2eIP 是 IP 管理表的一列，只取情境會用到的欄位。
type e2eIP struct {
	ID            int64  `json:"id"`
	IPAddress     string `json:"ip_address"`
	MACAddress    string `json:"mac_address"`
	VendorName    string `json:"vendor_name"`
	Presence      string `json:"presence"`
	Registration  string `json:"registration"`
	ExpiringSoon  bool   `json:"expiring_soon"`
	HasConflict   bool   `json:"has_conflict"`
	StateLabel    string `json:"state_label"`
	OwnerUsername string `json:"owner_username"`
	Notes         string `json:"notes"`
}

// ipRecords 以指定的篩選條件讀完 IP 管理表的所有頁。
func (s *e2eSession) ipRecords(query url.Values) []e2eIP {
	s.env.t.Helper()
	var all []e2eIP
	for page := 1; ; page++ {
		q := url.Values{}
		for k, v := range query {
			q[k] = v
		}
		q.Set("page", fmt.Sprint(page))
		q.Set("page_size", "200")
		rec := s.do(http.MethodGet, "/api/v1/ip-records?"+q.Encode(), nil)
		if rec.Code != http.StatusOK {
			s.env.t.Fatalf("讀取 IP 管理表失敗: %d %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Items      []e2eIP `json:"items"`
			Pagination struct {
				TotalPages int `json:"total_pages"`
			} `json:"pagination"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			s.env.t.Fatalf("解析 IP 管理表失敗: %v", err)
		}
		all = append(all, resp.Items...)
		if page >= resp.Pagination.TotalPages {
			return all
		}
	}
}

// ip 回傳指定位址的 IP 紀錄；不存在時回傳 nil。
func (s *e2eSession) ip(address string) *e2eIP {
	s.env.t.Helper()
	for _, r := range s.ipRecords(url.Values{"search": {address}}) {
		if r.IPAddress == address {
			return &r
		}
	}
	return nil
}

// mustIP 回傳指定位址的 IP 紀錄；不存在時測試中止。
func (s *e2eSession) mustIP(address string) e2eIP {
	s.env.t.Helper()
	r := s.ip(address)
	if r == nil {
		s.env.t.Fatalf("IP 管理表中找不到 %s", address)
	}
	return *r
}

func ipAddresses(records []e2eIP) []string {
	out := make([]string, 0, len(records))
	for _, r := range records {
		out = append(out, r.IPAddress)
	}
	return out
}

func containsAll(text string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(text, p) {
			return false
		}
	}
	return true
}
