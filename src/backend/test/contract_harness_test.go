package test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/jeffhou369/wurzburg/backend/internal/server"
	"github.com/jeffhou369/wurzburg/backend/internal/testutil/apicontract"
)

// apiRouter 取得伺服器的路由器，並掛上 OpenAPI 回應契約檢查：
// 測試送出的每一次 API 請求，回應都要符合 docs/api/openapi.yaml，否則測試失敗。
// 整合測試一律經由這裡取得路由器，不直接呼叫 apiRouter(t, srv)。
func apiRouter(t testing.TB, srv *server.Server) http.Handler {
	t.Helper()
	checker, err := apicontract.Shared()
	if err != nil {
		t.Fatalf("載入 OpenAPI 描述檔失敗: %v", err)
	}
	return checker.Wrap(t, srv.Router(), isTestOnlyRoute)
}

// isTestOnlyRoute 列出不屬於產品 API 契約的路由：伺服器內建的權限檢測示範端點，
// 以及測試自行掛載、用來觀察中介軟體行為的端點。
func isTestOnlyRoute(r *http.Request) bool {
	p := r.URL.Path
	return p == "/api/v1/auth/check" || p == "/api/v1/auth/admin-check" ||
		p == "/api/v1/custom-ping" || strings.HasPrefix(p, "/api/v1/test-")
}
