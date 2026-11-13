package test

import (
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeffhou369/wurzburg/backend/internal/accounts"
	"github.com/jeffhou369/wurzburg/backend/internal/audit"
	"github.com/jeffhou369/wurzburg/backend/internal/authsource"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/config"
	"github.com/jeffhou369/wurzburg/backend/internal/conflicts"
	"github.com/jeffhou369/wurzburg/backend/internal/dashboard"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
	"github.com/jeffhou369/wurzburg/backend/internal/iprecords"
	"github.com/jeffhou369/wurzburg/backend/internal/mailer"
	"github.com/jeffhou369/wurzburg/backend/internal/oui"
	"github.com/jeffhou369/wurzburg/backend/internal/pipeline"
	"github.com/jeffhou369/wurzburg/backend/internal/purge"
	"github.com/jeffhou369/wurzburg/backend/internal/reconciliation"
	"github.com/jeffhou369/wurzburg/backend/internal/scheduler"
	"github.com/jeffhou369/wurzburg/backend/internal/server"
	"github.com/jeffhou369/wurzburg/backend/internal/subnets"
	"github.com/jeffhou369/wurzburg/backend/internal/switches"
	"github.com/jeffhou369/wurzburg/backend/internal/testutil/apicontract"
	"github.com/jeffhou369/wurzburg/backend/internal/wizard"
)

// TestRegisteredRoutesMatchSpec 比對「後端實際註冊的路由」與 OpenAPI 描述檔，兩邊必須完全一致。
//
// apiRouter 的回應檢查只看得到測試有打到的請求；這個測試補上另一個方向：
// 後端多註冊了描述檔沒有的端點（前端若使用它，就沒有任何型別與契約保護），或描述檔寫了後端沒有的端點。
// 不需要資料庫：只建立路由，不送出請求。
//
// 模組清單要與 cmd/wurzburg/modules.go 的 mountFeatureModules 一致；新增功能模組時兩邊都要加。
func TestRegisteredRoutesMatchSpec(t *testing.T) {
	clk := clock.NewTestClock(time.Date(2026, 11, 16, 1, 0, 0, 0, time.UTC))
	srv := server.New(&config.Config{AppEnv: "production", SecretKey: "0123456789abcdef0123456789abcdef"}, nil, clk)

	ouiSvc := oui.NewService(nil, clk, srv.Audit)
	authSrc := authsource.NewDynamicManager(nil, srv.Settings, srv.Audit)
	scopeSvc := subnets.NewScopeService(nil, clk, srv.Audit, "")
	reconcileSvc := reconciliation.NewService(nil, clk, srv.Settings, subnets.NewResolver(nil), ouiSvc)
	collector := interfaces.NewFakeCollector()
	sched := scheduler.New(nil, clk, srv.Cipher, srv.Settings, srv.Audit, collector, pipeline.New(scopeSvc, reconcileSvc))
	srv.Mount(
		accounts.NewHandler(nil, clk, srv.Sessions, srv.Settings, srv.Audit, authSrc),
		authSrc,
		audit.NewHandler(nil),
		ouiSvc,
		purge.NewService(nil, clk, srv.Settings, srv.Audit),
		switches.NewHandler(nil, clk, srv.Cipher, srv.Audit, collector),
		sched,
		scopeSvc,
		conflicts.NewHandler(nil, clk, srv.Audit, ouiSvc),
		dashboard.NewHandler(nil, clk, sched),
		iprecords.NewHandler(nil, clk, srv.Settings, srv.Audit),
		mailer.NewHandler(nil, clk, srv.Settings, srv.Audit, mailer.NewSMTPMailer(nil, clk, srv.Settings)),
		wizard.NewHandler(nil, clk, srv.Settings, srv.Audit),
	)

	// 伺服器內建的權限檢測示範端點，供基礎測試與診斷使用，不屬於產品契約。
	internalOnly := map[string]bool{
		"GET /api/v1/auth/check":        true,
		"POST /api/v1/auth/admin-check": true,
	}

	registered := map[string]bool{}
	routes, ok := srv.Router().(chi.Routes)
	if !ok {
		t.Fatal("伺服器的路由器應可列舉已註冊的路由")
	}
	err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.TrimSuffix(route, "/")
		key := method + " " + route
		if apicontract.InScope(route) && !internalOnly[key] {
			registered[key] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("列舉路由失敗: %v", err)
	}

	checker, err := apicontract.Shared()
	if err != nil {
		t.Fatalf("載入 OpenAPI 描述檔失敗: %v", err)
	}
	documented := map[string]bool{}
	for _, op := range checker.Operations() {
		documented[op] = true
	}

	var undocumented, unimplemented []string
	for op := range registered {
		if !documented[op] {
			undocumented = append(undocumented, op)
		}
	}
	for op := range documented {
		if !registered[op] {
			unimplemented = append(unimplemented, op)
		}
	}
	sort.Strings(undocumented)
	sort.Strings(unimplemented)
	for _, op := range undocumented {
		t.Errorf("後端註冊了描述檔沒有定義的端點：%s", op)
	}
	for _, op := range unimplemented {
		t.Errorf("描述檔定義了後端沒有註冊的端點：%s", op)
	}
}
