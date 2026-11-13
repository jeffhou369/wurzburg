# Würzburg API 契約與共通慣例

本目錄以 OpenAPI 3.0.3 定義前後端之間唯一的介面契約。為支援多個工作項目平行開發，每個資源拆分為 `docs/api/resources/*.yaml` 獨立檔案，並於 `docs/api/openapi.yaml` 彙整。

## 1. 共通慣例

- **分頁、排序與篩選**：清單端點統一由伺服端負責分頁（`page`、`page_size`），回傳 `items` 與 `pagination`（`total_items`、`total_pages`）。IP 管理表與 Excel 匯出共用完全相同的篩選查詢參數。
- **兩階段刪除保護**：凡會導致 IP 紀錄直接刪除的網段狀態變更、CIDR 修改或排除範圍建立，必須先呼叫 `/preview` 取得受影響筆數與 `preview_token`，再由前端提示確認後附帶 `preview_token` 執行變更。若確認期間資料筆數變動，後端回傳 `409 Conflict` 要求重新預覽。
- **統一批次登記介面**：`register`、`confirm`、`release`、`reassign` 均接受 `ip_record_ids` 陣列或 `select_all_filtered: true` + `filter`，並回傳逐筆執行結果（`BatchActionResult`）；單筆操作即為陣列長度為 1 的特例。
- **狀態表現**：`IPRecordSummary` 同時提供 `presence`、`registration`、`expiring_soon`、`has_conflict` 四個獨立欄位，前端依此組合文字標籤（如「在線 · 未登記」）。
- **錯誤回應格式與狀態碼**：所有錯誤回應都是 `ErrorResponse`（`code`、`message`，422 時可帶 `fields`）。每個操作的 `responses` 都有 `default`，指向 `resources/common.yaml` 的共用 `Error` 回應；只有對呼叫端有特殊意義的錯誤才個別列出（例如兩階段確認的 409、登入鎖定的 423）。成功的狀態碼一律要明確列出，不可只靠 `default`。常見的錯誤狀態碼：
  - `401 Unauthorized`：未登入或工作階段逾時
  - `403 Forbidden`：權限不足（含一般使用者試圖編輯他人登記）
  - `409 Conflict`：狀態不允許該動作（如重複登記已登記之 IP、停用最後一位管理員）
  - `422 Unprocessable Entity`：輸入格式驗證失敗（回傳 `fields` 欄位錯誤明細）
- **機敏資料防護**：任何回應絕不包含密碼或 Secret；設定類端點僅回傳布林旗標（如 `has_credential`、`has_password`）。

## 2. 契約檢查

描述檔與後端實作由後端整合測試自動比對，不一致時 `go test ./...` 會失敗：

- **回應內容**：整合測試一律經由 `src/backend/test/contract_harness_test.go` 的 `apiRouter` 取得路由器。每一次 API 回應都會依描述檔驗證狀態碼、`Content-Type` 與本文。檢查比 OpenAPI 的預設語意嚴格：回應有本文而描述檔沒有定義內容、或 JSON 中出現描述檔沒有列出的欄位，都視為不符合。
- **路由**：`TestRegisteredRoutesMatchSpec` 比對後端實際註冊的路由與描述檔，兩邊必須完全一致。
- **涵蓋率**：完整執行 `src/backend/test` 時，描述檔的每個操作都要至少有一個回應被驗證過，否則測試失敗。其他測試沒有打到的操作放在 `TestAPIContractCoverage`。

新增或修改端點時，同時更新描述檔、重新產生前端型別，並確認有測試打到該操作。檢查器在 `src/backend/internal/testutil/apicontract`。

## 3. 型別產生指令

```sh
cd src/frontend
npm run generate:api
```
