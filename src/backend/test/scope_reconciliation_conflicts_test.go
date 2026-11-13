package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/config"
	"github.com/jeffhou369/wurzburg/backend/internal/conflicts"
	"github.com/jeffhou369/wurzburg/backend/internal/database"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
	"github.com/jeffhou369/wurzburg/backend/internal/reconciliation"
	"github.com/jeffhou369/wurzburg/backend/internal/server"
	"github.com/jeffhou369/wurzburg/backend/internal/subnets"
	"github.com/jeffhou369/wurzburg/backend/internal/testutil"
	"github.com/jeffhou369/wurzburg/backend/migrations"
	"github.com/testcontainers/testcontainers-go"
	mariadbmod "github.com/testcontainers/testcontainers-go/modules/mariadb"
	"strings"
)

func TestScopeReconciliationAndConflicts(t *testing.T) {
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
	scopeSvc := subnets.NewScopeService(db, clk, srv.Audit, "test_token_secret_32bytes_length!!")
	resolver := subnets.NewResolver(db)
	fakeOUI := interfaces.NewFakeOUILookup()
	fakeOUI.Vendors["b8:27:eb"] = "Raspberry Pi"
	reconcileSvc := reconciliation.NewService(db, clk, srv.Settings, resolver, fakeOUI)
	conflictsHandler := conflicts.NewHandler(db, clk, srv.Audit, fakeOUI)

	srv.Mount(scopeSvc, conflictsHandler)
	router := apiRouter(t, srv)

	seeder := testutil.NewSeeder(t, db, clk)
	adminID := seeder.SeedAdmin("admin")
	userID := seeder.SeedUser("chen", "user", "ad", "chen@example.com", "陳工程師")
	swID, vrfDefaultID := seeder.SeedSwitch("core-sw-01", "10.0.0.1", "ios-xe", "v3")

	// ========================================================
	// 1. 驗證 Ticket 12：網段發現、三種狀態、閘道位址與未定前綴
	// ========================================================
	t.Run("Subnet Discovery and State Transitions", func(t *testing.T) {
		pollOutput := &interfaces.PollOutput{
			VRFConfigID: vrfDefaultID,
			SwitchID:    swID,
			VRFName:     "default",
			InterfaceAddresses: []interfaces.InterfaceAddress{
				{IPAddress: "10.10.10.1", NetMask: "255.255.255.0", IfIndex: 11, IfName: "Vlan10"},
				{IPAddress: "10.10.20.1", NetMask: "255.255.255.0", IfIndex: 12, IfName: "Vlan20"},
			},
			ARPObservations: []interfaces.ARPObservation{
				{IPAddress: "10.10.10.50", MACAddress: "b8:27:eb:11:22:33", IfIndex: 11, IfName: "Vlan10"},
				{IPAddress: "192.168.99.10", MACAddress: "00:11:22:33:44:55", IfIndex: 10, IfName: "GigabitEthernet1/0/1"}, // 未定前綴
			},
		}

		if err := scopeSvc.ApplyInterfaceAddresses(ctx, []*interfaces.PollOutput{pollOutput}); err != nil {
			t.Fatalf("發現網段失敗: %v", err)
		}

		// 檢查發現的網段狀態為 pending（待檢視）
		var sID int64
		var sState string
		if err := db.QueryRow(`SELECT id, state FROM subnets WHERE cidr = '10.10.10.0/24'`).Scan(&sID, &sState); err != nil || sState != "pending" {
			t.Fatalf("發現之 10.10.10.0/24 狀態應為 pending，實際 %s, err=%v", sState, err)
		}

		// 檢查閘道位址已記錄且不建 IP 紀錄
		var gwCount int
		_ = db.QueryRow(`SELECT COUNT(*) FROM subnet_gateways WHERE subnet_id = ? AND ip_address = '10.10.10.1'`, sID).Scan(&gwCount)
		if gwCount != 1 {
			t.Fatal("未在 subnet_gateways 找到閘道 10.10.10.1")
		}

		// 整併執行（此時網段仍為 pending，故 10.10.10.50 不應產生 IP 紀錄；192.168.99.10 應進未定前綴）
		if err := reconcileSvc.ReconcileObservations(ctx, []*interfaces.PollOutput{pollOutput}, nil); err != nil {
			t.Fatalf("整併失敗: %v", err)
		}
		var ipCount int
		_ = db.QueryRow(`SELECT COUNT(*) FROM ip_records WHERE ip_address = '10.10.10.50'`).Scan(&ipCount)
		if ipCount != 0 {
			t.Fatal("pending 網段內的位址不應建立 IP 紀錄")
		}
		var undetCount int
		_ = db.QueryRow(`SELECT COUNT(*) FROM undetermined_prefixes WHERE ip_address = '192.168.99.10'`).Scan(&undetCount)
		if undetCount != 1 {
			t.Fatal("未知網段之 192.168.99.10 應出現在未定前綴清單")
		}

		// 將 10.10.10.0/24 設為納管 (managed)
		reqManage := testutil.NewAuthRequest(http.MethodPut, "/api/v1/subnets/1/state", map[string]any{"state": "managed"}, adminID, "admin", "admin")
		recManage := httptest.NewRecorder()
		router.ServeHTTP(recManage, reqManage)
		if recManage.Code != http.StatusOK {
			t.Fatalf("設為納管失敗: %d %s", recManage.Code, recManage.Body.String())
		}

		// 再次整併 -> 10.10.10.50 應成功建立 IP 紀錄
		_ = reconcileSvc.ReconcileObservations(ctx, []*interfaces.PollOutput{pollOutput}, nil)
		_ = db.QueryRow(`SELECT COUNT(*) FROM ip_records WHERE ip_address = '10.10.10.50'`).Scan(&ipCount)
		if ipCount != 1 {
			t.Fatal("納管網段內的位址應成功建立 IP 紀錄")
		}
	})

	// ========================================================
	// 2. 驗證 Ticket 24：排除範圍、刪除前預覽與確認、明細稽核
	// ========================================================
	t.Run("Exclusion Ranges and Delete Preview/Confirm", func(t *testing.T) {
		// 先為 10.10.10.50 登記擁有者
		exp := now.AddDate(1, 0, 0)
		_, _ = db.Exec(`UPDATE ip_records SET registration = 'registered', owner_user_id = ?, owner_mail = 'chen@example.com', expires_at = ? WHERE ip_address = '10.10.10.50'`, userID, exp)

		// 1. 預覽新增排除範圍 10.10.10.40 - 10.10.10.60
		reqPrev := testutil.NewAuthRequest(http.MethodPost, "/api/v1/subnets/1/exclusions/preview", map[string]any{
			"start_ip": "10.10.10.40",
			"end_ip":   "10.10.10.60",
			"reason":   "dhcp",
		}, adminID, "admin", "admin")
		recPrev := httptest.NewRecorder()
		router.ServeHTTP(recPrev, reqPrev)
		if recPrev.Code != http.StatusOK {
			t.Fatalf("預覽排除範圍失敗: %d %s", recPrev.Code, recPrev.Body.String())
		}

		var prevResp map[string]any
		_ = json.NewDecoder(recPrev.Body).Decode(&prevResp)
		if prevResp["total_ip_records"].(float64) != 1 || prevResp["owned_ip_records"].(float64) != 1 {
			t.Fatalf("預覽筆數不符: %+v", prevResp)
		}
		previewToken := prevResp["preview_token"].(string)

		// 未帶 preview_token 或 token 不符應遭拒絕
		reqBadToken := testutil.NewAuthRequest(http.MethodPost, "/api/v1/subnets/1/exclusions", map[string]any{
			"start_ip":      "10.10.10.40",
			"end_ip":        "10.10.10.60",
			"reason":        "dhcp",
			"preview_token": "wrong_token",
		}, adminID, "admin", "admin")
		recBadToken := httptest.NewRecorder()
		router.ServeHTTP(recBadToken, reqBadToken)
		if recBadToken.Code != http.StatusConflict {
			t.Fatalf("未帶正確 token 應回 409，實際: %d", recBadToken.Code)
		}

		// 帶正確 token 確認新增排除範圍 -> 10.10.10.50 應被直接刪除，稽核包含明細
		reqConfirm := testutil.NewAuthRequest(http.MethodPost, "/api/v1/subnets/1/exclusions", map[string]any{
			"start_ip":      "10.10.10.40",
			"end_ip":        "10.10.10.60",
			"reason":        "dhcp",
			"preview_token": previewToken,
		}, adminID, "admin", "admin")
		recConfirm := httptest.NewRecorder()
		router.ServeHTTP(recConfirm, reqConfirm)
		if recConfirm.Code != http.StatusCreated {
			t.Fatalf("確認新增排除失敗: %d %s", recConfirm.Code, recConfirm.Body.String())
		}

		var recExists int
		_ = db.QueryRow(`SELECT COUNT(*) FROM ip_records WHERE ip_address = '10.10.10.50'`).Scan(&recExists)
		if recExists != 0 {
			t.Fatal("排除範圍內的 10.10.10.50 應已被直接刪除")
		}

		// 驗證稽核紀錄包含被刪除已登記者之明細
		var auditDetails string
		_ = db.QueryRow(`SELECT details_json FROM audit_logs WHERE action = 'exclusion.create_delete' ORDER BY id DESC LIMIT 1`).Scan(&auditDetails)
		if !strings.Contains(auditDetails, "10.10.10.50") || !strings.Contains(auditDetails, "chen@example.com") {
			t.Fatalf("稽核日誌缺少刪除已登記者明細: %s", auditDetails)
		}
	})

	// ========================================================
	// 3. 驗證 Ticket 13, 25, 31：多來源在線/離線、衝突偵測與三種裁決
	// ========================================================
	t.Run("Sightings, Offline Grace, Conflicts Detection and Resolution", func(t *testing.T) {
		sub3 := seeder.SeedSubnet("10.10.30.0/24", "managed", nil)
		ipID := seeder.SeedIPRecord("10.10.30.100", "00:11:22:33:44:55", sub3, &testutil.IPRecordSeedOpts{
			Presence:     "online",
			Registration: "registered",
			OwnerUserID:  &userID,
			OwnerMail:    "chen@example.com",
		})

		// 第一輪：回報新的 MAC (66:77:88:99:aa:bb) -> 已登記者換 MAC 必須開立衝突
		sw2ID, vrf2ID := seeder.SeedSwitch("core-sw-02", "10.0.0.2", "ios-xe", "v3")
		cycle1 := &interfaces.PollOutput{
			VRFConfigID: vrf2ID,
			SwitchID:    sw2ID,
			VRFName:     "default",
			ARPObservations: []interfaces.ARPObservation{
				{IPAddress: "10.10.30.100", MACAddress: "66:77:88:99:aa:bb", IfIndex: 13, IfName: "Vlan30"},
			},
		}
		_ = reconcileSvc.ReconcileObservations(ctx, []*interfaces.PollOutput{cycle1}, nil)

		var confID int64
		var confStatus string
		err := db.QueryRow(`SELECT id, status FROM ip_conflicts WHERE ip_record_id = ?`, ipID).Scan(&confID, &confStatus)
		if err != nil || confStatus != "open" {
			t.Fatalf("已登記位址出現不同 MAC 應開立 open 衝突，實際: %s, err=%v", confStatus, err)
		}

		// 系統管理員裁決為「維持原本 MAC」(keep_adopted) -> 衝突進入 in_progress
		reqResolveKeep := testutil.NewAuthRequest(http.MethodPost, fmt.Sprintf("/api/v1/conflicts/%d/resolve", confID), map[string]string{
			"resolution": "keep_adopted",
			"note":       "現場排查中，維持原本登記",
		}, adminID, "admin", "admin")
		recResolveKeep := httptest.NewRecorder()
		router.ServeHTTP(recResolveKeep, reqResolveKeep)
		if recResolveKeep.Code != http.StatusOK {
			t.Fatalf("裁決失敗: %d %s", recResolveKeep.Code, recResolveKeep.Body.String())
		}
		_ = db.QueryRow(`SELECT status FROM ip_conflicts WHERE id = ?`, confID).Scan(&confStatus)
		if confStatus != "in_progress" {
			t.Fatalf("維持原本 MAC 裁決後衝突應為 in_progress，實際: %s", confStatus)
		}

		// 模擬該來源連續 2 輪不再回報 10.10.30.100 -> 觀測移除，衝突自動已解消 (keep_adopted)
		emptyCycle := &interfaces.PollOutput{
			VRFConfigID:     vrf2ID,
			SwitchID:        sw2ID,
			VRFName:         "default",
			ARPObservations: []interfaces.ARPObservation{},
		}
		_ = reconcileSvc.ReconcileObservations(ctx, []*interfaces.PollOutput{emptyCycle}, nil)
		_ = reconcileSvc.ReconcileObservations(ctx, []*interfaces.PollOutput{emptyCycle}, nil)

		var confRes string
		_ = db.QueryRow(`SELECT status, resolution FROM ip_conflicts WHERE id = ?`, confID).Scan(&confStatus, &confRes)
		if confStatus != "resolved" || confRes != "keep_adopted" {
			t.Fatalf("另一 MAC 消失後衝突應自動已解消，實際 status=%s resolution=%s", confStatus, confRes)
		}
	})
}
