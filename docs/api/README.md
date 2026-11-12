# Würzburg API 契約與共通慣例

本目錄以 OpenAPI 3.0.3 定義前後端之間唯一的介面契約。為支援多個工作項目平行開發，每個資源拆分為 `docs/api/resources/*.yaml` 獨立檔案，並於 `docs/api/openapi.yaml` 彙整。

## 1. 共通慣例

- **分頁、排序與篩選**：清單端點統一由伺服端負責分頁（`page`、`page_size`），回傳 `items` 與 `pagination`（`total_items`、`total_pages`）。IP 管理表與 Excel 匯出共用完全相同的篩選查詢參數。
- **兩階段刪除保護**：凡會導致 IP 紀錄直接刪除的網段狀態變更、CIDR 修改或排除範圍建立，必須先呼叫 `/preview` 取得受影響筆數與 `preview_token`，再由前端提示確認後附帶 `preview_token` 執行變更。若確認期間資料筆數變動，後端回傳 `409 Conflict` 要求重新預覽。
- **統一批次登記介面**：`register`、`confirm`、`release`、`reassign` 均接受 `ip_record_ids` 陣列或 `select_all_filtered: true` + `filter`，並回傳逐筆執行結果（`BatchActionResult`）；單筆操作即為陣列長度為 1 的特例。
- **狀態表現**：`IPRecordSummary` 同時提供 `presence`、`registration`、`expiring_soon`、`has_conflict` 四個獨立欄位，前端依此組合文字標籤（如「在線 · 未登記」）。
- **錯誤回應格式與狀態碼**：
  - `401 Unauthorized`：未登入或工作階段逾時
  - `403 Forbidden`：權限不足（含一般使用者試圖編輯他人登記）
  - `409 Conflict`：狀態不允許該動作（如重複登記已登記之 IP、停用最後一位管理員）
  - `422 Unprocessable Entity`：輸入格式驗證失敗（回傳 `fields` 欄位錯誤明細）
- **機敏資料防護**：任何回應絕不包含密碼或 Secret；設定類端點僅回傳布林旗標（如 `has_credential`、`has_password`）。

## 2. 型別產生指令

```sh
cd src/frontend
npm run generate:api
```
