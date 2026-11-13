package test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/accounts"
	"github.com/jeffhou369/wurzburg/backend/internal/audit"
	"github.com/jeffhou369/wurzburg/backend/internal/authsource"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/config"
	"github.com/jeffhou369/wurzburg/backend/internal/conflicts"
	"github.com/jeffhou369/wurzburg/backend/internal/database"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
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
	"github.com/jeffhou369/wurzburg/backend/internal/testutil"
	"github.com/jeffhou369/wurzburg/backend/internal/wizard"
	"github.com/jeffhou369/wurzburg/backend/migrations"
	"github.com/testcontainers/testcontainers-go"
	mariadbmod "github.com/testcontainers/testcontainers-go/modules/mariadb"
)

// startContractDB 啟動一個套用完遷移的 MariaDB 容器。
func startContractDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	container, err := mariadbmod.Run(ctx, "mariadb:11.4",
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
	db, err := connectTestDB(t, ctx, dsn)
	if err != nil {
		t.Fatalf("連線資料庫失敗: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := database.RunMigrations(db, migrations.FS, "."); err != nil {
		t.Fatalf("遷移失敗: %v", err)
	}
	return db
}

// TestAPIContractCoverage 走過其他整合測試沒有打到的操作。
//
// 這裡的重點不是業務規則（各功能的測試已涵蓋），而是讓 OpenAPI 描述檔的每個操作都至少有一個
// 帶有實際資料的回應經過 apiRouter 的契約檢查；TestMain 會確認沒有操作被漏掉。
// 回應內容由契約檢查器比對描述檔，測試本身只斷言狀態碼。
func TestAPIContractCoverage(t *testing.T) {
	if testing.Short() {
		t.Skip("略過需啟動 Docker 容器的整合測試")
	}
	ctx := context.Background()
	db := startContractDB(t)
	clk := clock.NewTestClock(time.Date(2026, 11, 16, 1, 0, 0, 0, time.UTC))
	secretKey := "0123456789abcdef0123456789abcdef"

	srv := server.New(&config.Config{Port: 8080, CampusName: "Würzburg", AppEnv: "test", SecretKey: secretKey}, db, clk)
	fakeMailer := newFakeMailer(t)
	ouiSvc := oui.NewService(db, clk, srv.Audit)
	if err := ouiSvc.EnsureLoaded(ctx); err != nil {
		t.Fatalf("載入內建 OUI 資料失敗: %v", err)
	}
	authSrc := authsource.NewDynamicManager(db, srv.Settings, srv.Audit)
	mailHandler := mailer.NewHandler(db, clk, srv.Settings, srv.Audit, mailer.NewSMTPMailer(db, clk, srv.Settings))
	mailHandler.SetTestMailer(fakeMailer)
	scopeSvc := subnets.NewScopeService(db, clk, srv.Audit, secretKey)
	collector := interfaces.NewFakeCollector()
	reconcileSvc := reconciliation.NewService(db, clk, srv.Settings, subnets.NewResolver(db), ouiSvc)
	sched := scheduler.New(db, clk, srv.Cipher, srv.Settings, srv.Audit, collector, pipeline.New(scopeSvc, reconcileSvc))
	srv.Mount(
		authSrc,
		sched,
		accounts.NewHandler(db, clk, srv.Sessions, srv.Settings, srv.Audit, authSrc),
		audit.NewHandler(db),
		purge.NewService(db, clk, srv.Settings, srv.Audit),
		scopeSvc,
		conflicts.NewHandler(db, clk, srv.Audit, ouiSvc),
		switches.NewHandler(db, clk, srv.Cipher, srv.Audit, collector),
		ouiSvc,
		mailHandler,
		wizard.NewHandler(db, clk, srv.Settings, srv.Audit),
	)
	router := apiRouter(t, srv)

	seed := testutil.NewSeeder(t, db, clk)
	adminID := seed.SeedAdmin("contract-admin")
	swID, _ := seed.SeedSwitch("contract-sw-01", "10.254.0.1", "ios-xe", "v2c")
	extraVRFID := seed.SeedVRFConfig(swID, "corp", "vrf-corp")
	subnetID := seed.SeedSubnet("10.61.0.0/24", "managed", nil)
	exclusionID := seed.SeedExclusionRange(subnetID, "10.61.0.200", "10.61.0.210", "dhcp")
	seed.SeedGateway(subnetID, "10.61.0.254", "Vlan61", swID)
	ownerID := seed.SeedUser("contract-owner", "user", "ad", "owner@example.test", "契約測試擁有者")
	registeredID := seed.SeedIPRecord("10.61.0.10", "02:61:00:00:00:10", subnetID, &testutil.IPRecordSeedOpts{
		Presence: "online", Registration: "registered", OwnerUserID: &ownerID, OwnerMail: "owner@example.test",
	})
	conflictID := seed.SeedConflict(registeredID, "10.61.0.10", "02:61:00:00:00:10", []string{"02:61:00:00:00:99"}, "open")
	seed.SeedIPRecord("10.61.0.20", "02:61:00:00:00:20", subnetID, &testutil.IPRecordSeedOpts{Presence: "online", Registration: "unregistered"})
	if _, err := db.ExecContext(ctx,
		`INSERT INTO undetermined_prefixes (ip_address, mac_address, switch_id, vrf_name, if_name, last_seen_at, created_at)
		 VALUES ('192.0.2.77', '02:61:00:00:00:77', ?, 'default', 'Vlan77', ?, ?)`, swID, clk.Now(), clk.Now()); err != nil {
		t.Fatalf("擺設未定前綴失敗: %v", err)
	}

	do := func(method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, testutil.NewAuthRequest(method, path, body, adminID, "admin", "contract-admin"))
		return rec
	}
	expect := func(method, path string, body any, want int) {
		t.Helper()
		if rec := do(method, path, body); rec.Code != want {
			t.Errorf("%s %s：預期 %d，實際 %d %s", method, path, want, rec.Code, rec.Body.String())
		}
	}

	t.Run("目前登入者", func(t *testing.T) {
		expect(http.MethodGet, "/api/v1/auth/me", nil, http.StatusOK)
	})

	t.Run("驗證來源設定", func(t *testing.T) {
		expect(http.MethodGet, "/api/v1/settings/auth-source", nil, http.StatusOK)
		expect(http.MethodPut, "/api/v1/settings/auth-source", map[string]any{
			"active_source":    "ad",
			"ad_url":           "ldap://127.0.0.1:1", // 沒有服務在聽的埠：連線立即被拒，測試驗證來源時得到失敗結果
			"ad_use_tls":       false,
			"ad_bind_dn":       "cn=svc,dc=example,dc=test",
			"ad_bind_password": "ContractBindSecret#39",
			"ad_base_dn":       "dc=example,dc=test",
			"ad_user_attr":     "sAMAccountName",
			"ad_mail_attr":     "mail",
			"ad_name_attr":     "displayName",
		}, http.StatusOK)
		expect(http.MethodGet, "/api/v1/settings/auth-source", nil, http.StatusOK)
		expect(http.MethodPost, "/api/v1/settings/auth-source/test", map[string]any{
			"username": "someone", "password": "ContractProbePass#39",
		}, http.StatusOK)
	})

	t.Run("保留政策與 OUI 狀態", func(t *testing.T) {
		expect(http.MethodGet, "/api/v1/settings/retention", nil, http.StatusOK)
		expect(http.MethodGet, "/api/v1/settings/oui", nil, http.StatusOK)
	})

	t.Run("通知紀錄與通知狀態", func(t *testing.T) {
		expect(http.MethodPut, "/api/v1/settings/campus", map[string]any{
			"campus_name": "契約測試園區", "timezone": "Asia/Taipei", "public_base_url": "https://ip.example.test",
		}, http.StatusOK)
		expect(http.MethodPut, "/api/v1/settings/mail", map[string]any{
			"smtp_host": "smtp.example.test", "smtp_port": 587, "smtp_tls_mode": "starttls",
			"sender_address": "ipmgmt@example.test", "admin_recipients": []string{"netadmin@example.test"},
			"weekly_summary_cron": "0 9 * * 1",
		}, http.StatusOK)
		// 讓通知服務實際寄出一封未登記彙整，產生一筆通知紀錄與通知狀態
		notif := notifications.NewService(db, clk, srv.Settings, srv.Audit, fakeMailer)
		if _, err := notif.RunUnregisteredDigest(ctx); err != nil {
			t.Fatalf("寄送未登記彙整失敗: %v", err)
		}
		if fakeMailer.Count() == 0 {
			t.Fatal("應寄出一封未登記彙整，通知紀錄清單才有資料可驗證")
		}
		expect(http.MethodGet, "/api/v1/notifications/logs", nil, http.StatusOK)
		expect(http.MethodGet, "/api/v1/notifications/states", nil, http.StatusOK)
	})

	t.Run("同步狀態", func(t *testing.T) {
		// 尚未執行過同步（last_sync_at 為 null）與執行一輪之後，兩種回應都要符合描述檔
		expect(http.MethodGet, "/api/v1/sync/status", nil, http.StatusOK)
		if err := sched.RunCycle(ctx); err != nil {
			t.Fatalf("執行同步輪次失敗: %v", err)
		}
		expect(http.MethodGet, "/api/v1/sync/status", nil, http.StatusOK)
	})

	t.Run("衝突詳情", func(t *testing.T) {
		expect(http.MethodGet, fmt.Sprintf("/api/v1/conflicts/%d", conflictID), nil, http.StatusOK)
	})

	t.Run("未定前綴", func(t *testing.T) {
		expect(http.MethodGet, "/api/v1/subnets/undetermined", nil, http.StatusOK)
	})

	t.Run("刪除排除範圍", func(t *testing.T) {
		expect(http.MethodDelete, fmt.Sprintf("/api/v1/subnets/%d/exclusions/%d", subnetID, exclusionID), nil, http.StatusNoContent)
	})

	t.Run("交換器與 VRF 讀取設定", func(t *testing.T) {
		expect(http.MethodGet, fmt.Sprintf("/api/v1/switches/%d", swID), nil, http.StatusOK)
		expect(http.MethodPut, fmt.Sprintf("/api/v1/switches/%d/vrfs/%d", swID, extraVRFID), map[string]any{
			"vrf_name": "corp", "snmp_context": "vrf-corp-2", "enabled": false,
		}, http.StatusOK)
		expect(http.MethodDelete, fmt.Sprintf("/api/v1/switches/%d", swID), nil, http.StatusNoContent)
	})
}
