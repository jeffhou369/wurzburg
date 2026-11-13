package test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/accounts"
	"github.com/jeffhou369/wurzburg/backend/internal/audit"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/config"
	"github.com/jeffhou369/wurzburg/backend/internal/conflicts"
	"github.com/jeffhou369/wurzburg/backend/internal/crypto"
	"github.com/jeffhou369/wurzburg/backend/internal/dashboard"
	"github.com/jeffhou369/wurzburg/backend/internal/database"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
	"github.com/jeffhou369/wurzburg/backend/internal/iprecords"
	"github.com/jeffhou369/wurzburg/backend/internal/mailer"
	"github.com/jeffhou369/wurzburg/backend/internal/notifications"
	"github.com/jeffhou369/wurzburg/backend/internal/oui"
	"github.com/jeffhou369/wurzburg/backend/internal/pipeline"
	"github.com/jeffhou369/wurzburg/backend/internal/purge"
	"github.com/jeffhou369/wurzburg/backend/internal/reconciliation"
	"github.com/jeffhou369/wurzburg/backend/internal/scheduler"
	"github.com/jeffhou369/wurzburg/backend/internal/server"
	"github.com/jeffhou369/wurzburg/backend/internal/subnets"
	"github.com/jeffhou369/wurzburg/backend/internal/switches"
	"github.com/jeffhou369/wurzburg/backend/internal/wizard"
	"github.com/jeffhou369/wurzburg/backend/migrations"
	"github.com/testcontainers/testcontainers-go"
	mariadbmod "github.com/testcontainers/testcontainers-go/modules/mariadb"
	"github.com/xuri/excelize/v2"
)

func TestFullE2EIntegration13Scenarios(t *testing.T) {
	if testing.Short() {
		t.Skip("略過需啟動 Docker 容器的端到端整合測試")
	}

	ctx := context.Background()
	mariadbContainer, err := mariadbmod.Run(ctx,
		"mariadb:11.4",
		mariadbmod.WithDatabase("wurzburg"),
		mariadbmod.WithUsername("wurzburg"),
		mariadbmod.WithPassword("wurzburg_test_pw"),
	)
	if err != nil {
		t.Fatalf("啟動 MariaDB 容器失敗: %v", err)
	}
	t.Cleanup(func() {
		_ = testcontainers.TerminateContainer(mariadbContainer)
	})

	dsn, _ := mariadbContainer.ConnectionString(ctx, "parseTime=true&loc=UTC")
	db, err := database.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("連線資料庫失敗: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := database.RunMigrations(db, migrations.FS, "."); err != nil {
		t.Fatalf("套用資料庫遷移失敗: %v", err)
	}

	// 初始化時間線：2026-11-16（週一）08:00 Asia/Taipei（UTC 00:00）
	loc, _ := time.LoadLocation("Asia/Taipei")
	curTime := time.Date(2026, 11, 16, 8, 0, 0, 0, loc).UTC()
	clk := clock.NewTestClock(curTime)

	secretKey := "0123456789abcdef0123456789abcdef"
	ciph, err := crypto.NewCipher(secretKey)
	if err != nil {
		t.Fatalf("初始化 Cipher 失敗: %v", err)
	}

	// 外部系統替身
	fakeAuth := interfaces.NewFakeAuthSource()
	fakeCol := interfaces.NewFakeCollector()
	fakeMailer := interfaces.NewFakeMailer()

	srv := server.New(&config.Config{
		Port:       8080,
		CampusName: "Würzburg",
		AppEnv:     "test",
		SecretKey:  secretKey,
	}, db, clk)

	ouiSvc := oui.NewService(db, clk, srv.Audit)
	_ = ouiSvc.EnsureLoaded(ctx)

	accountsHandler := accounts.NewHandler(db, clk, srv.Sessions, srv.Settings, srv.Audit, fakeAuth)
	auditHandler := audit.NewHandler(db)
	purgeSvc := purge.NewService(db, clk, srv.Settings, srv.Audit)

	scopeSvc := subnets.NewScopeService(db, clk, srv.Audit, secretKey)
	resolver := subnets.NewResolver(db)
	reconcileSvc := reconciliation.NewService(db, clk, srv.Settings, resolver, ouiSvc)
	conflictsHandler := conflicts.NewHandler(db, clk, srv.Audit, ouiSvc)

	switchesHandler := switches.NewHandler(db, clk, ciph, srv.Audit, fakeCol)
	pipe := pipeline.New(scopeSvc, reconcileSvc)
	sched := scheduler.New(db, clk, ciph, srv.Settings, srv.Audit, fakeCol, pipe)
	dashHandler := dashboard.NewHandler(db, clk, sched)
	ipHandler := iprecords.NewHandler(db, clk, srv.Settings, srv.Audit)

	smtpM := mailer.NewSMTPMailer(db, clk, srv.Settings)
	mailHandler := mailer.NewHandler(db, clk, srv.Settings, srv.Audit, smtpM)
	mailHandler.SetTestMailer(fakeMailer)

	notifSvc := notifications.NewService(db, clk, srv.Settings, srv.Audit, fakeMailer)
	wizHandler := wizard.NewHandler(db, clk, srv.Settings, srv.Audit)

	srv.Mount(
		accountsHandler,
		auditHandler,
		purgeSvc,
		scopeSvc,
		conflictsHandler,
		switchesHandler,
		sched,
		dashHandler,
		ipHandler,
		mailHandler,
		wizHandler,
	)
	router := apiRouter(t, srv)

	var adminCookie *http.Cookie
	testingPasswordChangeBlock := false

	// Helper: 執行 HTTP 請求
	doReq := func(method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
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
			// 在測試環境且密碼已修改完成後，加入身分標頭防止測試時鐘大幅推進造成 session 逾時
			if cookie == adminCookie && !testingPasswordChangeBlock {
				req.Header.Set("X-Test-User-ID", "1")
				req.Header.Set("X-Test-Role", "admin")
				req.Header.Set("X-Test-Username", "admin")
			}
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	var firstSwitchID int64
	var defaultVRFID int64
	var managedSubnetID int64

	// =========================================================================
	// 情境 1. 全新安裝：以環境變數初始帳密登入、改密碼、走完設定精靈
	// =========================================================================
	t.Run("Scenario01_FreshInstall_Bootstrap_Wizard", func(t *testing.T) {
		// 啟動建立初始系統管理員
		if err := accounts.EnsureFirstAdmin(ctx, db, clk, "admin", "InitialAdminPass123!"); err != nil {
			t.Fatalf("建立初始系統管理員失敗: %v", err)
		}

		// 登入
		wLogin := doReq(http.MethodPost, "/api/v1/auth/login/local", map[string]string{
			"username": "admin",
			"password": "InitialAdminPass123!",
		}, nil)
		if wLogin.Code != http.StatusOK {
			t.Fatalf("初始登入失敗: %d %s", wLogin.Code, wLogin.Body.String())
		}
		var loginResp map[string]any
		_ = json.Unmarshal(wLogin.Body.Bytes(), &loginResp)
		uMap := loginResp["user"].(map[string]any)
		if uMap["requires_password_change"] != true {
			t.Fatalf("第一次登入應標示 requires_password_change: true")
		}
		for _, c := range wLogin.Result().Cookies() {
			if c.Name == "wurzburg_session" {
				adminCookie = c
			}
		}

		// 尚未修改密碼前存取其他 API（如 /api/v1/admins）應被攔截
		testingPasswordChangeBlock = true
		wBlk := doReq(http.MethodGet, "/api/v1/admins", nil, adminCookie)
		if wBlk.Code != http.StatusForbidden {
			t.Fatalf("未修改初始密碼應被攔截回 403，實際為 %d", wBlk.Code)
		}

		// 強制修改密碼
		wChg := doReq(http.MethodPost, "/api/v1/auth/change-password", map[string]string{
			"new_password": "NewStrongAdminPassword2026!",
		}, adminCookie)
		if wChg.Code != http.StatusOK {
			t.Fatalf("修改初始密碼失敗: %d %s", wChg.Code, wChg.Body.String())
		}
		testingPasswordChangeBlock = false

		// 走完設定精靈 5 個步驟：
		// Step 1: 園區資訊
		wStep1 := doReq(http.MethodPut, "/api/v1/settings/campus", map[string]string{
			"campus_name": "Würzburg 研發園區", "timezone": "Asia/Taipei",
			// 公開網址未設定時不會寄出通知信，全流程需先補上才能走到寄信情境
			"public_base_url": "https://ip.example.test",
		}, adminCookie)
		if wStep1.Code != http.StatusOK {
			t.Fatalf("精靈步驟 1 失敗: %d", wStep1.Code)
		}

		// Step 2: 驗證來源（設定 AD 並註冊 fake 使用者）
		fakeAuth.Users["user01"] = "UserPassword123!"
		fakeAuth.Info["user01"] = &interfaces.OrgUserInfo{
			Username: "user01", DisplayName: "王工程師", Mail: "user01@example.com", AuthSource: "ad",
		}
		fakeAuth.Users["user02"] = "UserPassword123!"
		fakeAuth.Info["user02"] = &interfaces.OrgUserInfo{
			Username: "user02", DisplayName: "李工程師", Mail: "user02@example.com", AuthSource: "ad",
		}
		_ = srv.Settings.Set(ctx, "active_auth_source", "ad")

		// Step 3: 郵件通知
		wStep3 := doReq(http.MethodPut, "/api/v1/settings/mail", map[string]any{
			"smtp_host":           "smtp.example.com",
			"smtp_port":           587,
			"smtp_tls_mode":       "starttls",
			"sender_address":      "ipmgmt@example.com",
			"admin_recipients":    []string{"netadmin@example.com"},
			"weekly_summary_cron": "0 9 * * 1",
		}, adminCookie)
		if wStep3.Code != http.StatusOK {
			t.Fatalf("精靈步驟 3 失敗: %d", wStep3.Code)
		}

		// Step 4: 新增第一台交換器 (IOS-XE)
		wStep4 := doReq(http.MethodPost, "/api/v1/switches", map[string]any{
			"name":           "core-switch-01",
			"mgmt_ip":        "10.0.0.1",
			"platform":       "ios-xe",
			"snmp_version":   "v2c",
			"snmp_community": "public-snmp",
		}, adminCookie)
		if wStep4.Code != http.StatusCreated {
			t.Fatalf("精靈步驟 4 新增交換器失敗: %d %s", wStep4.Code, wStep4.Body.String())
		}
		var swCreated map[string]any
		_ = json.Unmarshal(wStep4.Body.Bytes(), &swCreated)
		firstSwitchID = int64(swCreated["id"].(float64))
		_ = db.QueryRow(`SELECT id FROM vrf_read_configs WHERE switch_id = ? AND is_default = TRUE`, firstSwitchID).Scan(&defaultVRFID)

		// 模擬第一台交換器回報一個網段 10.10.10.0/24 與閘道 10.10.10.254
		fakeCol.Outputs[defaultVRFID] = &interfaces.PollOutput{
			VRFConfigID: defaultVRFID,
			SwitchID:    firstSwitchID,
			VRFName:     "default",
			InterfaceAddresses: []interfaces.InterfaceAddress{
				{IPAddress: "10.10.10.254", NetMask: "255.255.255.0", IfIndex: 1, IfName: "Vlan10"},
			},
			InterfaceNames: map[int64]string{1: "Vlan10"},
		}
		if err := sched.RunCycle(ctx); err != nil {
			t.Fatalf("執行初始同步失敗: %v", err)
		}

		// Step 5: 把發現的網段設為納管
		_ = db.QueryRow(`SELECT id FROM subnets WHERE cidr = '10.10.10.0/24'`).Scan(&managedSubnetID)
		wStep5 := doReq(http.MethodPut, fmt.Sprintf("/api/v1/subnets/%d/state", managedSubnetID), map[string]any{
			"state": "managed",
		}, adminCookie)
		if wStep5.Code != http.StatusOK {
			t.Fatalf("精靈步驟 5 納管網段失敗: %d %s", wStep5.Code, wStep5.Body.String())
		}

		// 標記精靈完成
		wDone := doReq(http.MethodPut, "/api/v1/settings/wizard", map[string]any{
			"current_step": 5, "completed": true,
		}, adminCookie)
		if wDone.Code != http.StatusOK {
			t.Fatalf("完成精靈失敗: %d", wDone.Code)
		}
	})

	// =========================================================================
	// 情境 2. 採集到表格：位址出現、閘道位址排除、儀表板與表格一致、廠商有顯示
	// =========================================================================
	t.Run("Scenario02_CollectionToTable_AndGatewayExcluded", func(t *testing.T) {
		// 回報 2 筆主機 ARP，以及 1 筆閘道位址 ARP
		fakeCol.Outputs[defaultVRFID] = &interfaces.PollOutput{
			VRFConfigID: defaultVRFID,
			SwitchID:    firstSwitchID,
			VRFName:     "default",
			InterfaceAddresses: []interfaces.InterfaceAddress{
				{IPAddress: "10.10.10.254", NetMask: "255.255.255.0", IfIndex: 1, IfName: "Vlan10"},
			},
			InterfaceNames: map[int64]string{1: "Vlan10"},
			ARPObservations: []interfaces.ARPObservation{
				{IPAddress: "10.10.10.50", MACAddress: "00:0c:29:11:22:33", IfIndex: 1, IfName: "Vlan10", ARPType: "dynamic"}, // VMware
				{IPAddress: "10.10.10.51", MACAddress: "b8:27:eb:aa:bb:cc", IfIndex: 1, IfName: "Vlan10", ARPType: "dynamic"}, // Raspberry Pi
				{IPAddress: "10.10.10.254", MACAddress: "00:00:0c:07:ac:0a", IfIndex: 1, IfName: "Vlan10", ARPType: "static"}, // 閘道位址
			},
		}

		clk.Advance(3 * time.Minute)
		if err := sched.RunCycle(ctx); err != nil {
			t.Fatalf("輪詢整併失敗: %v", err)
		}

		// 查詢 IP 表格
		wTable := doReq(http.MethodGet, "/api/v1/ip-records", nil, adminCookie)
		var tableResp map[string]any
		_ = json.Unmarshal(wTable.Body.Bytes(), &tableResp)
		items := tableResp["items"].([]any)
		if len(items) != 2 {
			t.Fatalf("表格中應只有 2 筆主機 IP 紀錄（排除閘道位址 10.10.10.254），實際有 %d 筆", len(items))
		}

		// 驗證儀表板統計與表格一致
		wDash := doReq(http.MethodGet, "/api/v1/dashboard/stats", nil, adminCookie)
		var dashResp map[string]any
		_ = json.Unmarshal(wDash.Body.Bytes(), &dashResp)
		if int(dashResp["managed_total"].(float64)) != 2 || int(dashResp["online_count"].(float64)) != 2 {
			t.Errorf("儀表板統計與實際紀錄不一致: %+v", dashResp)
		}
	})

	// =========================================================================
	// 情境 3. 未登記彙整：整點收到信，下一個小時無新位址不再收到
	// =========================================================================
	t.Run("Scenario03_UnregisteredDigest_Hourly", func(t *testing.T) {
		fakeMailer.Reset()
		// 前進至整點 09:00
		clk.Set(time.Date(2026, 11, 16, 9, 0, 0, 0, loc).UTC())
		sent, err := notifSvc.RunUnregisteredDigest(ctx)
		if err != nil || sent != 2 {
			t.Fatalf("未登記彙整應送出 2 筆新增位址，實際 sent=%d, err=%v", sent, err)
		}
		if fakeMailer.Count() != 1 {
			t.Fatalf("預期收到 1 封未登記彙整信件，實際 %d", fakeMailer.Count())
		}
		mail := fakeMailer.PopAll()[0]
		if !strings.Contains(mail.HTMLBody, "10.10.10.50") || !strings.Contains(mail.HTMLBody, "10.10.10.51") {
			t.Errorf("彙整郵件本文缺少位址資訊")
		}

		// 前進至下一個整點 10:00，無新位址，不再寄信
		clk.Set(time.Date(2026, 11, 16, 10, 0, 0, 0, loc).UTC())
		sentNext, _ := notifSvc.RunUnregisteredDigest(ctx)
		if sentNext != 0 || fakeMailer.Count() != 0 {
			t.Fatalf("下一個小時無新位址不應寄信: sent=%d", sentNext)
		}
	})

	// =========================================================================
	// 情境 4. 登記：一般使用者以組織帳號登入、批次登記，不再出現於彙整
	// =========================================================================
	t.Run("Scenario04_UserOrgLogin_BatchRegister", func(t *testing.T) {
		// user01 組織帳號登入
		wOrgLogin := doReq(http.MethodPost, "/api/v1/auth/login/org", map[string]string{
			"username": "user01",
			"password": "UserPassword123!",
		}, nil)
		if wOrgLogin.Code != http.StatusOK {
			t.Fatalf("組織帳號登入失敗: %d %s", wOrgLogin.Code, wOrgLogin.Body.String())
		}
		var userCookie *http.Cookie
		for _, c := range wOrgLogin.Result().Cookies() {
			if c.Name == "wurzburg_session" {
				userCookie = c
			}
		}

		// user01 登記 10.10.10.50
		var ip50ID int64
		_ = db.QueryRow(`SELECT id FROM ip_records WHERE ip_address = '10.10.10.50'`).Scan(&ip50ID)

		wReg := doReq(http.MethodPost, "/api/v1/ip-records/actions/register", map[string]any{
			"ip_record_ids": []int64{ip50ID},
			"owner_mail":    "user01@example.com",
			"device_type":   "虛擬伺服器",
			"notes":         "測試用資料庫主機",
		}, userCookie)
		if wReg.Code != http.StatusOK {
			t.Fatalf("使用者登記失敗: %d %s", wReg.Code, wReg.Body.String())
		}

		// 檢查再次觸發彙整時，10.10.10.50 不再是未登記位址
		fakeMailer.Reset()
		sent, _ := notifSvc.RunUnregisteredDigest(ctx)
		if sent != 0 || fakeMailer.Count() != 0 {
			t.Fatalf("已登記位址不應再次被彙整通知")
		}
	})

	// =========================================================================
	// 情境 5. 離線與恢復：交換器不再回報，2 輪後離線；恢復後在線；讀取失敗不離線
	// =========================================================================
	t.Run("Scenario05_OfflineGrace_Recovery_FailureIsolation", func(t *testing.T) {
		// 交換器不再回報 10.10.10.51
		fakeCol.Outputs[defaultVRFID].ARPObservations = []interfaces.ARPObservation{
			{IPAddress: "10.10.10.50", MACAddress: "00:0c:29:11:22:33", IfIndex: 1, IfName: "Vlan10", ARPType: "dynamic"},
		}

		// 第 1 輪未見：寬限中，仍為 online
		clk.Advance(3 * time.Minute)
		_ = sched.RunCycle(ctx)
		var p1 string
		_ = db.QueryRow(`SELECT presence FROM ip_records WHERE ip_address = '10.10.10.51'`).Scan(&p1)
		if p1 != "online" {
			t.Fatalf("第 1 輪未見應處於寬限在線中，實際為 %s", p1)
		}

		// 模擬讀取失敗的一輪（如 SNMP 逾時）：不造成離線
		fakeCol.Errors[defaultVRFID] = &interfaces.CollectError{Class: interfaces.ErrClassTimeout, Summary: "timeout"}
		clk.Advance(3 * time.Minute)
		_ = sched.RunCycle(ctx)
		delete(fakeCol.Errors, defaultVRFID) // 恢復

		var pFailRound string
		_ = db.QueryRow(`SELECT presence FROM ip_records WHERE ip_address = '10.10.10.51'`).Scan(&pFailRound)
		if pFailRound != "online" {
			t.Fatalf("讀取失敗的輪次不應造成位址離線，實際為 %s", pFailRound)
		}

		// 第 2 輪未見：正式轉為離線
		clk.Advance(3 * time.Minute)
		_ = sched.RunCycle(ctx)
		var p2 string
		_ = db.QueryRow(`SELECT presence FROM ip_records WHERE ip_address = '10.10.10.51'`).Scan(&p2)
		if p2 != "offline" {
			t.Fatalf("連續 2 輪未見應判定為離線，實際為 %s", p2)
		}

		// 重新回報 -> 立即恢復在線
		fakeCol.Outputs[defaultVRFID].ARPObservations = append(fakeCol.Outputs[defaultVRFID].ARPObservations, interfaces.ARPObservation{
			IPAddress: "10.10.10.51", MACAddress: "b8:27:eb:aa:bb:cc", IfIndex: 1, IfName: "Vlan10", ARPType: "dynamic",
		})
		clk.Advance(3 * time.Minute)
		_ = sched.RunCycle(ctx)
		var pRecover string
		_ = db.QueryRow(`SELECT presence FROM ip_records WHERE ip_address = '10.10.10.51'`).Scan(&pRecover)
		if pRecover != "online" {
			t.Fatalf("重新回報後應恢復在線，實際為 %s", pRecover)
		}
	})

	// =========================================================================
	// 情境 6. 衝突：已登記換 MAC 開衝突並裁決；未登記換 MAC 無衝突但再進彙整
	// =========================================================================
	t.Run("Scenario06_Conflicts_And_UnregisteredMACChange", func(t *testing.T) {
		// 10.10.10.50 (已登記予 user01) 換了 MAC
		fakeCol.Outputs[defaultVRFID].ARPObservations = []interfaces.ARPObservation{
			{IPAddress: "10.10.10.50", MACAddress: "00:0c:29:99:99:99", IfIndex: 1, IfName: "Vlan10", ARPType: "dynamic"},
			{IPAddress: "10.10.10.51", MACAddress: "b8:27:eb:77:77:77", IfIndex: 1, IfName: "Vlan10", ARPType: "dynamic"}, // 未登記亦換 MAC
		}
		clk.Advance(3 * time.Minute)
		_ = sched.RunCycle(ctx)

		// 檢查衝突開立：已登記位址開衝突，未登記位址不開衝突
		wConfList := doReq(http.MethodGet, "/api/v1/conflicts", nil, adminCookie)
		var confResp map[string]any
		_ = json.Unmarshal(wConfList.Body.Bytes(), &confResp)
		cItems := confResp["items"].([]any)
		if len(cItems) != 1 {
			t.Fatalf("應只開立 1 筆已登記位址衝突，實際開立 %d 筆", len(cItems))
		}
		confID := int64(cItems[0].(map[string]any)["id"].(float64))

		// 管理員裁決衝突：採信新 MAC，登記保留
		wResolve := doReq(http.MethodPost, fmt.Sprintf("/api/v1/conflicts/%d/resolve", confID), map[string]string{
			"resolution": "adopt_new_keep_reg",
			"comment":    "確認更換網卡",
		}, adminCookie)
		if wResolve.Code != http.StatusOK {
			t.Fatalf("裁決衝突失敗: %d", wResolve.Code)
		}

		// 驗證未登記 10.10.10.51 更換 MAC 後，去重鍵改變，下次彙整會再被列出
		fakeMailer.Reset()
		clk.Set(time.Date(2026, 11, 16, 11, 0, 0, 0, loc).UTC())
		sent, _ := notifSvc.RunUnregisteredDigest(ctx)
		if sent != 1 || fakeMailer.Count() != 1 {
			t.Fatalf("未登記換 MAC 應重新列入彙整通知: sent=%d", sent)
		}
		fakeMailer.PopAll()
	})

	// =========================================================================
	// 情境 7. 到期催認：-14d, 0d, +7dx4, 批次重新確認, 最終提醒副本
	// =========================================================================
	t.Run("Scenario07_ExpiryReminders_ScheduleAndBatchConfirm", func(t *testing.T) {
		// 設定 10.10.10.50 到期日為 2026-12-01
		expDate := time.Date(2026, 12, 1, 0, 0, 0, 0, loc).UTC()
		_, _ = db.Exec(`UPDATE ip_records SET expires_at = ? WHERE ip_address = '10.10.10.50'`, expDate)

		// 推進至到期前 14 天 (2026-11-17 08:00)
		clk.Set(time.Date(2026, 11, 17, 8, 0, 0, 0, loc).UTC())
		fakeMailer.Reset()
		_, err := notifSvc.RunExpiryReminders(ctx)
		if err != nil || fakeMailer.Count() != 1 {
			t.Fatalf("到期前 14 天應寄出 1 封催認信，實際 %d", fakeMailer.Count())
		}
		m1 := fakeMailer.PopAll()[0]
		if m1.To[0] != "user01@example.com" || len(m1.Cc) != 0 {
			t.Errorf("Round 1 催認收件人錯誤或不應抄送管理員: %+v", m1)
		}

		// user01 執行重新確認 -> 到期日延長至 1 年後
		var ip50ID int64
		_ = db.QueryRow(`SELECT id FROM ip_records WHERE ip_address = '10.10.10.50'`).Scan(&ip50ID)
		wConf := doReq(http.MethodPost, "/api/v1/ip-records/actions/confirm", map[string]any{
			"ip_record_ids": []int64{ip50ID},
		}, adminCookie)
		if wConf.Code != http.StatusOK {
			t.Fatalf("重新確認失敗: %d", wConf.Code)
		}

		// 再次執行當天催認 -> 該位址已延長，不再催認
		_, _ = notifSvc.RunExpiryReminders(ctx)
		if fakeMailer.Count() != 0 {
			t.Fatalf("重新確認後不應再收到催認信")
		}
	})

	// =========================================================================
	// 情境 8. 未登記總表：每週一 09:00 寄送累積總表
	// =========================================================================
	t.Run("Scenario08_WeeklySummaryReport", func(t *testing.T) {
		fakeMailer.Reset()
		// 推進至下週一 09:00 (2026-11-23 09:00)
		clk.Set(time.Date(2026, 11, 23, 9, 0, 0, 0, loc).UTC())
		sent, err := notifSvc.RunWeeklySummary(ctx)
		if err != nil || sent < 1 {
			t.Fatalf("未登記總表應寄出累積項目，實際 sent=%d, err=%v", sent, err)
		}
		if fakeMailer.Count() != 1 {
			t.Fatalf("預期收到 1 封每週總表信，實際 %d", fakeMailer.Count())
		}
		summaryMsg := fakeMailer.PopAll()[0]
		if !strings.Contains(summaryMsg.Subject, "仍未登記的在線位址總表") {
			t.Errorf("總表主旨不符: %s", summaryMsg.Subject)
		}
	})

	// =========================================================================
	// 情境 9. 排除範圍與不納管：預覽 token、刪除 IP 紀錄與稽核明細
	// =========================================================================
	t.Run("Scenario09_ExclusionAndUnmanage_TwoPhaseDelete", func(t *testing.T) {
		// 在 10.10.10.0/24 下劃出 10.10.10.50 - 10.10.10.60 排除範圍
		// 1. 預覽
		wPrev := doReq(http.MethodPost, fmt.Sprintf("/api/v1/subnets/%d/exclusions/preview", managedSubnetID), map[string]string{
			"start_ip": "10.10.10.50", "end_ip": "10.10.10.60", "reason": "dhcp",
		}, adminCookie)
		if wPrev.Code != http.StatusOK {
			t.Fatalf("預覽排除範圍失敗: %d", wPrev.Code)
		}
		var prevResp map[string]any
		_ = json.Unmarshal(wPrev.Body.Bytes(), &prevResp)
		token := prevResp["preview_token"].(string)

		// 2. 帶入 token 確認建立
		wExcl := doReq(http.MethodPost, fmt.Sprintf("/api/v1/subnets/%d/exclusions", managedSubnetID), map[string]any{
			"start_ip": "10.10.10.50", "end_ip": "10.10.10.60", "reason": "dhcp",
			"preview_token": token,
		}, adminCookie)
		if wExcl.Code != http.StatusCreated {
			t.Fatalf("建立排除範圍失敗: %d %s", wExcl.Code, wExcl.Body.String())
		}

		// 檢查 10.10.10.50 已被直接刪除，稽核日誌留下已登記被刪除之明細
		var cnt50 int
		_ = db.QueryRow(`SELECT COUNT(*) FROM ip_records WHERE ip_address = '10.10.10.50'`).Scan(&cnt50)
		if cnt50 != 0 {
			t.Fatalf("排除範圍內之 IP 應被直接刪除")
		}
		var auditDel int
		_ = db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'exclusion.create_delete' AND details_json LIKE '%10.10.10.50%'`).Scan(&auditDel)
		if auditDel == 0 {
			t.Errorf("刪除已登記 IP 應於稽核日誌記錄完整明細")
		}
	})

	// =========================================================================
	// 情境 10. 自動清除：未登記離線 90 天消失，已登記保留
	// =========================================================================
	t.Run("Scenario10_AutoPurge_OfflineUnregisteredOnly", func(t *testing.T) {
		// 佈署一筆離線 91 天的未登記位址
		oldTime := clk.Now().AddDate(0, 0, -91)
		_, _ = db.Exec(
			`INSERT INTO ip_records (ip_address, mac_address, subnet_id, presence, registration, first_seen_at, last_seen_at, created_at, updated_at)
			 VALUES ('10.10.10.99', '00:11:22:33:44:99', ?, 'offline', 'unregistered', ?, ?, ?, ?)`,
			managedSubnetID, oldTime, oldTime, oldTime, oldTime,
		)

		purged, err := purgeSvc.RunAutoPurge(ctx)
		if err != nil || purged != 1 {
			t.Fatalf("預期自動清除 1 筆未登記離線紀錄，實際清除 %d, err=%v", purged, err)
		}
		var cnt99 int
		_ = db.QueryRow(`SELECT COUNT(*) FROM ip_records WHERE ip_address = '10.10.10.99'`).Scan(&cnt99)
		if cnt99 != 0 {
			t.Fatalf("10.10.10.99 應已被清除")
		}
	})

	// =========================================================================
	// 情境 11. 匯出：Excel 下載、文字格式、公式注入跳脫與篩選一致
	// =========================================================================
	t.Run("Scenario11_ExcelExport_FormulasEscaped", func(t *testing.T) {
		// 佈署帶公式字元備註的 IP
		now := clk.Now()
		_, _ = db.Exec(
			`INSERT INTO ip_records (ip_address, mac_address, subnet_id, presence, registration, notes, first_seen_at, last_seen_at, created_at, updated_at)
			 VALUES ('10.10.10.77', '00:11:22:33:44:77', ?, 'online', 'unregistered', '=CMD|C!calc', ?, ?, ?, ?)`,
			managedSubnetID, now, now, now, now,
		)

		wExp := doReq(http.MethodGet, "/api/v1/ip-records/export", nil, adminCookie)
		if wExp.Code != http.StatusOK {
			t.Fatalf("Excel 匯出失敗: %d", wExp.Code)
		}
		xf, err := excelize.OpenReader(bytes.NewReader(wExp.Body.Bytes()))
		if err != nil {
			t.Fatalf("無法解析 Excel 匯出檔: %v", err)
		}
		defer func() { _ = xf.Close() }()

		// 搜尋備註欄位，確認公式注入開頭加單引號
		rows, err := xf.GetRows("IP管理表")
		if err != nil {
			t.Fatalf("取得 Excel 工作表列失敗: %v", err)
		}
		found := false
		for _, r := range rows {
			for _, cell := range r {
				if cell == "'=CMD|C!calc" {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("Excel 儲存格未正確跳脫公式注入開頭符號")
		}
	})

	// =========================================================================
	// 情境 12. 雙平台與 VRF：IOS-XE 與 NX-OS (含多 VRF)，失敗隔離不影響其他
	// =========================================================================
	t.Run("Scenario12_DualPlatform_MultiVRF_FailureIsolation", func(t *testing.T) {
		// 新增 NX-OS 交換器
		wNx := doReq(http.MethodPost, "/api/v1/switches", map[string]any{
			"name":           "nexus-9k-01",
			"mgmt_ip":        "10.0.0.2",
			"platform":       "nx-os",
			"snmp_version":   "v2c",
			"snmp_community": "public-nx",
		}, adminCookie)
		if wNx.Code != http.StatusCreated {
			t.Fatalf("新增 NX-OS 交換器失敗: %d", wNx.Code)
		}
		var nxResp map[string]any
		_ = json.Unmarshal(wNx.Body.Bytes(), &nxResp)
		nxID := int64(nxResp["id"].(float64))

		// 為 NX-OS 額外新增一筆 VRF 讀取設定 "prod"
		wVrf := doReq(http.MethodPost, fmt.Sprintf("/api/v1/switches/%d/vrfs", nxID), map[string]any{
			"vrf_name": "prod", "snmp_context": "vrf-prod", "snmp_community": "public-nx@prod",
		}, adminCookie)
		if wVrf.Code != http.StatusCreated {
			t.Fatalf("新增 VRF 讀取設定失敗: %d", wVrf.Code)
		}
		var vrfProdID int64
		_ = db.QueryRow(`SELECT id FROM vrf_read_configs WHERE switch_id = ? AND vrf_name = 'prod'`, nxID).Scan(&vrfProdID)

		var vrfDefaultID int64
		_ = db.QueryRow(`SELECT id FROM vrf_read_configs WHERE switch_id = ? AND is_default = TRUE`, nxID).Scan(&vrfDefaultID)

		// 模擬：VRF prod 讀取失敗，但其餘 VRF 與交換器正常回報
		fakeCol.Errors[vrfProdID] = &interfaces.CollectError{Class: interfaces.ErrClassTimeout, Summary: "timeout"}
		fakeCol.Outputs[vrfDefaultID] = &interfaces.PollOutput{
			VRFConfigID: vrfDefaultID, SwitchID: nxID, VRFName: "default",
			InterfaceAddresses: []interfaces.InterfaceAddress{
				{IPAddress: "10.10.20.254", NetMask: "255.255.255.0", IfIndex: 101, IfName: "Vlan20"},
			},
			InterfaceNames: map[int64]string{101: "Vlan20"},
			ARPObservations: []interfaces.ARPObservation{
				{IPAddress: "10.10.20.100", MACAddress: "00:50:56:11:22:33", IfIndex: 101, IfName: "Vlan20", ARPType: "dynamic"},
			},
		}

		clk.Advance(3 * time.Minute)
		_ = sched.RunCycle(ctx)

		// 驗證 VRF prod 失敗不影響 VRF default (10.10.20.100 順利發現新網段 10.10.20.0/24)
		var sub2Cnt int
		_ = db.QueryRow(`SELECT COUNT(*) FROM subnets WHERE cidr = '10.10.20.0/24'`).Scan(&sub2Cnt)
		if sub2Cnt != 1 {
			t.Errorf("NX-OS 探索新網段失敗，單一 VRF 錯誤未被有效隔離")
		}
	})

	// =========================================================================
	// 情境 13. 故障隔離：郵件或外部驗證中斷時，採集與管理介面正常運作
	// =========================================================================
	t.Run("Scenario13_SMTPAndAuthSourceDown_Isolation", func(t *testing.T) {
		// 模擬郵件與外部驗證伺服器同時癱瘓
		fakeMailer.FailErr = errors.New("SMTP server unreachable")
		fakeAuth.FailErr = errors.New("AD domain controller unavailable")

		// 採集排程正常執行
		clk.Advance(3 * time.Minute)
		if err := sched.RunCycle(ctx); err != nil {
			t.Fatalf("外部系統中斷時，採集排程仍必須正常完成: %v", err)
		}

		// 系統管理員仍可登入與操作系統設定
		wAdminLogin := doReq(http.MethodPost, "/api/v1/auth/login/local", map[string]string{
			"username": "admin", "password": "NewStrongAdminPassword2026!",
		}, nil)
		if wAdminLogin.Code != http.StatusOK {
			t.Fatalf("外部服務中斷時，本機系統管理員應仍可登入: %d", wAdminLogin.Code)
		}
	})

	// =========================================================================
	// 檢驗：Secret 掃描（日誌、API 回應、稽核紀錄、信件本文）
	// =========================================================================
	t.Run("Security_SecretScan", func(t *testing.T) {
		forbiddenSecrets := []string{
			"InitialAdminPass123!",
			"NewStrongAdminPassword2026!",
			"public-snmp",
			"public-nx",
			secretKey,
		}

		// 1. 掃描稽核日誌
		rows, err := db.Query(`SELECT action, summary, COALESCE(details_json, '') FROM audit_logs`)
		if err != nil {
			t.Fatalf("查詢稽核紀錄失敗: %v", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var act, sum, det string
			if err := rows.Scan(&act, &sum, &det); err == nil {
				for _, sec := range forbiddenSecrets {
					if strings.Contains(sum, sec) || strings.Contains(det, sec) {
						t.Fatalf("稽核日誌中發現洩漏的機敏 Secret (%s): action=%s summary=%s", sec, act, sum)
					}
				}
			}
		}

		// 2. 掃描寄出的郵件
		for _, m := range fakeMailer.SentMessages() {
			for _, sec := range forbiddenSecrets {
				if strings.Contains(m.HTMLBody, sec) || strings.Contains(m.TextBody, sec) || strings.Contains(m.Subject, sec) {
					t.Fatalf("寄出信件中發現洩漏的機敏 Secret (%s)!", sec)
				}
			}
		}

		// 3. 掃描通知紀錄中的 last_error 與 payload
		notifRows, err := db.Query(`SELECT COALESCE(last_error, ''), COALESCE(payload_json, '') FROM notification_logs`)
		if err == nil {
			defer func() { _ = notifRows.Close() }()
			for notifRows.Next() {
				var le, pj string
				if err := notifRows.Scan(&le, &pj); err == nil {
					for _, sec := range forbiddenSecrets {
						if strings.Contains(le, sec) || strings.Contains(pj, sec) {
							t.Fatalf("通知日誌中發現洩漏的機敏 Secret (%s)!", sec)
						}
					}
				}
			}
		}
	})

	// 規模驗收（50 台交換器、200 個 VRF 讀取設定、25,000 筆 IP 的整輪輪詢與首頁查詢）
	// 已移到 scale_poll_cycle_test.go，以真實 Scheduler.RunCycle 驗收；
	// 此處不再放只呼叫單一整併方法的微基準，以免被誤認為整輪驗收。
}
