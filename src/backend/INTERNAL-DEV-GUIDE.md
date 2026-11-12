# Würzburg 後端共用機制與平行開發指引

本文件說明第 06 張工作項目建立的後端共用設施，供第 07 至 32 張工作項目直接使用，**開發功能時請勿修改共用檔案**。

## 1. 模組路由註冊方式

每個功能在 `src/backend/internal/<feature>/` 實作 `server.RouteRegistrar` 介面：

```go
type Handler struct { ... }

func (h *Handler) RegisterRoutes(api chi.Router) {
    api.With(auth.RequireAuth).Get("/my-resource", h.handleList)
    api.With(auth.RequireAdmin).Post("/my-resource", h.handleCreate)
}
```

## 2. 授權與取得目前登入者

- **強制登入**：路由掛載 `auth.RequireAuth`（未登入自動回 401）。
- **強制管理員**：路由掛載 `auth.RequireAdmin`（非系統管理員自動回 403）。
- **讀取登入者**：`user := auth.UserFromContext(r.Context())`。
- **判斷擁有者編輯權**：`auth.CanEditRegistration(user, recordOwnerID)`。
- **CSRF 防護**：所有對 `/api/v1/*` 發出的 `POST`/`PUT`/`PATCH`/`DELETE` 請求均已強制要求 `X-Requested-With` 或 `X-CSRF-Token` 標頭。

## 3. 讀取系統設定與存取機敏資料（Secrets）

透過 `settings.Store`：
- 一般設定：`val, err := store.Get(ctx, "campus_name")` 或 `days := store.GetInt(ctx, "auto_purge_days", 90)`。
- 機敏資料（如 SNMP community、SMTP 密碼）：
  - 寫入：`err := store.SetSecret(ctx, "smtp_password", rawPassword)`（自動以 AES-256-GCM 加密儲存）。
  - 讀取：`raw, err := store.GetSecret(ctx, "smtp_password")`（僅供連線使用，嚴禁回傳 API）。
  - 檢查是否已設定：`exists, err := store.HasSecret(ctx, "smtp_password")`。

## 4. 寫入稽核紀錄

透過 `audit.Logger`：

```go
auditLogger.Log(ctx, user.Username, user.Role, r.RemoteAddr,
    "registration.create", "ip_record", "10.10.10.50",
    "使用者 chen 完成 IP 登記",
    map[string]any{"owner_mail": "chen@example.com"},
)
```

任何包含 `password`、`secret`、`community`、`token`、`passphrase` 的欄位名稱會自動替換為 `[REDACTED]`。

## 5. 推算 IP 紀錄狀態與位址歸屬

- **狀態推算**：一律呼叫 `state.Compute(...)`，禁止在功能模組重複撰寫「即將到期」或組合標籤邏輯。
- **位址歸屬查詢**：呼叫 `subnets.NewResolver(db).Resolve(ctx, ipAddress)` 判斷是否屬於納管網段、是否在排除範圍或為閘道位址。

## 6. 使用測試替身與測試環境輔助

- **快速以特定身分呼叫 API**：
  ```go
  req := testutil.NewAuthRequest(http.MethodPost, "/api/v1/...", body, userID, "user", "chen")
  ```
- **模組測試替身**：位於 `internal/interfaces/fakes.go`，提供 `FakeCollector`、`FakeAuthSource`、`FakeOUILookup` 與 `FakeMailer`。
- **注入一輪採集結果**：建立 `pipeline.New(scopeProc, reconcilerProc)` 並呼叫 `Process(ctx, outputs, failedVRFConfigIDs)`。
