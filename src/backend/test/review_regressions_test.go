package test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/audit"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/config"
	"github.com/jeffhou369/wurzburg/backend/internal/database"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
	"github.com/jeffhou369/wurzburg/backend/internal/iprecords"
	"github.com/jeffhou369/wurzburg/backend/internal/mailer"
	"github.com/jeffhou369/wurzburg/backend/internal/notifications"
	"github.com/jeffhou369/wurzburg/backend/internal/server"
	"github.com/jeffhou369/wurzburg/backend/internal/testutil"
	"github.com/jeffhou369/wurzburg/backend/internal/wizard"
	"github.com/jeffhou369/wurzburg/backend/migrations"
	"github.com/testcontainers/testcontainers-go"
	mariadbmod "github.com/testcontainers/testcontainers-go/modules/mariadb"
)

type reviewFixture struct {
	db            *sql.DB
	clk           *clock.TestClock
	srv           *server.Server
	router        http.Handler
	seed          *testutil.Seeder
	adminID       int64
	mail          *interfaces.FakeMailer
	notifications *notifications.Service
}

func newReviewFixture(t *testing.T) *reviewFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("略過需要 MariaDB 容器的回歸測試")
	}
	ctx := context.Background()
	container, err := mariadbmod.Run(ctx, "mariadb:11.4", mariadbmod.WithDatabase("wurzburg"), mariadbmod.WithUsername("wurzburg"), mariadbmod.WithPassword("wurzburg_test_pw"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	dsn, err := container.ConnectionString(ctx, "parseTime=true&loc=UTC")
	if err != nil {
		t.Fatal(err)
	}
	db, err := database.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := database.RunMigrations(db, migrations.FS, "."); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewTestClock(time.Date(2026, 11, 16, 1, 0, 0, 0, time.UTC))
	srv := server.New(&config.Config{AppEnv: "test", SecretKey: "0123456789abcdef0123456789abcdef"}, db, clk)
	seed := testutil.NewSeeder(t, db, clk)
	fm := interfaces.NewFakeMailer()
	mh := mailer.NewHandler(db, clk, srv.Settings, srv.Audit, mailer.NewSMTPMailer(db, clk, srv.Settings))
	mh.SetTestMailer(fm)
	srv.Mount(wizard.NewHandler(db, clk, srv.Settings, srv.Audit), mh, iprecords.NewHandler(db, clk, srv.Settings, srv.Audit), audit.NewHandler(db))
	return &reviewFixture{db: db, clk: clk, srv: srv, router: apiRouter(t, srv), seed: seed, adminID: seed.SeedAdmin("review-admin"), mail: fm, notifications: notifications.NewService(db, clk, srv.Settings, srv.Audit, fm)}
}

func (f *reviewFixture) request(method, path string, body any, userID int64, role string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, testutil.NewAuthRequest(method, path, body, userID, role, "review-user"))
	return w
}

func TestReviewPublicURL(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	campus := map[string]any{"campus_name": "測試園區", "timezone": "Asia/Taipei", "public_base_url": "https://ip.example.test"}
	w := f.request(http.MethodPut, "/api/v1/settings/campus", campus, f.adminID, "admin")
	if w.Code != http.StatusOK {
		t.Fatalf("儲存公開網址失敗：%d %s", w.Code, w.Body.String())
	}
	var saved map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved["public_base_url"] != "https://ip.example.test" {
		t.Fatalf("API 必須回傳儲存的公開網址：%s", w.Body.String())
	}

	// 舊精靈未送新欄位時不得覆寫既有公開網址。
	w = f.request(http.MethodPut, "/api/v1/settings/campus", map[string]string{"campus_name": "另一個名稱", "timezone": "UTC"}, f.adminID, "admin")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "https://ip.example.test") {
		t.Fatalf("省略欄位應保留公開網址：%s", w.Body.String())
	}

	for _, bad := range []string{"javascript:alert(1)", "https://user:pass@ip.example.test", "//ip.example.test", "https://ip.example.test/?token=secret", "https://ip.example.test/#fragment", "https://ip.example.test/subpath"} {
		campus["public_base_url"] = bad
		w = f.request(http.MethodPut, "/api/v1/settings/campus", campus, f.adminID, "admin")
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("無效公開網址應回 422：%q，得到 %d", bad, w.Code)
		}
	}

	w = f.request(http.MethodPost, "/api/v1/settings/mail/test", map[string]string{"recipient": "verify@example.test"}, f.adminID, "admin")
	if w.Code != http.StatusOK || f.mail.Count() != 1 {
		t.Fatalf("測試信失敗：%s", w.Body.String())
	}
	msg := f.mail.PopAll()[0]
	if !strings.Contains(msg.TextBody, "https://ip.example.test/") || strings.Contains(msg.HTMLBody, "localhost") {
		t.Fatal("測試信必須使用設定網址")
	}

	if err := f.srv.Settings.Set(ctx, "admin_recipients", `["admin@example.test"]`); err != nil {
		t.Fatal(err)
	}
	sub := f.seed.SeedSubnet("10.40.0.0/24", "managed", nil)
	f.seed.SeedIPRecord("10.40.0.10", "00:11:22:33:44:10", sub, nil)
	if _, err := f.notifications.RunUnregisteredDigest(ctx); err != nil {
		t.Fatal(err)
	}
	msg = f.mail.PopAll()[0]
	if !strings.Contains(msg.TextBody, "https://ip.example.test/?presence=online&registration=unregistered") {
		t.Fatal("未登記彙整連結必須使用既有路由與篩選")
	}

	if _, err := f.notifications.RunWeeklySummary(ctx); err != nil {
		t.Fatal(err)
	}
	weekly := f.mail.PopAll()
	if len(weekly) != 1 || !strings.Contains(weekly[0].TextBody, "https://ip.example.test/?presence=online&registration=unregistered") {
		t.Fatal("未登記總表也必須使用園區公開網址")
	}

	owner := f.seed.SeedUser("review-owner", "user", "ad", "owner@example.test", "擁有者")
	expiry := f.clk.Now().AddDate(0, 0, 14)
	f.seed.SeedIPRecord("10.40.0.20", "00:11:22:33:44:20", sub, &testutil.IPRecordSeedOpts{Registration: "registered", OwnerUserID: &owner, OwnerMail: "owner@example.test", ExpiresAt: &expiry})
	if _, err := f.notifications.RunExpiryReminders(ctx); err != nil {
		t.Fatal(err)
	}
	msg = f.mail.PopAll()[0]
	if !strings.Contains(msg.TextBody, "https://ip.example.test/?owner_me=true&pending_confirmation=true") || strings.Contains(msg.TextBody, "/workbench") {
		t.Fatal("催認信連結必須帶入自己的待確認清單")
	}
}
