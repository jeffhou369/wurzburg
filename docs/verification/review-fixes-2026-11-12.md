# 2026-11-12 最終審查修復驗證

## 範圍

修復基準：`c4b9f8e`（工作項目 34）；修復提交：`0c32510`（第一輪），第二輪修正即本文件所在的提交。本報告區分程式修復、測試涵蓋與尚未驗收的產品條件，不將既有元件測試或採集器替身的結果視為正式環境保證。

| 審查問題 | 修復與可觀察回歸 |
| --- | --- |
| 介面文字散落 | 功能 `.i18n.ts` 與共用文字檔，保留既有文字與技術格式提示 |
| 不存在的樣式 token／散落色碼 | 使用既有 success/danger、motion、shadow token；ChromeHeadless 檢查兩主題與元件引用 |
| 公開固定預覽金鑰 | 每實例不可預測金鑰；預覽綁定操作與逐筆資料指紋，偽造／跨實例／同筆數內容變更皆拒絕 |
| 刪除與範圍異動非原子 | 同筆交易包含範圍變更、IP／通知狀態刪除與詳細／摘要稽核；MariaDB trigger 模擬失敗後由 API 驗證回滾 |
| 改派失敗誤報成功 | 逐筆交易與成功稽核一同提交；一筆失敗不阻擋其他筆，寫入或稽核失敗皆不得回傳成功 |
| 自動清除未排程 | 正式啟動獨立維護背景迴圈；每日執行、失敗不標記完成，下次重試，仍保護有擁有者紀錄 |
| 郵件網址／登入後篩選失效 | 園區公開根網址、未設定停寄；需登入首頁路由、安全 returnUrl、帳號限定的已到期／即將到期待確認清單 |
| 多來源 MAC 順序影響採信 | 全輪更新來源與寬限後才做集合決策；並存 MAC 不改採信，改變來源順序不改結果 |
| 未處理衝突在零觀測時解消 | 零觀測維持未處理；集合恢復成採信 MAC 才自行解消；處理中仍保留原裁決語意 |
| 規模驗收不足 | 移除單方法微基準的完整輪詢宣稱，改為 50 台／200 VRF／25,000 IP 的 Scheduler → Scope → Reconciliation → HTTP 查詢測試 |

## 已執行驗證

### 後端

在 `src/backend`：

```sh
gofmt -l .
go vet ./...
go build -o /dev/null ./cmd/wurzburg
go test -short ./...
go test -race -count=1 -timeout=15m ./...
```

- 最終競態測試全部通過；`test` 整合測試套件耗時 **250.629 秒**。這是整個套件耗時，不是單輪輪詢耗時。
- 回歸檔案：`scope_atomic_regression_test.go`、`reconciliation_round_regression_test.go`、`batch_reassign_regression_test.go`、`maintenance_regression_test.go`、`review_regressions_test.go`、`mail_url_required_test.go`、`pending_confirmation_test.go`、`cidr_preview_route_test.go`。
- 規模與隔離：`scale_poll_cycle_test.go`；配置由真實登入後的 HTTP API 建立，不猜資料庫 ID、不以測試身分標頭繞過登入；逐頁核對實際 IP、來源、網段及儀表板。
- 故障注入只存在獨立 MariaDB 測試容器；沒有新增產品用的故障注入 HTTP 端點。

### 完整輪詢規模的最後量測

對提交 `0c32510` 另執行：

```sh
go test -v -count=1 -timeout=5m -run '^TestScalePollCycle_FullCycle_50Switches200VRFs25kIPs$' ./test
```

環境：macOS、14 個邏輯 CPU／GOMAXPROCS=14，單機 Docker MariaDB 11.4，採集器為程序內替身；本次測試整體耗時 72.48 秒。

| 項目 | 單次測值 | 測試門檻 |
| --- | --- | --- |
| 50 台／200 VRF 探索輪 | 590 ms | — |
| 25,000 筆首次落地的完整輪詢 | **10.171 s** | 180 s |
| 25,000 筆已存在的穩態輪詢 | **4.657 s** | 180 s |
| 儀表板統計 | 11 ms | 5 s |
| 首頁 50 筆查詢 | **169 ms** | 5 s |
| 讀完 125 頁、每頁 200 筆 | 21.999 s；最慢單頁 187 ms | — |

計時之外也核對 25,000 筆實際 IP、閘道／範圍外雜訊排除、所有 VRF／交換器來源、網段筆數與儀表板一致。**以上不含網路往返與實體 SNMP walk 時間，不代表實機輪詢承諾。**

### 前端

在 `src/frontend`：

```sh
npm run generate:api
npx prettier --write src/app/core/api.types.ts
npm run lint
npm run format:check
npm run build
npm test
```

最後整合執行全部通過，ChromeHeadless 為 **151／151**。這些是 Angular TestBed／HttpTestingController／樣式檢查，不宣稱為完整瀏覽器 E2E。產生 TypeScript 型別也不等於全部實際回應已通過 OpenAPI 驗證。

### 真實瀏覽器

使用獨立 MariaDB、正式模式 Go 後端與內嵌 Angular 正式建置；沒有攔截網路或偽造 API 回應。

`test/browser/review-smoke.js` 已透過 `playwright-cli` 執行通過：

1. 未登入的郵件連結導向登入頁，保留完整篩選。
2. 初始系統管理員登入後要求改密碼，目的地仍保留。
3. 五步精靈中的園區資訊儲存與其他步驟跳過、完成流程。
4. 返回自己的待確認清單，實際 API 請求含 `owner_me=true` 與 `pending_confirmation=true`。
5. 重新整理後從伺服端工作階段還原身分與篩選。
6. 拒絕站外 `returnUrl`。

另外以真實設定頁儲存公開網址，等待 GET 設定回應及表單非同步載入後，確認重新整理仍保留值。已截圖並檢視工作臺畫面；快照與工作階段資料不提交。

瀏覽器腳本需全新、隔離的測試資料庫及檔頭列明的合成帳密，**不可對既有部署執行**。精靈採跳過路徑，未驗證 SMTP／交換器測試連線，也未驗證含登記資料的破壞性確認流程。

## 第二輪獨立複查（`c4b9f8e...0c32510`）

第一次重啟時兩個只讀代理因 API 429 中止；之後重新執行並取得兩軸報告。以下發現已在後續提交修正，並各有先失敗後通過的回歸測試。

### Standards（3 項明確違規，已修正）

| 發現 | 修正 |
| --- | --- |
| 事件文字使用 `CONTEXT.md` 列為避用的「上線」 | 改為「位址重新在線」 |
| 自創色票 `--color-danger-strong:#7f1d1d`、遮罩 `rgba(15,23,42,.35)`，樣式包無此值 | 改用樣式包 `components.css` 的 `--button-danger-hover-bg` 與兩主題 `--scrim` |
| 登入導向以內嵌型別讀取精靈狀態 | 改用 OpenAPI 產生的 `WizardStatus` |

另補上自動清除失敗日誌的錯誤原因。報告中的判斷性 smell（去重鍵格式分散、`scope_handler.go` 過長、少量重複輔助函式、網段狀態字面值）**未處理**，留作後續重構。

### Spec（4 項，已修正）

| 發現 | 修正與回歸測試 |
| --- | --- |
| 修正 CIDR 時，仍被其他納管網段涵蓋的 IP 被刪除（含已登記資料） | 改以套用新 CIDR 後的範圍判定；仍屬其他納管網段者改歸屬，不列入刪除也不需確認。`TestReviewCIDRChangeKeepsRecordsCoveredByAnotherManagedSubnet` |
| 處理中衝突：來源改回報採信 MAC 時觀測被覆寫，第 1 輪即解消 | 新增遷移 `20261112000005` 的 `other_mac_absent_rounds`；其他 MAC 連續未被回報達離線判定輪數才解消，再次出現即歸零，管理者裁決時重設。`InProgressConflict_OverwrittenSourceStillWaitsForGrace` |
| 批次重新確認／釋放忽略寫入錯誤 | 與改派共用逐筆交易：鎖定後重驗權限，異動、去重鍵與成功稽核同交易。`TestReviewBatchConfirmAndReleaseReportWriteFailure` |
| 真實範圍步驟吞掉資料庫錯誤，`RunCycle` 無法回報 | 網段發現、閘道寫入、來源消失標記的錯誤一律回傳；無法解析的位址資料仍略過。`TestReviewRunCycleReportsRealScopeStepFailure` |

修正後重新執行：後端 `gofmt`、`go vet`、建置與 `go test -race -count=1 -timeout=20m ./...` 全部通過（`test` 整合測試套件 361.2 秒）；前端 `lint`、`format:check` 與 ChromeHeadless 151／151 通過。

修正後**未再做第三輪獨立複查**；上述結論來自回歸測試與完整測試結果。

## 尚未完成的驗收

第 33 張維持 `ready-for-agent`，以下條件沒有因本次回歸通過而自動完成：

- 所有 13 情境只由 API／時鐘產生產品狀態，且以 SNMP、驗證來源、SMTP 模擬服務走完。
- 實際 HTTP 回應逐端點自動符合 OpenAPI。
- 完整收集所有信件、API 回應、應用程式日誌及稽核／通知資料的 Secret 掃描。
- 登記、批次重新確認、衝突裁決及含登記紀錄的不納管確認等剩餘瀏覽器流程。
- 第 35 張郵件軟體／人類設計審閱、第 36 張 Cisco 實機驗證。

實體 SNMP 速度、網路延遲與設備負載未在本次規模測試中測量。不得將上述結果寫成可直接正式部署或完整產品驗收完成。
