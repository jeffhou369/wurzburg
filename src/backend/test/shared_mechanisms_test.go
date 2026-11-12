package test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/config"
	"github.com/jeffhou369/wurzburg/backend/internal/crypto"
	"github.com/jeffhou369/wurzburg/backend/internal/database"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
	"github.com/jeffhou369/wurzburg/backend/internal/pipeline"
	"github.com/jeffhou369/wurzburg/backend/internal/server"
	"github.com/jeffhou369/wurzburg/backend/internal/settings"
	"github.com/jeffhou369/wurzburg/backend/internal/subnets"
	"github.com/jeffhou369/wurzburg/backend/internal/testutil"
	"github.com/jeffhou369/wurzburg/backend/migrations"
	"github.com/testcontainers/testcontainers-go"
	mariadbmod "github.com/testcontainers/testcontainers-go/modules/mariadb"
)

type spyScopeProcessor struct {
	called bool
	count  int
}

func (s *spyScopeProcessor) ApplyInterfaceAddresses(_ context.Context, outputs []*interfaces.PollOutput) error {
	s.called = true
	s.count = len(outputs)
	return nil
}

type spyReconcilerProcessor struct {
	called bool
	failed int
}

func (r *spyReconcilerProcessor) ReconcileObservations(_ context.Context, _ []*interfaces.PollOutput, failedIDs []int64) error {
	r.called = true
	r.failed = len(failedIDs)
	return nil
}

type customRegistrar struct{}

func (c *customRegistrar) RegisterRoutes(api chi.Router) {
	api.Get("/custom-ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pong"))
	})
}

func TestSharedMechanisms_UnitFakesAndCrypto(t *testing.T) {
	// 1. 金鑰不存在或長度錯誤應回傳明確錯誤，不默默採用空值
	if _, err := crypto.NewCipher(""); err == nil {
		t.Fatal("預期空金鑰應回報錯誤")
	}
	if _, err := crypto.NewCipher("short-key"); err == nil {
		t.Fatal("預期過短金鑰應回報錯誤")
	}

	ciph, err := crypto.NewCipher("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("建立加密處理器失敗: %v", err)
	}
	encrypted, err := ciph.Encrypt("my_snmp_secret")
	if err != nil || encrypted == "my_snmp_secret" {
		t.Fatalf("加密失敗或仍為明文: %v", err)
	}
	decrypted, err := ciph.Decrypt(encrypted)
	if err != nil || decrypted != "my_snmp_secret" {
		t.Fatalf("解密失敗: %v", err)
	}

	// 錯誤金鑰解密必須失敗
	wrongCiph, _ := crypto.NewCipher("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if _, err := wrongCiph.Decrypt(encrypted); err == nil {
		t.Fatal("使用錯誤金鑰解密應失敗")
	}

	// 2. 四個模組測試替身驗證
	ctx := context.Background()
	fakeCol := interfaces.NewFakeCollector()
	fakeCol.Outputs[1] = &interfaces.PollOutput{VRFConfigID: 1, VRFName: "CORP"}
	out, err := fakeCol.Collect(ctx, interfaces.VRFReadInput{VRFConfigID: 1})
	if err != nil || out.VRFName != "CORP" {
		t.Fatalf("FakeCollector 回應不符: %+v, err=%v", out, err)
	}

	fakeAuth := interfaces.NewFakeAuthSource()
	fakeAuth.Users["chen"] = "pass123"
	uInfo, err := fakeAuth.Authenticate(ctx, "chen", "pass123")
	if err != nil || uInfo.Username != "chen" {
		t.Fatalf("FakeAuthSource 驗證失敗: %v", err)
	}

	fakeOUI := interfaces.NewFakeOUILookup()
	if v := fakeOUI.Lookup("aa:bb:cc:11:22:33"); v != "未知" {
		t.Fatalf("預期未知 OUI 應回傳「未知」，實際取得: %s", v)
	}
	fakeOUI.Vendors["aa:bb:cc"] = "Cisco"
	if v := fakeOUI.Lookup("aa:bb:cc:11:22:33"); v != "Cisco" {
		t.Fatalf("預期查詢為 Cisco，實際取得: %s", v)
	}

	fakeMailer := interfaces.NewFakeMailer()
	_ = fakeMailer.Send(ctx, interfaces.MailMessage{To: []string{"a@b.com"}, Subject: "test"})
	if len(fakeMailer.SentMessages()) != 1 {
		t.Fatalf("FakeMailer 應記錄 1 封信")
	}

	// 3. 處理管線呼叫驗證
	spyScope := &spyScopeProcessor{}
	spyRecon := &spyReconcilerProcessor{}
	pipe := pipeline.New(spyScope, spyRecon)
	if err := pipe.Process(ctx, []*interfaces.PollOutput{out}, []int64{99}); err != nil {
		t.Fatalf("處理管線失敗: %v", err)
	}
	if !spyScope.called || spyScope.count != 1 || !spyRecon.called || spyRecon.failed != 1 {
		t.Fatalf("管線步驟未正確觸發: scope=%+v, recon=%+v", spyScope, spyRecon)
	}
}

func TestSharedMechanisms_IntegrationMariaDB(t *testing.T) {
	if testing.Short() {
		t.Skip("略過需啟動 Docker 容器的共用機制整合測試")
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
	defer db.Close()

	if err := database.RunMigrations(db, migrations.FS, "."); err != nil {
		t.Fatalf("遷移失敗: %v", err)
	}

	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	clk := clock.NewTestClock(now)
	cfg := &config.Config{
		Port:       8080,
		CampusName: "Würzburg",
		SecretKey:  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		AppEnv:     "test",
	}

	srv := server.New(cfg, db, clk, &customRegistrar{})
	seeder := testutil.NewSeeder(t, db, clk)
	adminID := seeder.SeedAdmin("admin")
	userID := seeder.SeedUser("chen", "user", "ad", "chen@example.com", "陳工程師")

	// 1. 驗證模組自行註冊路由運作正常
	reqCustom := httptest.NewRequest(http.MethodGet, "/api/v1/custom-ping", nil)
	recCustom := httptest.NewRecorder()
	srv.Router().ServeHTTP(recCustom, reqCustom)
	if recCustom.Code != http.StatusOK || recCustom.Body.String() != "pong" {
		t.Fatalf("自訂路由未生效: code=%d body=%s", recCustom.Code, recCustom.Body.String())
	}

	// 2. 驗證未登入回 401、角色不符回 403、缺少 CSRF 標頭遭拒、一行輔助標頭通過
	t.Run("Authorization and CSRF", func(t *testing.T) {
		// 未登入 -> 401
		reqUnauth := httptest.NewRequest(http.MethodGet, "/api/v1/auth/check", nil)
		recUnauth := httptest.NewRecorder()
		srv.Router().ServeHTTP(recUnauth, reqUnauth)
		if recUnauth.Code != http.StatusUnauthorized {
			t.Errorf("未登入預期 401，實際 %d", recUnauth.Code)
		}

		// 缺少 CSRF 標頭之變更請求 -> 403
		reqNoCSRF := httptest.NewRequest(http.MethodPost, "/api/v1/auth/admin-check", nil)
		recNoCSRF := httptest.NewRecorder()
		srv.Router().ServeHTTP(recNoCSRF, reqNoCSRF)
		if recNoCSRF.Code != http.StatusForbidden {
			t.Errorf("缺 CSRF 標頭預期 403，實際 %d", recNoCSRF.Code)
		}

		// 一般使用者呼叫需管理員權限端點 -> 403
		reqUser := testutil.NewAuthRequest(http.MethodPost, "/api/v1/auth/admin-check", nil, userID, "user", "chen")
		recUser := httptest.NewRecorder()
		srv.Router().ServeHTTP(recUser, reqUser)
		if recUser.Code != http.StatusForbidden {
			t.Errorf("一般使用者呼叫管理員端點預期 403，實際 %d", recUser.Code)
		}

		// 系統管理員一行輔助請求 -> 200
		reqAdmin := testutil.NewAuthRequest(http.MethodPost, "/api/v1/auth/admin-check", nil, adminID, "admin", "admin")
		recAdmin := httptest.NewRecorder()
		srv.Router().ServeHTTP(recAdmin, reqAdmin)
		if recAdmin.Code != http.StatusOK {
			t.Errorf("管理員呼叫預期 200，實際 %d", recAdmin.Code)
		}
	})

	// 3. 驗證工作階段 Cookie 與閒置逾時（由 TestClock 控制）
	t.Run("Session Idle Timeout", func(t *testing.T) {
		token, err := srv.Sessions.CreateSession(ctx, userID, 8, "127.0.0.1", "GoTest")
		if err != nil {
			t.Fatalf("建立工作階段失敗: %v", err)
		}

		reqWithCookie := httptest.NewRequest(http.MethodGet, "/api/v1/auth/check", nil)
		reqWithCookie.AddCookie(&http.Cookie{Name: "wurzburg_session", Value: token})

		// 剛建立應有效
		rec := httptest.NewRecorder()
		srv.Router().ServeHTTP(rec, reqWithCookie)
		if rec.Code != http.StatusOK {
			t.Fatalf("有效工作階段應回 200，實際 %d", rec.Code)
		}

		// 推進 9 小時（超過 8 小時閒置）應失效回 401
		clk.Advance(9 * time.Hour)
		recExpired := httptest.NewRecorder()
		srv.Router().ServeHTTP(recExpired, reqWithCookie)
		if recExpired.Code != http.StatusUnauthorized {
			t.Fatalf("閒置逾時後應回 401，實際 %d", recExpired.Code)
		}
	})

	// 4. 驗證設定與 Secret 加密儲存（資料庫不得為明文）
	t.Run("Settings and Secrets", func(t *testing.T) {
		defCampus, err := srv.Settings.Get(ctx, "campus_name")
		if err != nil || defCampus != "Würzburg" {
			t.Fatalf("預設園區名稱預期 Würzburg，實際 %q, err=%v", defCampus, err)
		}
		if err := srv.Settings.Set(ctx, "campus_name", "新竹園區"); err != nil {
			t.Fatalf("寫入設定失敗: %v", err)
		}
		gotCampus, _ := srv.Settings.Get(ctx, "campus_name")
		if gotCampus != "新竹園區" {
			t.Fatalf("讀回設定預期新竹園區，實際 %q", gotCampus)
		}

		if err := srv.Settings.SetSecret(ctx, "smtp_password", "TopSecretSMTP#99"); err != nil {
			t.Fatalf("寫入 secret 失敗: %v", err)
		}

		// 檢查資料庫內存的是密文而非明文
		var rawDBVal string
		_ = db.QueryRow(`SELECT setting_value FROM settings WHERE setting_key = 'smtp_password'`).Scan(&rawDBVal)
		if strings.Contains(rawDBVal, "TopSecretSMTP#99") || rawDBVal == "" {
			t.Fatalf("資料庫儲存內容不應包含明文: %s", rawDBVal)
		}

		// 禁止透過一般 Get 讀出 secret
		if _, err := srv.Settings.Get(ctx, "smtp_password"); err == nil {
			t.Fatal("預期以一般 Get 讀取 secret 應被拒絕")
		}

		// HasSecret 應為 true 且 GetSecret 還原正確
		if has, _ := srv.Settings.HasSecret(ctx, "smtp_password"); !has {
			t.Fatal("HasSecret 應為 true")
		}
		dec, err := srv.Settings.GetSecret(ctx, "smtp_password")
		if err != nil || dec != "TopSecretSMTP#99" {
			t.Fatalf("解密內容不符: %q, err=%v", dec, err)
		}

		// 無金鑰設定庫寫入 secret 必須給出明確錯誤
		noKeyStore := settings.NewStore(db, nil, clk)
		if err := noKeyStore.SetSecret(ctx, "k", "v"); err == nil {
			t.Fatal("無金鑰時 SetSecret 應回報錯誤")
		}
	})

	// 5. 驗證稽核遮蔽機制
	t.Run("Audit Log Secret Sanitization", func(t *testing.T) {
		srv.Audit.Log(ctx, "admin", "admin", "127.0.0.1", "settings.update", "mail", "smtp", "更新郵件設定", map[string]any{
			"host":          "smtp.example.com",
			"smtp_password": "DoNotLeakThisPassword",
			"nested": map[string]any{
				"snmp_community": "SecretComm",
			},
		})

		var details string
		err := db.QueryRow(`SELECT details_json FROM audit_logs ORDER BY id DESC LIMIT 1`).Scan(&details)
		if err != nil {
			t.Fatalf("查詢稽核日誌失敗: %v", err)
		}
		if strings.Contains(details, "DoNotLeakThisPassword") || strings.Contains(details, "SecretComm") {
			t.Fatalf("稽核日誌外洩機敏資訊: %s", details)
		}
		if !strings.Contains(details, "[REDACTED]") {
			t.Fatalf("稽核日誌應包含 [REDACTED] 遮蔽標記: %s", details)
		}
	})

	// 6. 驗證位址歸屬查詢（Longest Prefix Match、排除邊界、閘道、未定前綴）
	t.Run("Subnet IP Resolution", func(t *testing.T) {
		swID, _ := seeder.SeedSwitch("sw-res", "10.0.0.10", "ios-xe", "v3")
		sub16 := seeder.SeedSubnet("10.20.0.0/16", "managed", nil)
		sub24 := seeder.SeedSubnet("10.20.30.0/24", "managed", nil)
		seeder.SeedExclusionRange(sub24, "10.20.30.100", "10.20.30.150", "dhcp")
		seeder.SeedGateway(sub24, "10.20.30.1", "Vlan30", swID)

		resolver := subnets.NewResolver(db)

		// 較長前綴優先 (/24 勝過 /16)
		r1, err := resolver.Resolve(ctx, "10.20.30.50")
		if err != nil || r1.SubnetID != sub24 || !r1.ShouldHaveRecord {
			t.Errorf("10.20.30.50 應歸屬 /24 網段並可建紀錄，實際 %+v, err=%v", r1, err)
		}

		// /16 其餘位址歸屬 /16
		r2, err := resolver.Resolve(ctx, "10.20.1.50")
		if err != nil || r2.SubnetID != sub16 || !r2.ShouldHaveRecord {
			t.Errorf("10.20.1.50 應歸屬 /16 網段並可建紀錄，實際 %+v, err=%v", r2, err)
		}

		// 排除範圍邊界 (100 與 150 皆被排除，99 與 151 不排除)
		rExStart, _ := resolver.Resolve(ctx, "10.20.30.100")
		if !rExStart.IsExcluded || rExStart.ShouldHaveRecord {
			t.Errorf("排除起點 10.20.30.100 應排除，實際 %+v", rExStart)
		}
		rExEnd, _ := resolver.Resolve(ctx, "10.20.30.150")
		if !rExEnd.IsExcluded || rExEnd.ShouldHaveRecord {
			t.Errorf("排除終點 10.20.30.150 應排除，實際 %+v", rExEnd)
		}
		rJustBefore, _ := resolver.Resolve(ctx, "10.20.30.99")
		if rJustBefore.IsExcluded || !rJustBefore.ShouldHaveRecord {
			t.Errorf("10.20.30.99 不應排除，實際 %+v", rJustBefore)
		}

		// 閘道位址
		rGw, _ := resolver.Resolve(ctx, "10.20.30.1")
		if !rGw.IsGateway || rGw.ShouldHaveRecord {
			t.Errorf("閘道位址 10.20.30.1 應標記為閘道且不建紀錄，實際 %+v", rGw)
		}

		// 不屬於任何已知網段
		rUnknown, _ := resolver.Resolve(ctx, "192.168.99.5")
		if rUnknown.SubnetID != 0 || rUnknown.ShouldHaveRecord {
			t.Errorf("未知網段 192.168.99.5 應不屬於任何網段，實際 %+v", rUnknown)
		}
	})
}
