# 39 — 自動檢查實際 HTTP 回應符合 OpenAPI 描述檔

Status: ready-for-agent
Blocked by: —

## 要做什麼

由第 33 張拆出（待補驗證第 2 項）。目前只有「依 `docs/api/openapi.yaml` 產生前端 TypeScript 型別」與型別檢查，沒有任何機制確認後端實際回傳的內容符合描述檔。描述檔與實作脫節時，前端型別會是錯的，而且沒有測試會失敗。

1. **回應契約檢查器**
   - 在後端整合測試中，攔截測試對路由器送出的每一次 HTTP 請求與回應，依 `docs/api/openapi.yaml`（含 `docs/api/resources/*.yaml`）驗證：
     - 請求的路徑與方法在描述檔中有定義
     - 回應的狀態碼在該操作中有定義
     - 回應的 `Content-Type` 與本文符合該狀態碼的 schema
   - 檢查器掛在既有整合測試共用的入口，讓所有既有測試的 HTTP 交換都被驗證，不另外寫一套只為契約而存在的請求。
   - 只驗證回應。測試會刻意送出不合法的請求來驗證 422 等錯誤處理，請求本文不納入驗證。
2. **描述檔本身的檢查**
   - 載入時驗證描述檔是合法的 OpenAPI 3.0 文件，所有 `$ref` 都能解析。
3. **涵蓋率**
   - 整套測試跑完後，列出描述檔中有定義、但沒有任何測試打到的操作（方法加路徑）。未涵蓋的操作要補上最小的請求，或在 `## Comments` 記錄不涵蓋的原因。
4. **修正發現的不一致**
   - 描述檔與實作不一致時，以開發規格與前端實際使用方式判斷哪一邊是對的，修正錯的那一邊。修改描述檔後重新產生 `src/frontend/src/app/core/api.types.ts`，前端型別檢查與測試要通過。
   - 逐項記在 `## Comments`：端點、不一致的內容、修正的是描述檔還是實作。

## 不在範圍內

- 不驗證請求本文與查詢參數是否符合描述檔。
- 不改寫既有測試的情境內容；只在共用入口掛上檢查器。
- 第 07 到 32 張 `## Comments` 的逐張清點留在第 33 張。

## 驗收條件

- [ ] 既有後端整合測試的每一次 API 回應都經過 OpenAPI 驗證，不符合時測試失敗，並指出端點、狀態碼與不符合的欄位
- [ ] 描述檔載入時通過 OpenAPI 文件驗證
- [ ] 描述檔中的每個操作至少被一個測試打到，或在 `## Comments` 記錄例外原因
- [ ] 故意改壞一個回應欄位（例如型別改錯）時測試會失敗，確認檢查器有效；結果記在 `## Comments`
- [ ] 發現的不一致全部修正並逐項記錄；修改描述檔後前端 `npm run generate:api`、`npm run lint`、`npm test` 通過
- [ ] 後端 `gofmt`、`go vet`、golangci-lint、`go test -race -count=1 ./...` 通過，CI 通過

## Comments

- 2026-11-13：由第 33 張拆出。
- 2026-11-13：實作完成。

  **機制**
  - 檢查器在 `src/backend/internal/testutil/apicontract`，使用 kin-openapi v0.153.0。整合測試一律經由 `src/backend/test/contract_harness_test.go` 的 `apiRouter` 取得路由器，原本 21 處 `srv.Router()` 全部改用它，所以既有測試的每個 API 回應都會被驗證。
  - 檢查比 OpenAPI 預設語意嚴格，否則抓不到脫節：成功狀態碼必須明確列出；回應有本文而描述檔沒定義內容視為不符合；JSON 出現描述檔沒列出的欄位視為不符合（`allOf` 的成員合併計算，明確宣告 `additionalProperties` 的物件除外）。
  - 錯誤狀態碼的處理與原本的驗收文字略有不同：不是每個操作逐一列出所有錯誤狀態碼，而是每個操作都有 `default` 指向共用的 `Error` 回應，本文必須符合 `ErrorResponse`。原因是 61 個操作都可能回 401、403、500，逐一列出只會讓描述檔膨脹；對呼叫端有特殊意義的錯誤仍個別列出。
  - 另外加了兩項原本沒列在本張的檢查，因為只看「測試打到的回應」會漏掉沒人測的端點：
    - `TestRegisteredRoutesMatchSpec`：後端註冊的路由與描述檔必須完全一致。
    - `TestMain`（`src/backend/test/main_test.go`）：完整執行時，描述檔的每個操作都要有回應被驗證過。`-short`、`-run`、`-skip` 時不檢查。

  **發現並修正的不一致**

  | 端點 | 不一致的內容 | 修正 |
  | --- | --- | --- |
  | 全部 61 個操作 | 描述檔只列了 10 個錯誤回應，而且都沒有定義內容；`ErrorResponse` 有定義卻沒有任何地方引用 | 描述檔：新增共用 `Error` 回應，每個操作加 `default`；既有的 10 個錯誤回應補上內容 |
  | `PUT /subnets/{id}`、`PUT /subnets/{id}/state`、`POST /subnets/{id}/exclusions` | 兩階段確認的 409 沒有列出 | 描述檔：明確列出 409 |
  | 16 個成功回應（管理員建立與啟停、改密碼、觸發同步、交換器與 VRF 讀取設定的新增修改、五個設定的 PUT、網段新增與修改、網段狀態、新增排除範圍） | 實作回傳 JSON，描述檔沒有定義內容 | 描述檔：補上內容定義，新增 `MessageResponse`、`VRFReadConfigList` |
  | `GET /ip-records`、`GET /ip-records/{id}` | 回應的 `state_label` 不在描述檔；前端以 `& { state_label?: string }` 自行補型別 | 描述檔：`IPRecordSummary` 加上必填的 `state_label`；前端移除補丁 |
  | `GET /switches`、`POST /switches`、`PUT /switches/{id}` | 回應的 `has_v3_creds` 不在描述檔 | 描述檔：`Switch` 加上 `has_v3_creds` |
  | `POST /admins`、`POST /admins/{id}/toggle`、`POST /auth/change-password`、`POST /sync/trigger` | 回傳 JSON 卻沒有 `Content-Type` 標頭 | 實作：補上 `application/json; charset=utf-8` |
  | `GET /sync/status` | 後端有註冊、前端交換器頁面在用，描述檔沒有 | 描述檔：新增操作與 `SyncStatus`；前端改用產生的型別 |
  | `GET /settings/auth-source` | 描述檔定義它回傳完整設定，但實作拆成兩個端點：帳號模組的 `GET /settings/auth-source` 只回兩個欄位、前端沒有使用；前端實際呼叫的是驗證來源模組的 `GET /settings/auth-source/config`，不在描述檔 | 實作：依描述檔整併為 `GET /settings/auth-source` 回傳完整設定，移除 `/config` 與帳號模組的重複處理器；前端改呼叫這個路徑並改用產生的型別 |
  | `GET`／`PUT /settings/auth-source` | 回應的 `ad_user_attr`、`ad_mail_attr`、`ad_name_attr`、`radius_timeout_ms` 不在描述檔；尚未設定驗證來源時 `active_source` 回空字串，不在列舉值內 | 描述檔：`AuthSourceSettings` 與 `AuthSourceUpdateRequest` 補上欄位；實作：未設定時回 `none` |

  **涵蓋率**
  - 原本有 14 個操作沒有任何整合測試打到。新增 `TestAPIContractCoverage`（`src/backend/test/contract_coverage_test.go`）以帶有實際資料的回應補齊，目前 62 個操作全部涵蓋，沒有例外。

  **檢查器有效性**
  - 檢查器本身有單元測試（`apicontract_test.go`）：欄位型別錯誤、多餘欄位、未定義的路徑與方法、未列出的成功狀態碼、有本文但沒定義內容，都會回報。
  - 實測：把 `switches` 回應的 `snmp_port` 改名後執行 `TestAPIContractCoverage`，測試失敗並指出 `GET /api/v1/switches/1 → 200`、`property "snmp_port" is missing`；已還原。

  **驗證**
  - 後端：`gofmt`、`go vet`、golangci-lint v2.15.0（0 issues）、`go test -race -count=1 ./...` 全部通過。
  - 前端：`npm run generate:api` 後型別一致，`npm run lint`、`npm run format:check`、`npm run build` 通過，單元測試 155／155。

  **留給後續的事項**
  - `TestRegisteredRoutesMatchSpec` 的模組清單是照 `cmd/wurzburg/modules.go` 手寫的，新增功能模組時兩邊都要加。第 41 張需要以正式的組裝方式啟動整套系統，屆時把 `mountFeatureModules` 移到可匯入的套件，兩邊共用。

