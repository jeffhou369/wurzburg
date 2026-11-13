# 38 — 讓 CI 執行後端與前端測試

Status: ready-for-agent
Blocked by: —

## 要做什麼

`.github/workflows/code-quality.yml` 目前只做靜態檢查：

- 後端：`gofmt`、`go vet`、golangci-lint
- 前端：`npm run lint`、`npm run format:check`

後端與前端的**測試都沒有在 CI 執行**，前端也沒有建置。PR #2 合併時，`TestClusterA_AuditOUIAndAutoPurge` 在 `main` 上失敗，CI 卻全部通過；這個失敗到後續人工審查才發現，之後由 PR #4 修正。

本工作項目要讓 PR 與推送到 `main` 時自動執行完整測試，失敗時擋下合併。

1. **後端測試**
   - 在 `ubuntu-latest` 執行 `go test -race -count=1 ./...`，含 `src/backend/test/` 以 Testcontainers 啟動 MariaDB 的整合測試。GitHub 代管的 Ubuntu runner 內建 Docker，不需另裝。
   - 本機完整執行約 5 分鐘（`test` 套件約 286 秒），CI 加上 race detector 與映像下載可能更久。另開獨立 job（例如 `Backend Tests (Go)`），`timeout-minutes` 依實測值留餘裕，不要拉長既有 lint job 的逾時。
   - 失敗時要能從日誌直接看出是哪個測試，例如用 `-v` 或 `gotestsum` 輸出摘要。若採用第三方 action 或工具，依本 repo 規則鎖定 40 字元 SHA 或確切版本。
2. **前端測試與建置**
   - 執行 `npm run build` 與 `npx ng test --watch=false --browsers=ChromeHeadless`。runner 內建 Chrome；若 ChromeHeadless 在容器沙箱下無法啟動，在 Karma 設定加 CI 專用的 launcher（`--no-sandbox`），只在 CI 使用。
   - 確認 `npm run generate:api` 產生的型別與 `src/frontend/src/app/core/api.types.ts` 一致，避免 OpenAPI 與前端型別脫節。
3. **合併閘門**
   - `Code Quality gate` 一併檢查新增的測試 job：測試失敗或被取消時，閘門必須失敗。
   - 子系統偵測（`detect` job）沿用既有輸出，新 job 依 `has_backend`／`has_frontend` 決定是否執行。
4. **文件**
   - 更新 `AGENTS.md`「常用開發指令」或 `docs/install.md` 相關段落，說明 CI 會跑哪些檢查。

## 不在範圍內

- 不新增或修改任何測試內容；本工作項目只處理 CI 設定。若接上後發現既有測試在 CI 上不穩定，另開工作項目處理，不在這裡調整斷言或加 `t.Skip`。
- 不在 CI 執行規模驗證（50 台／200 VRF／25,000 IP）以外的效能量測，也不接實體交換器。
- 不處理瀏覽器端到端流程（見第 33 張）。

## 驗收條件

- [ ] PR 與推送到 `main` 時，CI 執行後端 `go test -race -count=1 ./...`，含需要 Docker 的整合測試，且全部通過
- [ ] CI 執行前端 `npm run build` 與 ChromeHeadless 單元測試，且全部通過
- [ ] CI 檢查 OpenAPI 產生的前端型別與 repo 內的檔案一致
- [ ] `Code Quality gate` 會因任一測試 job 失敗而失敗：在分支上故意讓一個後端測試、一個前端測試各失敗一次，確認閘門變紅後再還原，並在 `## Comments` 附上那次執行的連結
- [ ] 新增的 action 都鎖定 40 字元 SHA；`actionlint` 通過
- [ ] 文件已說明 CI 執行的檢查項目

## Comments

- 2026-11-13：PR #2 至 #5 期間發現本缺口。`TestClusterA_AuditOUIAndAutoPurge` 在 `main` 上失敗，但 CI 全綠，因為 CI 沒有執行任何測試。
