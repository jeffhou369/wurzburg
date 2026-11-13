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
	"github.com/jeffhou369/wurzburg/backend/migrations"
	"github.com/testcontainers/testcontainers-go"
	mariadbmod "github.com/testcontainers/testcontainers-go/modules/mariadb"
)

func TestPrimarySeam_HealthEndpointsAndClock(t *testing.T) {
	cfg := &config.Config{
		Port:       8080,
		CampusName: "Würzburg 測試園區",
		AppEnv:     "test",
	}

	initialTime := time.Date(2026, 11, 12, 8, 30, 0, 0, time.UTC)
	testClock := clock.NewTestClock(initialTime)

	srv := server.New(cfg, nil, testClock)
	router := apiRouter(t, srv)

	// 1. 測試 /healthz 基礎存活檢查
	t.Run("GET /healthz", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("預期狀態碼 200，實際取得: %d", rec.Code)
		}
		if body := rec.Body.String(); body != `{"status":"ok"}` {
			t.Fatalf("預期回應 %s，實際取得: %s", `{"status":"ok"}`, body)
		}
	})

	// 2. 測試 /api/v1/health 詳細健康檢查端點
	t.Run("GET /api/v1/health 驗證時間與無機敏資訊洩漏", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("預期狀態碼 200，實際取得: %d", rec.Code)
		}

		var resp server.HealthResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("解析 JSON 回應失敗: %v", err)
		}

		if resp.Status != "ok" {
			t.Errorf("預期 status 為 ok，實際取得: %s", resp.Status)
		}
		if !resp.Timestamp.Equal(initialTime) {
			t.Errorf("預期時間 %v，實際取得: %v", initialTime, resp.Timestamp)
		}
		if resp.Campus != "Würzburg 測試園區" {
			t.Errorf("預期 campus 為 Würzburg 測試園區，實際取得: %s", resp.Campus)
		}

		// 推進可控時鐘 2 小時
		testClock.Advance(2 * time.Hour)
		expectedTime := initialTime.Add(2 * time.Hour)

		rec2 := httptest.NewRecorder()
		router.ServeHTTP(rec2, req)

		var resp2 server.HealthResponse
		if err := json.NewDecoder(rec2.Body).Decode(&resp2); err != nil {
			t.Fatalf("解析第二次回應失敗: %v", err)
		}
		if !resp2.Timestamp.Equal(expectedTime) {
			t.Errorf("時間推進後預期 %v，實際取得: %v", expectedTime, resp2.Timestamp)
		}
	})

	// 3. 測試前端 SPA 回退處理
	t.Run("GET /non-api-path SPA 回退", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/some/angular/route", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("預期狀態碼 200，實際取得: %d", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
			t.Errorf("預期 Content-Type 為 text/html，實際取得: %s", ct)
		}
	})
}

func TestPrimarySeam_MariaDBAndMigrations(t *testing.T) {
	if testing.Short() {
		t.Skip("略過需啟動 Docker 容器的整合測試")
	}

	ctx := context.Background()

	// 啟動真實 MariaDB 容器進行端到端整合測試
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
		if err := testcontainers.TerminateContainer(mariadbContainer); err != nil {
			t.Logf("終止 MariaDB 容器失敗: %v", err)
		}
	})

	dsn, err := mariadbContainer.ConnectionString(ctx, "parseTime=true&loc=UTC")
	if err != nil {
		t.Fatalf("取得 MariaDB 連線字串失敗: %v", err)
	}

	// 1. 測試資料庫連線池
	db, err := database.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("連線至測試 MariaDB 失敗: %v", err)
	}
	defer func() { _ = db.Close() }()

	// 2. 測試套用內嵌 Goose 遷移
	if err := database.RunMigrations(db, migrations.FS, "."); err != nil {
		t.Fatalf("套用資料庫遷移失敗: %v", err)
	}

	// 3. 驗證遷移建立的初始檢查資料表
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_initial_check").Scan(&count); err != nil {
		t.Fatalf("查詢 schema_initial_check 資料表失敗: %v", err)
	}

	// 4. 以接上真實資料庫的伺服器測試主要接縫
	cfg := &config.Config{
		Port:       8080,
		CampusName: "Würzburg 整合測試",
		AppEnv:     "test",
	}
	srv := server.New(cfg, db, clock.NewRealClock())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	apiRouter(t, srv).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("預期狀態碼 200，實際取得: %d", rec.Code)
	}

	var resp server.HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("解析回應失敗: %v", err)
	}
	if resp.Database != "connected" {
		t.Errorf("預期 database 狀態為 connected，實際為: %s", resp.Database)
	}
}
