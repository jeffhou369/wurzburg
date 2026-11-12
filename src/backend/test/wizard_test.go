package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/config"
	"github.com/jeffhou369/wurzburg/backend/internal/database"
	"github.com/jeffhou369/wurzburg/backend/internal/server"
	"github.com/jeffhou369/wurzburg/backend/internal/testutil"
	"github.com/jeffhou369/wurzburg/backend/internal/wizard"
	"github.com/jeffhou369/wurzburg/backend/migrations"
	"github.com/testcontainers/testcontainers-go"
	mariadbmod "github.com/testcontainers/testcontainers-go/modules/mariadb"
)

func TestWizardAndCampusSettings(t *testing.T) {
	if testing.Short() {
		t.Skip("略過需啟動 Docker 容器的整合測試")
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
		t.Fatalf("遷移失敗: %v", err)
	}

	clk := clock.NewTestClock(time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC))
	srv := server.New(&config.Config{Port: 8080, CampusName: "Würzburg", AppEnv: "test"}, db, clk)
	wizHandler := wizard.NewHandler(db, clk, srv.Settings, srv.Audit)
	srv.Mount(wizHandler)
	router := srv.Router()

	seeder := testutil.NewSeeder(t, db, clk)
	adminID := seeder.SeedAdmin("admin")
	userID := seeder.SeedUser("chen", "user", "local", "chen@example.com", "陳工程師")

	// 1. 一般使用者讀取與修改權限驗證
	t.Run("Permissions_RegularUserForbidden", func(t *testing.T) {
		reqPutCampus := testutil.NewAuthRequest(http.MethodPut, "/api/v1/settings/campus", map[string]string{
			"campus_name": "新竹園區", "timezone": "Asia/Taipei",
		}, userID, "user", "chen")
		wPutCampus := httptest.NewRecorder()
		router.ServeHTTP(wPutCampus, reqPutCampus)
		if wPutCampus.Code != http.StatusForbidden {
			t.Errorf("一般使用者更新園區設定應回 403，實際為 %d", wPutCampus.Code)
		}

		reqWiz := testutil.NewAuthRequest(http.MethodGet, "/api/v1/settings/wizard", nil, userID, "user", "chen")
		wWiz := httptest.NewRecorder()
		router.ServeHTTP(wWiz, reqWiz)
		if wWiz.Code != http.StatusForbidden {
			t.Errorf("一般使用者讀取精靈狀態應回 403，實際為 %d", wWiz.Code)
		}
	})

	// 2. 園區資訊查詢與修改（驗證時區合法性與稽核日誌）
	t.Run("CampusSettings_UpdateAndAudit", func(t *testing.T) {
		// 預設值檢查
		reqGet := testutil.NewAuthRequest(http.MethodGet, "/api/v1/settings/campus", nil, adminID, "admin", "admin")
		wGet := httptest.NewRecorder()
		router.ServeHTTP(wGet, reqGet)
		if wGet.Code != http.StatusOK {
			t.Fatalf("取得園區設定失敗: %d", wGet.Code)
		}
		var getResp map[string]string
		_ = json.Unmarshal(wGet.Body.Bytes(), &getResp)
		if getResp["campus_name"] != "Würzburg" || getResp["timezone"] != "Asia/Taipei" {
			t.Errorf("預設園區設定不符: %+v", getResp)
		}

		// 嘗試設定非法時區 -> 422
		reqBadTz := testutil.NewAuthRequest(http.MethodPut, "/api/v1/settings/campus", map[string]string{
			"campus_name": "台北總部", "timezone": "Invalid/Unknown_Zone",
		}, adminID, "admin", "admin")
		wBadTz := httptest.NewRecorder()
		router.ServeHTTP(wBadTz, reqBadTz)
		if wBadTz.Code != http.StatusUnprocessableEntity {
			t.Errorf("非法時區應回 422，實際為 %d", wBadTz.Code)
		}

		// 正確修改
		reqPut := testutil.NewAuthRequest(http.MethodPut, "/api/v1/settings/campus", map[string]string{
			"campus_name": "新竹二廠", "timezone": "Asia/Taipei",
		}, adminID, "admin", "admin")
		wPut := httptest.NewRecorder()
		router.ServeHTTP(wPut, reqPut)
		if wPut.Code != http.StatusOK {
			t.Fatalf("更新園區設定失敗: %d %s", wPut.Code, wPut.Body.String())
		}

		// 檢查稽核紀錄
		var auditCount int
		_ = db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'settings.campus_update' AND target_id = 'campus'`).Scan(&auditCount)
		if auditCount != 1 {
			t.Errorf("更新園區設定未正確記錄稽核日誌")
		}
	})

	// 3. 設定精靈狀態與未完成清單動態計算
	t.Run("Wizard_StatusAndUnfinishedChecklist", func(t *testing.T) {
		// 初始狀態下：
		// - active_auth_source: "none" -> unfinished (auth_source)
		// - smtp_host: "" -> unfinished (mail)
		// - switches 數量為 0 -> unfinished (switches)
		// - subnets 納管數量為 0 -> unfinished (subnets)
		// - 非 HTTPS 請求 (無 TLS 無 X-Forwarded-Proto) -> unfinished (https)
		reqWiz := testutil.NewAuthRequest(http.MethodGet, "/api/v1/settings/wizard", nil, adminID, "admin", "admin")
		wWiz := httptest.NewRecorder()
		router.ServeHTTP(wWiz, reqWiz)
		if wWiz.Code != http.StatusOK {
			t.Fatalf("讀取精靈狀態失敗: %d", wWiz.Code)
		}

		var status wizard.WizardStatus
		if err := json.Unmarshal(wWiz.Body.Bytes(), &status); err != nil {
			t.Fatalf("解析精靈狀態失敗: %v", err)
		}

		if status.Completed {
			t.Errorf("初始精靈狀態不應為已完成")
		}
		if status.CurrentStep != 1 {
			t.Errorf("初始步驟應為 1，實際為 %d", status.CurrentStep)
		}
		if len(status.UnfinishedItems) != 5 {
			t.Fatalf("初始未完成項目應有 5 項，實際有 %d 項", len(status.UnfinishedItems))
		}

		// 驗證 HTTPS 標頭代入後，https 未完成項目解除
		reqHTTPS := testutil.NewAuthRequest(http.MethodGet, "/api/v1/settings/wizard", nil, adminID, "admin", "admin")
		reqHTTPS.Header.Set("X-Forwarded-Proto", "https")
		wHTTPS := httptest.NewRecorder()
		router.ServeHTTP(wHTTPS, reqHTTPS)
		var statusHTTPS wizard.WizardStatus
		_ = json.Unmarshal(wHTTPS.Body.Bytes(), &statusHTTPS)
		for _, it := range statusHTTPS.UnfinishedItems {
			if it.Key == "https" {
				t.Errorf("在 HTTPS 請求下不應出現 https 未完成項目")
			}
		}

		// 更新步驟進度為 3
		reqStep := testutil.NewAuthRequest(http.MethodPut, "/api/v1/settings/wizard", map[string]any{
			"current_step": 3,
			"completed":    false,
		}, adminID, "admin", "admin")
		wStep := httptest.NewRecorder()
		router.ServeHTTP(wStep, reqStep)
		if wStep.Code != http.StatusOK {
			t.Fatalf("更新步驟進度失敗: %d", wStep.Code)
		}

		// 模擬完成所有設定：
		_ = srv.Settings.Set(ctx, "active_auth_source", "ad")
		_ = srv.Settings.Set(ctx, "smtp_host", "smtp.campus.local")
		if err := srv.Settings.Set(ctx, "public_base_url", "https://ip.example.test"); err != nil {
			t.Fatal(err)
		}
		_ = srv.Settings.Set(ctx, "smtp_sender", "ip@campus.local")
		seeder.SeedSwitch("core-gw", "10.0.0.1", "ios-xe", "v3")
		seeder.SeedSubnet("10.0.0.0/8", "managed", nil)

		// 標記精靈完成
		reqFinish := testutil.NewAuthRequest(http.MethodPut, "/api/v1/settings/wizard", map[string]any{
			"current_step": 5,
			"completed":    true,
		}, adminID, "admin", "admin")
		wFinish := httptest.NewRecorder()
		router.ServeHTTP(wFinish, reqFinish)
		if wFinish.Code != http.StatusOK {
			t.Fatalf("完成精靈失敗: %d", wFinish.Code)
		}

		// 再次查詢（帶 HTTPS 標頭），所有未完成清單項目解除，且 completed == true
		reqDone := testutil.NewAuthRequest(http.MethodGet, "/api/v1/settings/wizard", nil, adminID, "admin", "admin")
		reqDone.Header.Set("X-Forwarded-Proto", "https")
		wDone := httptest.NewRecorder()
		router.ServeHTTP(wDone, reqDone)
		var statusDone wizard.WizardStatus
		_ = json.Unmarshal(wDone.Body.Bytes(), &statusDone)
		if !statusDone.Completed {
			t.Errorf("精靈應標記為已完成")
		}
		if len(statusDone.UnfinishedItems) != 0 {
			t.Errorf("所有項目已完成時未完成清單應為空，實際有 %d 項: %+v", len(statusDone.UnfinishedItems), statusDone.UnfinishedItems)
		}
	})
}
