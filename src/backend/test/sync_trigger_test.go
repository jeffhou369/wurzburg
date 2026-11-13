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
	"github.com/jeffhou369/wurzburg/backend/internal/crypto"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
	"github.com/jeffhou369/wurzburg/backend/internal/pipeline"
	"github.com/jeffhou369/wurzburg/backend/internal/scheduler"
	"github.com/jeffhou369/wurzburg/backend/internal/server"
	"github.com/jeffhou369/wurzburg/backend/internal/testutil"
)

// gatedCollector 在 release 關閉前不回傳，讓測試能觀察「同步進行中」的狀態。
type gatedCollector struct {
	started chan struct{}
	release chan struct{}
}

func (g *gatedCollector) Collect(ctx context.Context, in interfaces.VRFReadInput) (*interfaces.PollOutput, error) {
	select {
	case g.started <- struct{}{}:
	default:
	}
	select {
	case <-g.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &interfaces.PollOutput{VRFConfigID: in.VRFConfigID, SwitchID: in.SwitchID, VRFName: in.VRFName}, nil
}

// 手動觸發同步後，呼叫端要能靠同步狀態知道這一輪何時結束（設定精靈與交換器頁面都這樣做）。
// 所以 POST /sync/trigger 回應時，狀態必須已經是進行中；若等背景工作啟動後才標記，
// 緊接著查狀態會看到「沒有在同步」，呼叫端就會在這一輪還沒開始時誤以為已經結束。
func TestSyncTriggerReportsInProgressUntilCycleFinishes(t *testing.T) {
	if testing.Short() {
		t.Skip("略過需啟動 Docker 容器的整合測試")
	}
	db := startContractDB(t)
	clk := clock.NewTestClock(time.Date(2026, 11, 16, 1, 0, 0, 0, time.UTC))
	const secretKey = "0123456789abcdef0123456789abcdef"
	ciph, err := crypto.NewCipher(secretKey)
	if err != nil {
		t.Fatal(err)
	}
	srv := server.New(&config.Config{AppEnv: "test", SecretKey: secretKey}, db, clk)
	col := &gatedCollector{started: make(chan struct{}, 1), release: make(chan struct{})}
	sched := scheduler.New(db, clk, ciph, srv.Settings, srv.Audit, col, pipeline.New(nil, nil))
	srv.Mount(sched)
	router := apiRouter(t, srv)

	seed := testutil.NewSeeder(t, db, clk)
	adminID := seed.SeedAdmin("sync-admin")
	seed.SeedSwitch("sync-sw-01", "10.254.1.1", "ios-xe", "v2c")

	do := func(method, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, testutil.NewAuthRequest(method, path, nil, adminID, "admin", "sync-admin"))
		return rec
	}
	status := func() map[string]any {
		rec := do(http.MethodGet, "/api/v1/sync/status")
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("解析同步狀態失敗: %v（%s）", err, rec.Body.String())
		}
		return out
	}

	if st := status(); st["in_progress"] != false || st["last_sync_at"] != nil {
		t.Fatalf("尚未同步過時的狀態不符：%v", st)
	}

	if rec := do(http.MethodPost, "/api/v1/sync/trigger"); rec.Code != http.StatusAccepted {
		t.Fatalf("觸發同步應回 202，實際 %d %s", rec.Code, rec.Body.String())
	}
	// 回應的當下就要是進行中，不可等背景工作排到才標記
	if st := status(); st["in_progress"] != true {
		t.Fatalf("觸發同步回應後，狀態應立刻是進行中：%v", st)
	}
	if rec := do(http.MethodPost, "/api/v1/sync/trigger"); rec.Code != http.StatusConflict {
		t.Errorf("同步進行中再次觸發應回 409，實際 %d", rec.Code)
	}

	select {
	case <-col.started:
	case <-time.After(10 * time.Second):
		t.Fatal("背景的同步輪次沒有開始讀取")
	}
	close(col.release)

	deadline := time.Now().Add(10 * time.Second)
	for status()["in_progress"] == true {
		if time.Now().After(deadline) {
			t.Fatal("同步輪次結束後，狀態應回到未進行中")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st := status(); st["last_sync_at"] == nil || st["last_sync_success"] != true {
		t.Errorf("同步結束後應記錄完成時間與結果：%v", st)
	}
}
