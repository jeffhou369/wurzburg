# 38 — 讓 CI 執行後端與前端測試

Status: resolved
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

- [x] PR 與推送到 `main` 時，CI 執行後端 `go test -race -count=1 ./...`，含需要 Docker 的整合測試，且全部通過
- [x] CI 執行前端 `npm run build` 與 ChromeHeadless 單元測試，且全部通過
- [x] CI 檢查 OpenAPI 產生的前端型別與 repo 內的檔案一致
- [x] `Code Quality gate` 會因任一測試 job 失敗而失敗：在分支上故意讓一個後端測試、一個前端測試各失敗一次，確認閘門變紅後再還原，並在 `## Comments` 附上那次執行的連結
- [x] 新增的 action 都鎖定 40 字元 SHA；`actionlint` 通過
- [x] 文件已說明 CI 執行的檢查項目

## Comments

- 2026-11-13：PR #2 至 #5 期間發現本缺口。`TestClusterA_AuditOUIAndAutoPurge` 在 `main` 上失敗，但 CI 全綠，因為 CI 沒有執行任何測試。
- 2026-11-13：接上 CI 後，`TestScopeLockSnapshotAndLogTx` 與 `TestScopeAtomicRollbackOnFailure` 在 CI 失敗（「等不到 N 個鎖等待中的交易」）。依「不在範圍內」原應另開工作項目，但不修就無法合併本工作項目，且原因已查明、只需改測試的觀察方式，因此在本工作項目一併處理：
  - 根因：本機先前的 `mariadb:11.4` 是在 Apple Silicon 上模擬執行的 amd64 映像；重新拉取後換成原生 arm64，本機也能重現。兩者同為 11.4.13。在原生 amd64／arm64 上，請求確實被列鎖擋住（`SHOW ENGINE INNODB STATUS` 顯示 `LOCK WAIT … insert intention waiting`），但 `information_schema.INNODB_TRX` 仍回報 `RUNNING`，`INNODB_LOCK_WAITS` 為 0 筆。產品的鎖定行為正確，是測試偵測鎖等待的方式不可靠。
  - 修正：`saWaitLockWaits` 改為同時計算 `SHOW ENGINE INNODB STATUS` 中的「TRX HAS BEEN WAITING」段落，取兩者較大值；逾時時把最後一次 InnoDB 狀態寫進測試日誌。斷言內容不變。
  - 驗證：原生 arm64 與模擬 amd64 兩種映像都通過。另外暫時拿掉 `lockScopeRowsSQL` 的 `FOR UPDATE`，測試會如預期失敗，確認仍能抓到鎖定回歸。完整 `go test -race -count=1 ./...` 通過，`test` 套件在原生映像上約 176 秒。
- 2026-11-13：在 PR #6 實作完成。
  - PR #6 的 CI 全部通過。`Backend Tests (Go)` 約 6 分 15 秒，`Frontend Tests (Angular)` 約 1 分 11 秒。
  - 閘門驗證：在暫時分支 `ci/verify-gate-failure` 加入一個故意失敗的後端測試與一個前端測試，以 workflow_dispatch 執行（https://github.com/jeffhou369/wurzburg/actions/runs/43360034335）。兩個測試 job 都失敗，`Code Quality gate` 隨之失敗；失敗清單只列出那兩個故意失敗的測試。驗證後已刪除該分支。
  - ChromeHeadless 在代管 runner（Ubuntu 26.04 映像）上不需要 `--no-sandbox`，未修改 Karma 設定。
