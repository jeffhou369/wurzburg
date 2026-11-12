# 01 — 走通骨架

Status: resolved
Blocked by: 無，可以立即開始

## 要做什麼

讓一套空的 Würzburg 能被啟動、被測試，並定下之後所有工作項目共用的慣例。

一行指令啟動 Compose 後，瀏覽器開啟就看得到一個最簡單的頁面，頁面顯示後端的健康狀態。後端是 Go，前端是 Angular，前端建置後的靜態檔包進後端的執行檔，兩者在同一個 `app` 容器；`db` 是 MariaDB。

同時建立主要測試接縫的測試環境：把應用程式整個啟動起來、接上真的 MariaDB、時鐘由測試控制，並跑通第一個測試。

開始前查最新文件，決定採用的程式庫與版本。

## 驗收條件

- [x] 一行指令啟動 Compose，瀏覽器看得到頁面與後端的健康狀態
- [x] 健康檢查端點有回應，內容不含任何 secret
- [x] `app` 容器以非 root 執行；`db` 只在 Compose 的內部網路，不對外開放
- [x] 資料庫結構變更以有版本的 SQL 檔管理，應用程式啟動時自動套用；檔名以時間戳排序，讓並行的工作項目不會撞號
- [x] 所有與時間有關的邏輯都經由時鐘模組取得時間，測試可以把時間往後撥
- [x] 結構化日誌寫到標準輸出
- [x] 測試環境可用一行指令執行，至少有一個測試從主要接縫（呼叫 API）通過
- [x] 容器、套件、資料庫與環境變數的名稱使用 ASCII 的 `wurzburg`
- [x] `AGENTS.md` 已補上實際的建置、lint、測試與執行單一測試的指令
- [x] `AGENTS.md` 已寫入目錄慣例與平行作業規則：每個功能擁有自己的後端模組、頁面、介面文字檔與 API 描述檔；共用的檔案只在地基的工作項目（03 到 06）修改

## Comments

已完成 01 — 走通骨架：
- Go 後端（`src/backend`）：Go 1.26、chi/v5、goose/v3 內嵌遷移、slog JSON 日誌、可控時鐘模組（`internal/clock`）、內嵌靜態前端服務與 `/healthz` / `/api/v1/health` 健康檢查端點。
- Angular 前端（`src/frontend`）：Angular 20 + `@angular/cdk` + Prettier，於首頁查詢並呈現後端健康狀態。
- 容器部署（`Dockerfile` + `infra/docker-compose.yml`）：多階段建置整合單一執行檔、非 root 使用者（UID 10001 `wurzburg`）、MariaDB 11.4 僅限內部網路、透過 `name: wurzburg` 解決目錄名稱非 ASCII 字元之 buildx gRPC 問題。
- 測試：Unit tests + Testcontainers MariaDB 整合測試 + Angular ChromeHeadless 單元測試均已驗證通過。

## 參考

- 開發規格第 9 節（技術基線）
- `docs/plan/mvp-spec.md` 的「測試決定」
- ADR 0010、0014
