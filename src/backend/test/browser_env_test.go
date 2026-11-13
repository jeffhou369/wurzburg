package test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/accounts"
	"github.com/jeffhou369/wurzburg/backend/internal/app"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/config"
	"github.com/jeffhou369/wurzburg/backend/internal/server"
	"github.com/jeffhou369/wurzburg/backend/internal/static"
)

// 瀏覽器關鍵流程（第 42 張工作項目）。
//
// TestBrowserFlows 啟動一套隔離的環境，再執行 test/browser 的 Playwright 流程：
//   - 真實的 Go 後端，內嵌建置後的前端，在本機的隨機埠提供 HTTP 服務；以正式環境的方式組裝（app.Mount），
//     使用實際時間，不啟動背景迴圈（流程以畫面上的「立即同步」觸發輪詢）。
//   - MariaDB 與 SNMP、LDAP、郵件三個模擬伺服器，都是 Testcontainers 啟動的拋棄式容器，
//     不使用既有部署的容器名稱與 volume。
//   - 瀏覽器送出的每個 API 請求一樣經過 OpenAPI 回應契約檢查與 Secret 掃描。
//   - 另有一個只給 Playwright 用的控制端點（不同的埠，不屬於產品），用來改變模擬交換器回報的內容。
//
// 這個測試需要 Node.js 與 Playwright 的瀏覽器，執行時間也比較長，所以只有設定 WURZBURG_BROWSER_TESTS=1 時才執行；
// 平常以 test/browser/run.sh 啟動，它會先建置前端並內嵌進後端。

const browserTestsEnv = "WURZBURG_BROWSER_TESTS"

// browserHoldEnv 設定後不執行 Playwright，只把環境資訊寫到指定的檔案並保持環境運作，供手動探索畫面。
// 刪除該檔案或送出中斷訊號即結束。
const browserHoldEnv = "WURZBURG_BROWSER_HOLD_FILE"

// browserSwitch 是控制端點管理的模擬交換器內容。
type browserSwitch struct {
	mu         sync.Mutex
	sim        *snmpSim
	interfaces []simInterface
	hosts      map[string]simARP
}

func (b *browserSwitch) apply(t *testing.T) {
	arps := make([]simARP, 0, len(b.hosts))
	for _, a := range b.hosts {
		arps = append(arps, a)
	}
	sort.Slice(arps, func(i, j int) bool { return arps[i].IP < arps[j].IP })
	b.sim.setData(t, e2eIOSXECommunity, buildSNMPRec(b.interfaces, arps))
}

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

// requireEmbeddedFrontend 確認後端內嵌的是目前的前端建置結果。
func requireEmbeddedFrontend(t *testing.T) {
	t.Helper()
	embedded, err := fs.ReadFile(static.DistFS, "dist/index.html")
	if err != nil {
		t.Fatalf("後端沒有內嵌前端（%v）。請以 test/browser/run.sh 執行，它會先建置前端並複製到 src/backend/internal/static/dist。", err)
	}
	built, err := os.ReadFile(filepath.Join(repoRoot(), "src", "frontend", "dist", "frontend", "browser", "index.html"))
	if err != nil {
		t.Fatalf("找不到前端的建置結果（%v）。請以 test/browser/run.sh 執行。", err)
	}
	if !bytes.Equal(embedded, built) {
		t.Fatal("後端內嵌的前端不是目前的建置結果。請以 test/browser/run.sh 執行，它會重新建置並內嵌。")
	}
}

func TestBrowserFlows(t *testing.T) {
	if os.Getenv(browserTestsEnv) == "" {
		t.Skipf("瀏覽器流程需要 Node.js 與 Playwright，設定 %s=1 才執行（見 test/browser/README.md）", browserTestsEnv)
	}
	requireEmbeddedFrontend(t)
	ctx := context.Background()

	interfaces := []simInterface{
		{Index: 10, Name: "Vlan10", IP: "10.10.10.254", Mask: "255.255.255.0"},
		{Index: 30, Name: "Vlan30", IP: "10.10.30.254", Mask: "255.255.255.0"},
	}
	sw := &browserSwitch{interfaces: interfaces, hosts: map[string]simARP{}}
	for ip, mac := range map[string]string{
		"10.10.10.50": "00:0c:29:11:22:50",
		"10.10.10.51": "b8:27:eb:aa:bb:51",
		"10.10.10.52": "00:50:56:11:22:52",
		"10.10.10.53": "00:0c:29:11:22:53",
		"10.10.30.10": "00:0c:29:33:00:10",
	} {
		ifIndex := int64(10)
		if strings.HasPrefix(ip, "10.10.30.") {
			ifIndex = 30
		}
		sw.hosts[ip] = simARP{IP: ip, MAC: mac, IfIndex: ifIndex}
	}
	initial := make([]simARP, 0, len(sw.hosts))
	for _, a := range sw.hosts {
		initial = append(initial, a)
	}
	sort.Slice(initial, func(i, j int) bool { return initial[i].IP < initial[j].IP })
	// 介面上沒有 SNMP 埠的欄位，設定精靈新增的交換器一律讀 161 埠，所以模擬器綁在主機的 161/udp
	sw.sim = startSNMPSimOn(t, map[string][]byte{e2eIOSXECommunity: buildSNMPRec(interfaces, initial)}, 161)

	ldap := startLDAPSim(t, []ldapSimUser{
		{Username: "user01", Password: e2eUser01Password, DisplayName: "王工程師", Mail: e2eUser01Mail, InGroup: true},
		{Username: "user02", Password: e2eUser02Password, DisplayName: "李工程師", Mail: e2eUser02Mail, InGroup: true},
	})
	mail := startMailSim(t)
	for label, v := range map[string]string{
		"LDAP 服務帳號密碼": ldapSimBindPassword, "LDAP 管理者密碼": ldapSimRootPassword, "SMTP 密碼": mailSimPassword,
		"組織帳號密碼（user01）": e2eUser01Password, "組織帳號密碼（user02）": e2eUser02Password,
		"SNMPv3 驗證密碼": simV3AuthPass, "SNMPv3 加密密碼": simV3PrivPass,
	} {
		useSecret(t, label, v)
	}

	// 與 cmd/wurzburg 相同的啟動流程，差別只在不啟動背景迴圈
	db := startContractDB(t)
	clk := clock.NewRealClock()
	cfg := &config.Config{
		Port: 8080, CampusName: "Würzburg", Timezone: "Asia/Taipei", AppEnv: "production",
		SecretKey: e2eSecretKey, AdminUsername: e2eAdminUser,
		AdminPassword: useSecret(t, "初始管理員密碼", e2eAdminInitialPW),
	}
	if err := accounts.EnsureFirstAdmin(ctx, db, clk, cfg.AdminUsername, cfg.AdminPassword); err != nil {
		t.Fatalf("建立初始管理員失敗: %v", err)
	}
	srv := server.New(cfg, db, clk)
	app.Mount(ctx, srv, db, clk)
	appServer := httptest.NewServer(apiRouter(t, srv))
	t.Cleanup(appServer.Close)

	// 控制端點：只給 Playwright 用，改變模擬交換器回報的內容
	control := http.NewServeMux()
	control.HandleFunc("POST /snmp/hosts", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Set    map[string]string `json:"set"`    // IP → MAC：新增或更換
			Remove []string          `json:"remove"` // 不再回報的 IP
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		sw.mu.Lock()
		defer sw.mu.Unlock()
		for ip, mac := range req.Set {
			ifIndex := int64(10)
			if strings.HasPrefix(ip, "10.10.30.") {
				ifIndex = 30
			}
			sw.hosts[ip] = simARP{IP: ip, MAC: mac, IfIndex: ifIndex}
		}
		for _, ip := range req.Remove {
			delete(sw.hosts, ip)
		}
		sw.apply(t)
		w.WriteHeader(http.StatusNoContent)
	})
	controlServer := httptest.NewServer(control)
	t.Cleanup(controlServer.Close)

	env := map[string]string{
		"WZ_BASE_URL":           appServer.URL,
		"WZ_CONTROL_URL":        controlServer.URL,
		"WZ_MAIL_API_URL":       mail.apiBase,
		"WZ_ADMIN_USER":         e2eAdminUser,
		"WZ_ADMIN_INITIAL_PW":   e2eAdminInitialPW,
		"WZ_ADMIN_PW":           e2eAdminPW,
		"WZ_ADMIN_MAILBOX":      e2eAdminMailbox,
		"WZ_SENDER_MAILBOX":     e2eSenderMailbox,
		"WZ_LDAP_URL":           ldap.url,
		"WZ_LDAP_BIND_DN":       ldapSimBindDN,
		"WZ_LDAP_BIND_PW":       ldapSimBindPassword,
		"WZ_LDAP_BASE_DN":       ldapSimBaseDN,
		"WZ_LDAP_ALLOWED_GROUP": ldapSimAllowedGroup,
		"WZ_SMTP_HOST":          mail.host,
		"WZ_SMTP_PORT":          itoa(mail.smtpPort),
		"WZ_SMTP_USER":          mailSimUser,
		"WZ_SMTP_PW":            mailSimPassword,
		"WZ_SNMP_HOST":          sw.sim.host,
		"WZ_SNMP_PORT":          itoa(sw.sim.port),
		"WZ_SNMP_COMMUNITY":     e2eIOSXECommunity,
		"WZ_USER01":             "user01",
		"WZ_USER01_PW":          e2eUser01Password,
		"WZ_USER01_MAIL":        e2eUser01Mail,
		"WZ_USER02":             "user02",
		"WZ_USER02_PW":          e2eUser02Password,
		"WZ_USER02_MAIL":        e2eUser02Mail,
	}
	// 管理員改密碼後的新密碼由瀏覽器輸入，會經由 API 自動登錄；這裡先登錄，讓啟動階段的日誌也在掃描範圍
	useSecret(t, "管理員密碼", e2eAdminPW)

	if holdFile := os.Getenv(browserHoldEnv); holdFile != "" {
		holdBrowserEnv(t, holdFile, env)
		return
	}

	cmd := exec.Command("npx", "playwright", "test")
	cmd.Dir = filepath.Join(repoRoot(), "test", "browser")
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var out bytes.Buffer
	cmd.Stdout = io.MultiWriter(&out, os.Stdout)
	cmd.Stderr = io.MultiWriter(&out, os.Stderr)
	if err := cmd.Run(); err != nil {
		t.Fatalf("Playwright 流程失敗: %v", err)
	}
}

func holdBrowserEnv(t *testing.T, holdFile string, env map[string]string) {
	t.Helper()
	raw, _ := json.MarshalIndent(env, "", "  ")
	if err := os.WriteFile(holdFile, raw, 0o600); err != nil {
		t.Fatalf("寫入環境資訊失敗: %v", err)
	}
	t.Logf("環境已啟動，資訊寫在 %s；刪除該檔案或送出中斷訊號即結束", holdFile)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	deadline := time.After(60 * time.Minute)
	for {
		select {
		case <-stop:
			return
		case <-deadline:
			return
		case <-time.After(time.Second):
			if _, err := os.Stat(holdFile); os.IsNotExist(err) {
				return
			}
		}
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
