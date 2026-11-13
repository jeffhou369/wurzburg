package test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/accounts"
	"github.com/jeffhou369/wurzburg/backend/internal/auth"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/config"
	"github.com/jeffhou369/wurzburg/backend/internal/database"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
	"github.com/jeffhou369/wurzburg/backend/internal/server"
	"github.com/jeffhou369/wurzburg/backend/internal/testutil"
	"github.com/jeffhou369/wurzburg/backend/migrations"
	"github.com/testcontainers/testcontainers-go"
	mariadbmod "github.com/testcontainers/testcontainers-go/modules/mariadb"
)

func postJSON(router http.Handler, path string, payload any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestAccounts_LoginSessionsAndAdminLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("略過需啟動 Docker 容器的登入與帳號整合測試")
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

	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	clk := clock.NewTestClock(now)

	// 1. 透過環境變數建立第一位系統管理員
	if err := accounts.EnsureFirstAdmin(ctx, db, clk, "rootadmin", "InitialStrongPass#1"); err != nil {
		t.Fatalf("EnsureFirstAdmin 失敗: %v", err)
	}

	// 若再次執行 EnsureFirstAdmin（即使換不同密碼），不可新增或覆蓋
	if err := accounts.EnsureFirstAdmin(ctx, db, clk, "rootadmin", "DifferentPassword#99"); err != nil {
		t.Fatalf("EnsureFirstAdmin 重複執行錯誤: %v", err)
	}

	cfg := &config.Config{
		Port:       8080,
		CampusName: "Würzburg",
		SecretKey:  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		AppEnv:     "test",
	}

	fakeAuth := interfaces.NewFakeAuthSource()
	fakeAuth.Users["chen"] = "OrgUserPass123!"
	fakeAuth.Info["chen"] = &interfaces.OrgUserInfo{
		Username:    "chen",
		DisplayName: "陳工程師",
		Mail:        "chen@example.com",
		AuthSource:  "ad",
	}

	srv := server.New(cfg, db, clk)
	accHandler := accounts.NewHandler(db, clk, srv.Sessions, srv.Settings, srv.Audit, fakeAuth)
	srv.Mount(accHandler)
	router := apiRouter(t, srv)

	var adminCookie *http.Cookie

	// 2. 系統管理員從「組織帳號」登入必須失敗
	recWrongPath := postJSON(router, "/api/v1/auth/login/org", map[string]string{
		"username": "rootadmin",
		"password": "InitialStrongPass#1",
	})
	if recWrongPath.Code == http.StatusOK {
		t.Fatal("本機系統管理員不應從組織帳號路徑登入成功")
	}

	// 3. 以環境變數帳密登入成功，並檢查強制改密碼阻擋
	recLogin := postJSON(router, "/api/v1/auth/login/local", map[string]string{
		"username": "rootadmin",
		"password": "InitialStrongPass#1",
	})
	if recLogin.Code != http.StatusOK {
		t.Fatalf("管理員首次登入預期 200，實際 %d: %s", recLogin.Code, recLogin.Body.String())
	}
	for _, c := range recLogin.Result().Cookies() {
		if c.Name == auth.CookieName {
			adminCookie = c
		}
	}
	if adminCookie == nil {
		t.Fatal("未收到 Session Cookie")
	}

	// 改密碼前呼叫 /api/v1/admins 應回 403 password_change_required
	reqAdmins := httptest.NewRequest(http.MethodGet, "/api/v1/admins", nil)
	reqAdmins.AddCookie(adminCookie)
	recBlocked := httptest.NewRecorder()
	router.ServeHTTP(recBlocked, reqAdmins)
	if recBlocked.Code != http.StatusForbidden {
		t.Fatalf("未改密碼前預期回 403，實際 %d", recBlocked.Code)
	}

	// 新密碼不符長度政策應回 422
	recShortPw := postJSON(router, "/api/v1/auth/change-password", map[string]string{
		"new_password": "short",
	}, adminCookie)
	if recShortPw.Code != http.StatusUnprocessableEntity {
		t.Fatalf("過短密碼預期回 422，實際 %d", recShortPw.Code)
	}

	// 改密碼成功後才能使用管理 API
	recGoodPw := postJSON(router, "/api/v1/auth/change-password", map[string]string{
		"new_password": "NewStrongPassword#2026",
	}, adminCookie)
	if recGoodPw.Code != http.StatusOK {
		t.Fatalf("修改密碼預期 200，實際 %d: %s", recGoodPw.Code, recGoodPw.Body.String())
	}

	recAllowed := httptest.NewRecorder()
	router.ServeHTTP(recAllowed, reqAdmins)
	if recAllowed.Code != http.StatusOK {
		t.Fatalf("改完密碼後預期取得管理員列表 200，實際 %d", recAllowed.Code)
	}

	// 4. 建立第二位管理員，停用她；停用最後一位管理員必須遭拒絕 (409)
	recCreate := postJSON(router, "/api/v1/admins", map[string]string{
		"username":     "secadmin",
		"password":     "SecondAdminPass#2026",
		"display_name": "次要管理員",
	}, adminCookie)
	if recCreate.Code != http.StatusCreated {
		t.Fatalf("建立第二位管理員失敗: %d %s", recCreate.Code, recCreate.Body.String())
	}

	var secID, rootID int64
	_ = db.QueryRow(`SELECT id FROM users WHERE username = 'secadmin'`).Scan(&secID)
	_ = db.QueryRow(`SELECT id FROM users WHERE username = 'rootadmin'`).Scan(&rootID)

	// 停用第二位管理員 -> 200
	recDisableSec := postJSON(router, "/api/v1/admins/"+testutil.NewAuthRequest("GET", "/", nil, secID, "admin", "").Header.Get("X-Test-User-ID")+"/toggle", map[string]bool{"enabled": false}, adminCookie)
	if recDisableSec.Code != http.StatusOK {
		t.Fatalf("停用第二位管理員失敗: %d %s", recDisableSec.Code, recDisableSec.Body.String())
	}

	// 嘗試停用最後一位啟用中的管理員 rootadmin -> 409
	recDisableRoot := postJSON(router, "/api/v1/admins/"+testutil.NewAuthRequest("GET", "/", nil, rootID, "admin", "").Header.Get("X-Test-User-ID")+"/toggle", map[string]bool{"enabled": false}, adminCookie)
	if recDisableRoot.Code != http.StatusConflict {
		t.Fatalf("停用最後一位管理員預期回 409，實際 %d: %s", recDisableRoot.Code, recDisableRoot.Body.String())
	}

	// 5. 連續失敗鎖定（預設 5 次鎖定 15 分鐘）
	for i := 0; i < 5; i++ {
		recFail := postJSON(router, "/api/v1/auth/login/local", map[string]string{
			"username": "rootadmin",
			"password": "WrongPassword1234",
		})
		if recFail.Code != http.StatusUnauthorized {
			t.Fatalf("第 %d 次錯誤預期 401，實際 %d", i+1, recFail.Code)
		}
	}

	// 第 6 次就算密碼正確也必須被鎖定 (423)
	recLocked := postJSON(router, "/api/v1/auth/login/local", map[string]string{
		"username": "rootadmin",
		"password": "NewStrongPassword#2026",
	})
	if recLocked.Code != 423 {
		t.Fatalf("鎖定期間預期回 423，實際 %d", recLocked.Code)
	}

	// 推進時間 16 分鐘後鎖定解除
	clk.Advance(16 * time.Minute)
	recUnlocked := postJSON(router, "/api/v1/auth/login/local", map[string]string{
		"username": "rootadmin",
		"password": "NewStrongPassword#2026",
	})
	if recUnlocked.Code != http.StatusOK {
		t.Fatalf("解鎖後預期登入 200，實際 %d", recUnlocked.Code)
	}

	// 6. 驗證主機端 ResetAdminPassword 指令函式
	if err := accounts.ResetAdminPassword(ctx, db, clk, "rootadmin", "CliResetPass#2026", 12); err != nil {
		t.Fatalf("CLI 重設密碼失敗: %v", err)
	}
	recCliLogin := postJSON(router, "/api/v1/auth/login/local", map[string]string{
		"username": "rootadmin",
		"password": "CliResetPass#2026",
	})
	if recCliLogin.Code != http.StatusOK {
		t.Fatalf("CLI 重設後預期登入 200，實際 %d", recCliLogin.Code)
	}

	// 7. 驗證登出後原本的 Session Cookie 立即失效
	recLogout := postJSON(router, "/api/v1/auth/logout", nil, adminCookie)
	if recLogout.Code != http.StatusNoContent {
		t.Fatalf("登出預期 204，實際 %d", recLogout.Code)
	}
	recAfterLogout := httptest.NewRecorder()
	router.ServeHTTP(recAfterLogout, reqAdmins)
	if recAfterLogout.Code != http.StatusUnauthorized {
		t.Fatalf("登出後再呼叫 API 預期回 401，實際 %d", recAfterLogout.Code)
	}

	// 8. 組織帳號第一次登入自動建立使用者
	_ = srv.Settings.Set(ctx, "active_auth_source", "ad")
	recOrgLogin := postJSON(router, "/api/v1/auth/login/org", map[string]string{
		"username": "chen",
		"password": "OrgUserPass123!",
	})
	if recOrgLogin.Code != http.StatusOK {
		t.Fatalf("組織帳號首次登入預期 200，實際 %d: %s", recOrgLogin.Code, recOrgLogin.Body.String())
	}

	var orgMail string
	if err := db.QueryRow(`SELECT mail FROM users WHERE username = 'chen' AND auth_source = 'ad'`).Scan(&orgMail); err != nil || orgMail != "chen@example.com" {
		t.Fatalf("組織帳號自動建立後信箱不符: %q, err=%v", orgMail, err)
	}

	// 9. 確認稽核紀錄有記錄登入事件且完全不含密碼明文
	rows, err := db.Query(`SELECT summary, details_json FROM audit_logs`)
	if err != nil {
		t.Fatalf("查詢稽核失敗: %v", err)
	}
	defer func() { _ = rows.Close() }()
	auditCount := 0
	for rows.Next() {
		var summary, details string
		_ = rows.Scan(&summary, &details)
		auditCount++
		if strings.Contains(summary+details, "InitialStrongPass") ||
			strings.Contains(summary+details, "NewStrongPassword") ||
			strings.Contains(summary+details, "OrgUserPass123") ||
			strings.Contains(summary+details, "WrongPassword") {
			t.Fatalf("稽核日誌不應包含密碼: %s %s", summary, details)
		}
	}
	if auditCount == 0 {
		t.Fatal("預期應寫入至少一筆登入稽核紀錄")
	}
}
