package test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/audit"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/config"
	"github.com/jeffhou369/wurzburg/backend/internal/database"
	"github.com/jeffhou369/wurzburg/backend/internal/oui"
	"github.com/jeffhou369/wurzburg/backend/internal/purge"
	"github.com/jeffhou369/wurzburg/backend/internal/server"
	"github.com/jeffhou369/wurzburg/backend/internal/testutil"
	"github.com/jeffhou369/wurzburg/backend/migrations"
	"github.com/testcontainers/testcontainers-go"
	mariadbmod "github.com/testcontainers/testcontainers-go/modules/mariadb"
)

func TestClusterA_AuditOUIAndAutoPurge(t *testing.T) {
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
	db, err := connectTestDB(t, ctx, dsn)
	if err != nil {
		t.Fatalf("連線資料庫失敗: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := database.RunMigrations(db, migrations.FS, "."); err != nil {
		t.Fatalf("遷移失敗: %v", err)
	}

	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	clk := clock.NewTestClock(now)
	cfg := &config.Config{Port: 8080, CampusName: "Würzburg", AppEnv: "test"}

	srv := server.New(cfg, db, clk)
	ouiSvc := oui.NewService(db, clk, srv.Audit)
	if err := ouiSvc.EnsureLoaded(ctx); err != nil {
		t.Fatalf("載入內建 OUI 快照失敗: %v", err)
	}
	auditHandler := audit.NewHandler(db)
	purgeSvc := purge.NewService(db, clk, srv.Settings, srv.Audit)
	srv.Mount(auditHandler, ouiSvc, purgeSvc)
	router := apiRouter(t, srv)

	seeder := testutil.NewSeeder(t, db, clk)
	adminID := seeder.SeedAdmin("admin")
	userID := seeder.SeedUser("chen", "user", "ad", "chen@example.com", "陳工程師")
	subnetID := seeder.SeedSubnet("10.10.10.0/24", "managed", nil)

	// ========================================================
	// 1. 驗證 Ticket 17：OUI 離線查詢與上傳更新
	// ========================================================
	t.Run("Local OUI lookup and upload", func(t *testing.T) {
		// 24 位前綴命中
		if got := ouiSvc.Lookup("00:50:56:11:22:33"); got != "VMware, Inc." {
			t.Errorf("預期 VMware, Inc.，實際 %q", got)
		}
		// 內建快照只收錄真實前綴；虛構的長前綴範例只存在於測試資料，不得命中
		if got := ouiSvc.Lookup("70:B3:D5:12:34:56"); got != "未知" {
			t.Errorf("內建快照不應含虛構長前綴，實際 %q", got)
		}
		// 查不到回傳「未知」
		if got := ouiSvc.Lookup("DE:AD:BE:EF:00:01"); got != "未知" {
			t.Errorf("預期「未知」，實際 %q", got)
		}

		// 先擺放一筆既有 IP 紀錄，上傳新 OUI 後廠商必須重算
		ipID := seeder.SeedIPRecord("10.10.10.42", "aa:bb:cc:11:22:33", subnetID, nil)

		// 模擬管理員上傳新 OUI CSV 檔案
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("file", "custom-oui.csv")
		_, _ = fw.Write([]byte("AABBCC,24,Custom Test Corp\n70B3D5,24,Example 24-bit Vendor\n70B3D51,28,Example 28-bit Vendor\n70B3D5123,36,Example 36-bit Vendor\n"))
		_ = mw.Close()

		reqUpload := testutil.NewAuthRequest(http.MethodPost, "/api/v1/settings/oui", nil, adminID, "admin", "admin")
		reqUpload.Body = http.NoBody
		reqUpload = httptest.NewRequest(http.MethodPost, "/api/v1/settings/oui", &buf)
		reqUpload.Header.Set("Content-Type", mw.FormDataContentType())
		reqUpload.Header.Set("X-Requested-With", "XMLHttpRequest")
		reqUpload.Header.Set("X-Test-User-ID", "1")
		reqUpload.Header.Set("X-Test-Role", "admin")
		reqUpload.Header.Set("X-Test-Username", "admin")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, reqUpload)
		if rec.Code != http.StatusOK {
			t.Fatalf("上傳 OUI 預期 200，實際 %d: %s", rec.Code, rec.Body.String())
		}

		if got := ouiSvc.Lookup("aa:bb:cc:11:22:33"); got != "Custom Test Corp" {
			t.Errorf("上傳後查詢預期 Custom Test Corp，實際 %q", got)
		}

		// 24/28/36 位前綴並存時，以最長前綴優先比對
		for mac, want := range map[string]string{
			"70:B3:D5:12:34:56": "Example 36-bit Vendor",
			"70:B3:D5:1F:00:01": "Example 28-bit Vendor",
			"70:B3:D5:20:00:01": "Example 24-bit Vendor",
		} {
			if got := ouiSvc.Lookup(mac); got != want {
				t.Errorf("查詢 %s 預期 %q，實際 %q", mac, want, got)
			}
		}

		var dbVendor string
		_ = db.QueryRow(`SELECT vendor_name FROM ip_records WHERE id = ?`, ipID).Scan(&dbVendor)
		if dbVendor != "Custom Test Corp" {
			t.Errorf("既有 IP 紀錄應同步更新為 Custom Test Corp，實際 %q", dbVendor)
		}
	})

	// ========================================================
	// 2. 驗證 Ticket 14：自動清除離線未登記紀錄
	// ========================================================
	t.Run("Auto purge offline unregistered records", func(t *testing.T) {
		old91Days := now.AddDate(0, 0, -91)
		old89Days := now.AddDate(0, 0, -89)

		// A: 離線 91 天、未登記 -> 必須被清除
		idPurged := seeder.SeedIPRecord("10.10.10.101", "11:22:33:44:55:01", subnetID, &testutil.IPRecordSeedOpts{
			Presence:     "offline",
			Registration: "unregistered",
			LastSeenAt:   &old91Days,
		})
		seeder.SeedNotificationState("unreg|10.10.10.101|11:22:33:44:55:01", "unregistered_digest", true)

		// B: 離線 89 天、未登記 -> 保留
		idKeptRecent := seeder.SeedIPRecord("10.10.10.102", "11:22:33:44:55:02", subnetID, &testutil.IPRecordSeedOpts{
			Presence:     "offline",
			Registration: "unregistered",
			LastSeenAt:   &old89Days,
		})

		// C: 離線 91 天、已登記 -> 絕對保留
		exp := now.AddDate(1, 0, 0)
		idKeptOwned := seeder.SeedIPRecord("10.10.10.103", "11:22:33:44:55:03", subnetID, &testutil.IPRecordSeedOpts{
			Presence:     "offline",
			Registration: "registered",
			OwnerUserID:  &userID,
			OwnerMail:    "chen@example.com",
			ExpiresAt:    &exp,
			LastSeenAt:   &old91Days,
		})

		// D: 在線超過 91 天未登記 -> 保留
		idKeptOnline := seeder.SeedIPRecord("10.10.10.104", "11:22:33:44:55:04", subnetID, &testutil.IPRecordSeedOpts{
			Presence:     "online",
			Registration: "unregistered",
			LastSeenAt:   &old91Days,
		})

		purgedCount, err := purgeSvc.RunAutoPurge(ctx)
		if err != nil {
			t.Fatalf("執行自動清除失敗: %v", err)
		}
		if purgedCount != 1 {
			t.Fatalf("預期清除 1 筆，實際 %d 筆", purgedCount)
		}

		// 確認該刪除的已刪除，不該刪除的都在
		var exists int
		_ = db.QueryRow(`SELECT COUNT(*) FROM ip_records WHERE id = ?`, idPurged).Scan(&exists)
		if exists != 0 {
			t.Errorf("10.10.10.101 應已被刪除")
		}
		_ = db.QueryRow(`SELECT COUNT(*) FROM notification_states WHERE dedup_key = 'unreg|10.10.10.101|11:22:33:44:55:01'`).Scan(&exists)
		if exists != 0 {
			t.Errorf("連帶通知去重狀態應已被清理")
		}

		for _, keptID := range []int64{idKeptRecent, idKeptOwned, idKeptOnline} {
			_ = db.QueryRow(`SELECT COUNT(*) FROM ip_records WHERE id = ?`, keptID).Scan(&exists)
			if exists != 1 {
				t.Errorf("IP ID %d 不應被刪除", keptID)
			}
		}

		// 設為 0 天代表停用自動清除
		reqDisable := testutil.NewAuthRequest(http.MethodPut, "/api/v1/settings/retention", map[string]int{
			"offline_grace_rounds":          2,
			"auto_purge_days":               0,
			"registration_valid_days":       365,
			"expiry_advance_days":           14,
			"expiry_reminder_interval_days": 7,
			"expiry_max_reminders":          4,
		}, adminID, "admin", "admin")
		recDisable := httptest.NewRecorder()
		router.ServeHTTP(recDisable, reqDisable)
		if recDisable.Code != http.StatusOK {
			t.Fatalf("更新自動清除設定失敗: %d", recDisable.Code)
		}

		clk.Advance(200 * 24 * time.Hour)
		purgedDisabled, _ := purgeSvc.RunAutoPurge(ctx)
		if purgedDisabled != 0 {
			t.Errorf("設為 0 停用後應清除 0 筆，實際 %d", purgedDisabled)
		}
	})

	// ========================================================
	// 3. 驗證 Ticket 08：稽核紀錄查詢與權限隔離
	// ========================================================
	t.Run("Audit log viewer API and RBAC", func(t *testing.T) {
		// 一般使用者回 403
		reqUser := testutil.NewAuthRequest(http.MethodGet, "/api/v1/audit/logs", nil, userID, "user", "chen")
		recUser := httptest.NewRecorder()
		router.ServeHTTP(recUser, reqUser)
		if recUser.Code != http.StatusForbidden {
			t.Errorf("一般使用者查詢稽核紀錄預期 403，實際 %d", recUser.Code)
		}

		// 管理員回 200 並能看到剛剛 OUI 匯入與自動清除的事件
		reqAdmin := testutil.NewAuthRequest(http.MethodGet, "/api/v1/audit/logs?page=1&page_size=20", nil, adminID, "admin", "admin")
		recAdmin := httptest.NewRecorder()
		router.ServeHTTP(recAdmin, reqAdmin)
		if recAdmin.Code != http.StatusOK {
			t.Fatalf("管理員查詢稽核預期 200，實際 %d", recAdmin.Code)
		}

		var body struct {
			Items []struct {
				Action string `json:"action"`
			} `json:"items"`
			Pagination struct {
				TotalItems int `json:"total_items"`
			} `json:"pagination"`
		}
		if err := json.NewDecoder(recAdmin.Body).Decode(&body); err != nil {
			t.Fatalf("解析稽核回應失敗: %v", err)
		}
		if body.Pagination.TotalItems < 2 {
			t.Errorf("預期至少 2 筆稽核紀錄，實際 %d", body.Pagination.TotalItems)
		}
	})
}
