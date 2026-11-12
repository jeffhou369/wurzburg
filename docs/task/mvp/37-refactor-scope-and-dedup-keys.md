# 37 — 整理管理範圍處理器與通知去重鍵

Status: resolved
Blocked by: —

## 要做什麼

第二輪程式審查（`c4b9f8e...0c32510`）提出幾項判斷性的結構建議，當時沒有處理（見 `docs/verification/review-fixes-2026-11-12.md`）。本工作項目只整理程式結構，**行為不變**：API 回應、資料庫寫入、稽核明細、信件內容與去重鍵字串都要與整理前完全相同。

1. **集中通知去重鍵格式**
   - 目前同一組格式分散在三處：
     - `src/backend/internal/notifications/service.go`：`unreg|<ip>|<mac>`、`weekly|<year>-W<week>`、`expiry|<ip>|<到期日>|<輪次>`
     - `src/backend/internal/subnets/scope_handler.go`：刪除 IP 紀錄時以 `unreg|<ip>|%`、`expiry|<ip>|%` 的 LIKE 前綴清除通知狀態
     - `src/backend/internal/purge/service.go`：自動清除時清除對應的通知狀態
   - 在單一位置提供建立去重鍵與「某 IP 的所有去重鍵前綴」的函式（含 LIKE 跳脫），三處改用它。改格式時只需要改一個地方。
2. **拆分 `src/backend/internal/subnets/scope_handler.go`**（約 1,450 行）
   - 依職責拆成數個檔案，例如：預覽與預覽 token（`signPreviewToken`、預覽指紋）、範圍異動（不納管、排除範圍、修正 CIDR，含 `recordMove`、`splitCIDROrphans`、`deleteIPRecordsWithAudit`）、網段發現與介面位址套用。
   - 只搬移與抽出私有函式，不改匯出介面與路由註冊。
3. **共用 SQL 輔助函式**
   - `placeholders` 在 `reconciliation/service.go` 與 `subnets/scope_handler.go` 各有一份；`chunkStrings`／`chunkInt64s` 只差型別。
   - 移到共用的內部套件（例如 `internal/sqlutil`），以泛型合併 chunk 函式。
4. **網段狀態字面值**
   - `pending`／`managed`／`unmanaged` 在非測試程式中以字串字面值出現約 13 處；改用具名常數。
5. **前端登入後導向**
   - 強制改密碼的導向邏輯同時存在於 `src/frontend/src/app/core/auth.guard.ts`（`createUrlTree`）與 `auth-redirect.service.ts`（`withReturnUrl`），`returnUrl` 有兩種組法。
   - 收斂為一個共用函式，由守衛與導向服務共同使用；`safe-return-url.ts` 的站內網址檢查維持不變。
6. **工作臺篩選字串**
   - 工作臺的篩選參數名稱與值（例如 `presence=online`、`registration=unregistered`、`owner_me`、`pending_confirmation`）散落在元件與測試中；集中為常數或由產生型別推導。

## 驗收條件

- [x] 去重鍵的建立與前綴比對只在一個地方定義；通知、管理範圍、自動清除三處都改用它
- [x] `scope_handler.go` 已依職責拆分，單一檔案不超過約 500 行
- [x] `placeholders` 與 chunk 函式只剩一份
- [x] 網段狀態在非測試程式中不再以字串字面值出現（SQL 字串除外）
- [x] 前端強制改密碼與 `returnUrl` 的組法只剩一處
- [x] 工作臺篩選參數集中定義
- [x] **既有測試不需修改即全部通過**：後端 `gofmt -l .`、`go vet ./...`、`go test -race -count=1 ./...`；前端 `npm run lint`、`npm run format:check`、`npm run build`、`npm test`
- [x] 若必須調整測試，只能是匯入路徑或識別碼改名，並在 `## Comments` 說明原因

## 參考

- `docs/verification/review-fixes-2026-11-12.md`「第二輪獨立複查」一節
- `AGENTS.md` 目錄慣例（後端功能模組各自一個套件；共用檔案的修改界線）
- `CONTEXT.md` 名詞

## Comments

### 2026-11-12 實作紀錄

- **去重鍵**：新增 `internal/dedupkey`（`Unregistered`、`Weekly`、`Expiry`、`UnregisteredSQL`、`UnregisteredPattern`、`ExpiryPattern`），以固定字串測試鎖定格式。實作時發現使用處不只三個模組，`conflicts/handler.go`（裁決後清除未登記狀態）與 `iprecords/handler.go`（登記、重新確認、釋放）也自行組字串，一併改用。原本 `purge`、`conflicts`、`iprecords` 以 `LIKE CONCAT('unreg|', ?, '|%')` 比對，現在統一傳入已跳脫的樣式；IP 位址不含 `%`、`_`，比對結果不變。
- **拆分**：`scope_handler.go` 拆成 `scope_handler.go`（服務、常數、錯誤、交易）、`scope_discovery.go`、`scope_preview.go`、`scope_delete.go`、`scope_views.go`、`scope_cidr.go`、`scope_state.go`、`scope_exclusions.go`。最大的檔案是 `scope_views.go`，327 行。只搬移程式，函式內容未改。
- **SQL 輔助**：新增 `internal/sqlutil`（`Placeholders`、泛型 `Args`、`Chunk`），取代整併模組的 `placeholders`、`stringArgs`、`int64Args`、`chunkStrings`、`chunkInt64s` 與管理範圍模組的 `placeholders`。
- **網段狀態**：`subnets.StatePending`、`StateManaged`、`StateUnmanaged`。
- **登入後導向**：`safe-return-url.ts` 新增 `returnUrlTree`，路由守衛與 `AuthRedirectService` 共用；統一以 Angular 的網址序列化編碼。首頁目的地是否附加 returnUrl 仍維持原本差異（守衛附加、登入後導向省略），以 `omitHome` 明確表達。
- **工作臺篩選**：新增 `workbench.filters.ts`，網址還原、網址寫入、API 參數、批次條件四處改用同一份參數清單，送出順序不變；另加 `workbench.filters.spec.ts`。

**驗證**：既有測試一行未改。後端 `gofmt`、`go vet`、`go test -race -count=1 ./...`：只有 `TestClusterA_AuditOUIAndAutoPurge` 失敗，原因是主線 `23ce83b` 帶入的既有問題（在未經本重構的 `main` 上同樣失敗），已在 `fix/copilot-review-regressions` 修正；其餘全部通過。前端 `lint`、`format:check`、`build` 通過，ChromeHeadless 155／155（既有 151 加新增 4）。

