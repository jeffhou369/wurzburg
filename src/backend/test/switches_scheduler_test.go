package test

import (
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
	"github.com/jeffhou369/wurzburg/backend/internal/crypto"
	"github.com/jeffhou369/wurzburg/backend/internal/database"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
	"github.com/jeffhou369/wurzburg/backend/internal/pipeline"
	"github.com/jeffhou369/wurzburg/backend/internal/scheduler"
	"github.com/jeffhou369/wurzburg/backend/internal/server"
	"github.com/jeffhou369/wurzburg/backend/internal/switches"
	"github.com/jeffhou369/wurzburg/backend/internal/testutil"
	"github.com/jeffhou369/wurzburg/backend/migrations"
	"github.com/testcontainers/testcontainers-go"
	mariadbmod "github.com/testcontainers/testcontainers-go/modules/mariadb"
)

func TestSwitchesAndSchedulerIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("略過需啟動 Docker 容器的交換器與排程整合測試")
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
	ciph, _ := crypto.NewCipher("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")

	fakeCol := interfaces.NewFakeCollector()
	srv := server.New(&config.Config{Port: 8080, CampusName: "Würzburg", AppEnv: "test"}, db, clk)
	swHandler := switches.NewHandler(db, clk, ciph, srv.Audit, fakeCol)
	pipe := pipeline.New(nil, nil)
	sched := scheduler.New(db, clk, ciph, srv.Settings, srv.Audit, fakeCol, pipe)
	srv.Mount(swHandler, sched)
	router := srv.Router()

	seeder := testutil.NewSeeder(t, db, clk)
	adminID := seeder.SeedAdmin("admin")

	// ========================================================
	// 1. 驗證 Ticket 09：交換器與 VRF 讀取設定 CRUD、憑證加密不回顯、測試讀取
	// ========================================================
	t.Run("Switch and VRF CRUD and Test Read", func(t *testing.T) {
		// 新增一台交換器（包含 v2c community 密碼）
		createReq := testutil.NewAuthRequest(http.MethodPost, "/api/v1/switches", map[string]any{
			"name":           "core-sw-01",
			"mgmt_ip":        "10.0.0.1",
			"platform":       "ios-xe",
			"snmp_version":   "v2c",
			"snmp_port":      161,
			"timeout_ms":     3000,
			"role_label":     "核心",
			"snmp_community": "MySecretCommunity#99",
		}, adminID, "admin", "admin")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, createReq)
		if rec.Code != http.StatusCreated {
			t.Fatalf("建立交換器預期 201，實際 %d: %s", rec.Code, rec.Body.String())
		}

		var swData map[string]any
		_ = json.NewDecoder(rec.Body).Decode(&swData)
		swID := int64(swData["id"].(float64))

		// 檢查回應絕不回顯密碼明文
		rawResp := rec.Body.String()
		if strings.Contains(rawResp, "MySecretCommunity#99") {
			t.Fatalf("API 回應外洩 SNMP Community 明文: %s", rawResp)
		}

		// 確認自動帶有預設 default VRF
		vrfs := swData["vrf_configs"].([]any)
		if len(vrfs) != 1 {
			t.Fatalf("新增交換器應自動附帶 1 個預設 VRF，實際 %d", len(vrfs))
		}
		defVRF := vrfs[0].(map[string]any)
		defVRFID := int64(defVRF["id"].(float64))

		// 嘗試刪除預設 default VRF 必須遭拒絕 (409)
		reqDelDef := testutil.NewAuthRequest(http.MethodDelete, fmt.Sprintf("/api/v1/switches/%d/vrfs/%d", swID, defVRFID), nil, adminID, "admin", "admin")
		recDelDef := httptest.NewRecorder()
		router.ServeHTTP(recDelDef, reqDelDef)
		if recDelDef.Code != http.StatusConflict {
			t.Fatalf("刪除預設 VRF 應回 409，實際 %d", recDelDef.Code)
		}

		// 為交換器新增第二筆 VRF 讀取設定 (CORP)
		reqAddVRF := testutil.NewAuthRequest(http.MethodPost, fmt.Sprintf("/api/v1/switches/%d/vrfs", swID), map[string]any{
			"vrf_name":       "CORP",
			"snmp_context":   "CORP",
			"snmp_community": "corp_community",
		}, adminID, "admin", "admin")
		recAddVRF := httptest.NewRecorder()
		router.ServeHTTP(recAddVRF, reqAddVRF)
		if recAddVRF.Code != http.StatusCreated {
			t.Fatalf("新增 VRF 設定失敗: %d %s", recAddVRF.Code, recAddVRF.Body.String())
		}

		// 測試讀取端點
		fakeCol.Outputs[defVRFID] = &interfaces.PollOutput{
			VRFConfigID: defVRFID,
			ARPObservations: []interfaces.ARPObservation{
				{IPAddress: "10.10.10.50", MACAddress: "b8:27:eb:11:22:33"},
			},
			InterfaceAddresses: []interfaces.InterfaceAddress{
				{IPAddress: "10.10.10.1", NetMask: "255.255.255.0"},
			},
		}

		reqTest := testutil.NewAuthRequest(http.MethodPost, fmt.Sprintf("/api/v1/switches/%d/vrfs/%d/test-read", swID, defVRFID), nil, adminID, "admin", "admin")
		recTest := httptest.NewRecorder()
		router.ServeHTTP(recTest, reqTest)
		if recTest.Code != http.StatusOK {
			t.Fatalf("測試讀取預期 200，實際 %d: %s", recTest.Code, recTest.Body.String())
		}
		var testResult map[string]any
		_ = json.NewDecoder(recTest.Body).Decode(&testResult)
		if testResult["success"] != true || testResult["arp_count"].(float64) != 1 {
			t.Fatalf("測試讀取結果不符: %+v", testResult)
		}

		// 模擬停用交換器，名下 ARP 觀測應被清空且 IP 變離線
		subnetID := seeder.SeedSubnet("10.10.10.0/24", "managed", nil)
		ipID := seeder.SeedIPRecord("10.10.10.50", "b8:27:eb:11:22:33", subnetID, nil)
		seeder.SeedSighting(ipID, defVRFID, swID, "b8:27:eb:11:22:33", "Vlan10", 0)

		disableReq := testutil.NewAuthRequest(http.MethodPut, fmt.Sprintf("/api/v1/switches/%d", swID), map[string]any{
			"name":         "core-sw-01",
			"mgmt_ip":      "10.0.0.1",
			"platform":     "ios-xe",
			"snmp_version": "v2c",
			"enabled":      false,
		}, adminID, "admin", "admin")
		recDis := httptest.NewRecorder()
		router.ServeHTTP(recDis, disableReq)
		if recDis.Code != http.StatusOK {
			t.Fatalf("停用交換器失敗: %d %s", recDis.Code, recDis.Body.String())
		}

		var sightingCount int
		_ = db.QueryRow(`SELECT COUNT(*) FROM arp_sightings WHERE switch_id = ?`, swID).Scan(&sightingCount)
		if sightingCount != 0 {
			t.Fatalf("交換器停用後其名下觀測應全數清除，剩餘 %d", sightingCount)
		}
		var presence string
		_ = db.QueryRow(`SELECT presence FROM ip_records WHERE id = ?`, ipID).Scan(&presence)
		if presence != "offline" {
			t.Fatalf("唯一的觀測來源停用後，IP 紀錄應自動改為離線，實際: %s", presence)
		}
	})

	// ========================================================
	// 2. 驗證 Ticket 11：排程器、重試、並行控管與讀取結果紀錄
	// ========================================================
	t.Run("Scheduler cycle, retries, and poll run logs", func(t *testing.T) {
		sw2ID, vrf2ID := seeder.SeedSwitch("core-sw-02", "10.0.0.2", "ios-xe", "v2c")

		// 設定第一次錯誤、重試時成功
		failThenSuccessCol := &retryFakeCollector{
			failCount: 1,
			successOut: &interfaces.PollOutput{
				VRFConfigID: vrf2ID,
				SwitchID:    sw2ID,
				ARPObservations: []interfaces.ARPObservation{
					{IPAddress: "10.10.20.10", MACAddress: "00:11:22:33:44:55"},
				},
			},
		}
		schedWithRetry := scheduler.New(db, clk, ciph, srv.Settings, srv.Audit, failThenSuccessCol, pipe)

		// 執行單輪
		if err := schedWithRetry.RunCycle(ctx); err != nil {
			t.Fatalf("RunCycle 失敗: %v", err)
		}

		// 檢查 poll_runs 有記錄且標註 retried = TRUE
		var isSuccess, retried bool
		var learnedCount int
		err = db.QueryRow(`SELECT is_success, retried, learned_count FROM poll_runs WHERE vrf_config_id = ? ORDER BY id DESC LIMIT 1`, vrf2ID).
			Scan(&isSuccess, &retried, &learnedCount)
		if err != nil {
			t.Fatalf("查詢 poll_runs 失敗: %v", err)
		}
		if !isSuccess || !retried || learnedCount != 1 {
			t.Errorf("預期重試成功且學習 1 筆，實際 is_success=%v retried=%v learned=%d", isSuccess, retried, learnedCount)
		}

		// 手動觸發立即同步 API
		reqTrigger := testutil.NewAuthRequest(http.MethodPost, "/api/v1/sync/trigger", nil, adminID, "admin", "admin")
		recTrig := httptest.NewRecorder()
		router.ServeHTTP(recTrig, reqTrigger)
		if recTrig.Code != http.StatusAccepted {
			t.Fatalf("手動觸發預期 202，實際 %d", recTrig.Code)
		}
	})
}

type retryFakeCollector struct {
	failCount  int
	successOut *interfaces.PollOutput
}

func (r *retryFakeCollector) Collect(_ context.Context, _ interfaces.VRFReadInput) (*interfaces.PollOutput, error) {
	if r.failCount > 0 {
		r.failCount--
		return nil, &interfaces.CollectError{Class: interfaces.ErrClassTimeout, Summary: "首次逾時"}
	}
	return r.successOut, nil
}
