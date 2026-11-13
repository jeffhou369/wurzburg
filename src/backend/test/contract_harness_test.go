package test

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"testing"

	"github.com/jeffhou369/wurzburg/backend/internal/database"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
	"github.com/jeffhou369/wurzburg/backend/internal/server"
	"github.com/jeffhou369/wurzburg/backend/internal/testutil/apicontract"
	"github.com/jeffhou369/wurzburg/backend/internal/testutil/secretscan"
)

// 本檔是整合測試共用的入口。測試一律經由這裡取得路由器、郵件替身與資料庫連線，
// 讓每一次 HTTP 交換、每一封信與每個測試資料庫都自動經過 OpenAPI 契約檢查與 Secret 掃描。

// apiRouter 取得伺服器的路由器，並掛上兩項檢查：
//   - OpenAPI 回應契約：每個 API 回應都要符合 docs/api/openapi.yaml。
//   - Secret 掃描：請求本文中的 Secret 欄位會被登錄，之後任何回應或請求 URL 出現它就算洩漏。
//
// 整合測試不直接呼叫 srv.Router()。
func apiRouter(t testing.TB, srv *server.Server) http.Handler {
	t.Helper()
	checker, err := apicontract.Shared()
	if err != nil {
		t.Fatalf("載入 OpenAPI 描述檔失敗: %v", err)
	}
	return secretscan.Shared().Wrap(t, checker.Wrap(t, srv.Router(), isTestOnlyRoute))
}

// isTestOnlyRoute 列出不屬於產品 API 契約的路由：伺服器內建的權限檢測示範端點，
// 以及測試自行掛載、用來觀察中介軟體行為的端點。
func isTestOnlyRoute(r *http.Request) bool {
	p := r.URL.Path
	return p == "/api/v1/auth/check" || p == "/api/v1/auth/admin-check" ||
		p == "/api/v1/custom-ping" || strings.HasPrefix(p, "/api/v1/test-")
}

// newFakeMailer 建立郵件替身，寄出的每一封信（含模擬寄送失敗的）都會在寄出當下經過 Secret 掃描。
// 整合測試不直接呼叫 interfaces.NewFakeMailer()：測試常以 PopAll／Reset 清空信件，事後再掃會漏掉。
func newFakeMailer(t testing.TB) *interfaces.FakeMailer {
	t.Helper()
	fm := interfaces.NewFakeMailer()
	fm.OnSend = func(msg interfaces.MailMessage) { scanSentMail(t, msg) }
	return fm
}

// scanSentMail 對一封信做 Secret 掃描。測試自行實作郵件替身時，要在 Send 的一開始呼叫它。
func scanSentMail(t testing.TB, msg interfaces.MailMessage) {
	t.Helper()
	for _, f := range secretscan.Shared().ScanMail(msg.To, msg.Cc, msg.Subject, msg.TextBody, msg.HTMLBody) {
		t.Errorf("Secret 洩漏：%s", f)
	}
}

// connectTestDB 連線到測試資料庫，並在測試結束時掃描所有資料表的文字欄位：
// Secret 應加密或雜湊後儲存，明文出現在任何欄位（含稽核紀錄與通知紀錄）都算洩漏。
// 整合測試不直接呼叫 database.Connect。
func connectTestDB(t testing.TB, ctx context.Context, dsn string) (*sql.DB, error) {
	t.Helper()
	db, err := database.Connect(ctx, dsn)
	if err != nil {
		return nil, err
	}
	// 測試通常會自行關閉 db（defer 或更晚註冊的 Cleanup，都比這裡先執行），所以掃描時另開一條連線。
	// 資料庫容器的終止是更早註冊的 Cleanup，會在掃描之後才執行。
	t.Cleanup(func() {
		scanDB, err := sql.Open("mysql", dsn)
		if err != nil {
			t.Errorf("Secret 掃描無法連線資料庫: %v", err)
			return
		}
		defer func() { _ = scanDB.Close() }()
		findings, err := secretscan.Shared().ScanDatabase(context.Background(), scanDB)
		if err != nil {
			t.Errorf("Secret 掃描資料庫失敗: %v", err)
		}
		for _, f := range findings {
			t.Errorf("Secret 洩漏：%s", f)
		}
	})
	return db, nil
}

// useSecret 登錄一個不經由 API 送出的 Secret 並原樣回傳，讓它也在掃描範圍內。
// 經由 API 請求本文送出的 Secret 欄位會自動登錄，不需要呼叫這個函式；
// 這裡處理的是測試直接寫進系統的值，例如啟動時建立的初始管理員密碼、主機端重設的密碼、驗證來源替身中的帳密。
func useSecret(t testing.TB, label, value string) string {
	t.Helper()
	if err := secretscan.Shared().Register(label, value); err != nil {
		t.Fatal(err)
	}
	return value
}
