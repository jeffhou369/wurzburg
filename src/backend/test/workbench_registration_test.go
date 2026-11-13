package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/config"
	"github.com/jeffhou369/wurzburg/backend/internal/dashboard"
	"github.com/jeffhou369/wurzburg/backend/internal/database"
	"github.com/jeffhou369/wurzburg/backend/internal/iprecords"
	"github.com/jeffhou369/wurzburg/backend/internal/server"
	"github.com/jeffhou369/wurzburg/backend/internal/testutil"
	"github.com/jeffhou369/wurzburg/backend/migrations"
	"github.com/testcontainers/testcontainers-go"
	mariadbmod "github.com/testcontainers/testcontainers-go/modules/mariadb"
	"github.com/xuri/excelize/v2"
)

func TestWorkbenchRegistrationAndExport(t *testing.T) {
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

	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	clk := clock.NewTestClock(now)

	srv := server.New(&config.Config{Port: 8080, CampusName: "Würzburg", AppEnv: "test"}, db, clk)
	dashHandler := dashboard.NewHandler(db, clk, nil)
	ipHandler := iprecords.NewHandler(db, clk, srv.Settings, srv.Audit)
	srv.Mount(dashHandler, ipHandler)
	router := apiRouter(t, srv)

	seeder := testutil.NewSeeder(t, db, clk)
	adminID := seeder.SeedAdmin("admin")
	user1ID := seeder.SeedUser("chen", "user", "ad", "chen@example.com", "陳工程師")
	user2ID := seeder.SeedUser("lin", "user", "ad", "lin@example.com", "林設計師")

	subID := seeder.SeedSubnet("10.10.10.0/24", "managed", nil)
	swID, vrfID := seeder.SeedSwitch("core-01", "10.0.0.1", "ios-xe", "v3")

	// 佈署三筆 IP 紀錄
	ip1 := seeder.SeedIPRecord("10.10.10.50", "b8:27:eb:11:22:33", subID, &testutil.IPRecordSeedOpts{
		Presence:     "online",
		Registration: "unregistered",
		Notes:        "=cmd|' /C calc'!A0", // 測試 Excel 公式跳脫
	})
	seeder.SeedSighting(ip1, vrfID, swID, "b8:27:eb:11:22:33", "Vlan10", 0)

	expPast := now.Add(-5 * 24 * time.Hour)
	ip2 := seeder.SeedIPRecord("10.10.10.60", "00:50:56:11:22:33", subID, &testutil.IPRecordSeedOpts{
		Presence:     "offline",
		Registration: "expired",
		OwnerUserID:  &user1ID,
		OwnerMail:    "chen@example.com",
		ExpiresAt:    &expPast,
	})

	ip3 := seeder.SeedIPRecord("10.10.10.70", "70:df:2f:aa:33:03", subID, &testutil.IPRecordSeedOpts{
		Presence:     "online",
		Registration: "registered",
		OwnerUserID:  &user2ID,
		OwnerMail:    "lin@example.com",
	})
	seeder.SeedSighting(ip3, vrfID, swID, "70:df:2f:aa:33:03", "Vlan10", 0)

	// ========================================================
	// 1. 驗證 Ticket 16：儀表板 KPI 統計
	// ========================================================
	t.Run("Dashboard stats calculation", func(t *testing.T) {
		req := testutil.NewAuthRequest(http.MethodGet, "/api/v1/dashboard/stats", nil, user1ID, "user", "chen")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("查詢儀表板失敗: %d", rec.Code)
		}
		var stats map[string]any
		_ = json.NewDecoder(rec.Body).Decode(&stats)

		if stats["managed_total"].(float64) != 3 || stats["online_count"].(float64) != 2 || stats["offline_count"].(float64) != 1 {
			t.Errorf("儀表板總數與在線統計不符: %+v", stats)
		}
		if stats["unregistered_online_count"].(float64) != 1 || stats["expired_count"].(float64) != 1 {
			t.Errorf("未登記在線或到期數不符: %+v", stats)
		}
	})

	// ========================================================
	// 2. 驗證 Ticket 15：IP 表格分頁、篩選與狀態組合
	// ========================================================
	t.Run("IP Table List and State Combinations", func(t *testing.T) {
		req := testutil.NewAuthRequest(http.MethodGet, "/api/v1/ip-records?presence=online", nil, user1ID, "user", "chen")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("查詢 IP 列表失敗: %d", rec.Code)
		}
		var resp struct {
			Items []struct {
				IPAddress  string `json:"ip_address"`
				StateLabel string `json:"state_label"`
			} `json:"items"`
		}
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		if len(resp.Items) != 2 {
			t.Fatalf("在線篩選預期 2 筆，實際 %d 筆", len(resp.Items))
		}
		if !strings.Contains(resp.Items[0].StateLabel, "在線") {
			t.Errorf("狀態標籤應包含在線: %s", resp.Items[0].StateLabel)
		}
	})

	// ========================================================
	// 3. 驗證 Ticket 18：登記動作與權限（包含 403 與格式驗證）
	// ========================================================
	t.Run("Single Registration and Authorization", func(t *testing.T) {
		// user1 登記未登記的 ip1 -> 成功成為擁有者
		regReq := testutil.NewAuthRequest(http.MethodPost, "/api/v1/ip-records/actions/register", map[string]any{
			"ip_record_ids": []int64{ip1},
			"owner_mail":    "mydept@example.com", // 允許共用信箱
			"device_type":   "測試主機",
			"notes":         "=cmd|' /C calc'!A0",
		}, user1ID, "user", "chen")
		recReg := httptest.NewRecorder()
		router.ServeHTTP(recReg, regReq)
		if recReg.Code != http.StatusOK {
			t.Fatalf("登記失敗: %d %s", recReg.Code, recReg.Body.String())
		}

		// user2 試圖編輯 user1 的 ip1 -> 403
		editReq := testutil.NewAuthRequest(http.MethodPut, fmt.Sprintf("/api/v1/ip-records/%d", ip1), map[string]any{
			"owner_mail": "intruder@example.com",
		}, user2ID, "user", "lin")
		recEdit := httptest.NewRecorder()
		router.ServeHTTP(recEdit, editReq)
		if recEdit.Code != http.StatusForbidden {
			t.Fatalf("編輯他人登記預期回 403，實際 %d", recEdit.Code)
		}

		// user1 重新確認已到期的 ip2 -> 到期日延長至 1 年後，狀態回到已登記
		confReq := testutil.NewAuthRequest(http.MethodPost, "/api/v1/ip-records/actions/confirm", map[string]any{
			"ip_record_ids": []int64{ip2},
		}, user1ID, "user", "chen")
		recConf := httptest.NewRecorder()
		router.ServeHTTP(recConf, confReq)
		if recConf.Code != http.StatusOK {
			t.Fatalf("重新確認失敗: %d %s", recConf.Code, recConf.Body.String())
		}
		var expAfter time.Time
		_ = db.QueryRow(`SELECT expires_at FROM ip_records WHERE id = ?`, ip2).Scan(&expAfter)
		if expAfter.Before(now.Add(360 * 24 * time.Hour)) {
			t.Fatalf("重新確認後到期日未展延 1 年: %v", expAfter)
		}
	})

	// ========================================================
	// 4. 驗證 Ticket 26：Excel 匯出與公式防注入
	// ========================================================
	t.Run("Excel Export and Formula Sanitization", func(t *testing.T) {
		req := testutil.NewAuthRequest(http.MethodGet, "/api/v1/ip-records/export", nil, user1ID, "user", "chen")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("Excel 匯出失敗: %d", rec.Code)
		}

		xf, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
		if err != nil {
			t.Fatalf("解析匯出之 Excel 失敗: %v", err)
		}
		defer func() { _ = xf.Close() }()

		rows, err := xf.GetRows("IP管理表")
		if err != nil || len(rows) < 4 { // 標題 + 3 筆資料
			t.Fatalf("Excel 內容列數不足: %d, err=%v", len(rows), err)
		}

		// 檢查公式防注入（以 = 開頭的備註應被加上 ' 單引號保護）
		foundSanitized := false
		for _, row := range rows {
			for _, cell := range row {
				if strings.HasPrefix(cell, "'=cmd") {
					foundSanitized = true
				}
			}
		}
		if !foundSanitized {
			t.Error("Excel 匯出未對以 = 開頭之公式字串進行安全跳脫")
		}
	})

	// ========================================================
	// 5. 驗證 Ticket 27：批次操作（全選與改派）
	// ========================================================
	t.Run("Batch Operations and Admin Reassign", func(t *testing.T) {
		// 一般使用者嘗試批次改派 -> 403
		reqReassignUser := testutil.NewAuthRequest(http.MethodPost, "/api/v1/ip-records/actions/reassign", map[string]any{
			"ip_record_ids":   []int64{ip1},
			"target_username": "lin",
		}, user1ID, "user", "chen")
		recReassignUser := httptest.NewRecorder()
		router.ServeHTTP(recReassignUser, reqReassignUser)
		if recReassignUser.Code != http.StatusForbidden {
			t.Fatalf("一般使用者改派預期 403，實際 %d", recReassignUser.Code)
		}

		// 系統管理員批次改派 ip1 與 ip2 予 lin -> 成功
		reqReassignAdmin := testutil.NewAuthRequest(http.MethodPost, "/api/v1/ip-records/actions/reassign", map[string]any{
			"ip_record_ids":   []int64{ip1, ip2},
			"target_username": "lin",
		}, adminID, "admin", "admin")
		recReassignAdmin := httptest.NewRecorder()
		router.ServeHTTP(recReassignAdmin, reqReassignAdmin)
		if recReassignAdmin.Code != http.StatusOK {
			t.Fatalf("管理員改派失敗: %d %s", recReassignAdmin.Code, recReassignAdmin.Body.String())
		}

		var ownerNow int64
		_ = db.QueryRow(`SELECT owner_user_id FROM ip_records WHERE id = ?`, ip1).Scan(&ownerNow)
		if ownerNow != user2ID {
			t.Fatalf("改派後擁有者應為 lin (%d)，實際 %d", user2ID, ownerNow)
		}
	})
}
