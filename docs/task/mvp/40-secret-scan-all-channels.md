# 40 — 全管道 Secret 掃描

Status: ready-for-agent
Blocked by: 39

## 要做什麼

由第 33 張拆出（待補驗證第 3 項）。規則：Secret 儲存後不回顯，不出現在日誌、稽核紀錄、錯誤頁、URL 與郵件本文。

目前的 `Security_SecretScan`（`src/backend/test/e2e_pipeline_test.go`）只掃描單一測試結束時的稽核紀錄、通知紀錄，以及當下還留在替身裡的信件。信件集合會被 `PopAll`／`Reset` 清空，HTTP 回應與應用程式日誌完全沒有掃描，因此不能宣稱全管道掃描已完成。

1. **Secret 登錄**
   - 測試中使用的每個 Secret（本機帳號密碼、組織帳號密碼、SNMP community、SNMPv3 驗證與加密密碼、SMTP 密碼、驗證來源的 bind 密碼或 RADIUS 共用密鑰、`WURZBURG_SECRET_KEY`、預覽簽章金鑰）都登錄到掃描器。
   - Secret 要用足以辨識的字串。像 `public` 這類會出現在一般文字中的值不能當作掃描樣本，測試要改用可辨識的值。
2. **掃描的管道**（沿用第 39 張在共用入口攔截 HTTP 交換的機制）
   - 每一次 HTTP 回應的本文與標頭（`Set-Cookie` 的工作階段識別碼不屬於登錄的 Secret）
   - 請求的 URL（Secret 不可出現在路徑或查詢字串）
   - 每一封寄出的信：主旨、純文字本文、HTML 本文、收件者與標頭。寄出時就記錄，不受 `PopAll`／`Reset` 影響
   - 應用程式日誌：測試期間 `slog` 與標準 `log` 的全部輸出
   - 資料庫：稽核紀錄、通知紀錄與通知狀態的所有文字欄位。另外確認 Secret 沒有以明文存放在任何資料表（依規格應加密或雜湊）
3. **失敗訊息**
   - 發現洩漏時指出管道與位置（端點、信件主旨、資料表與欄位、日誌行），但訊息本身不印出 Secret 的值。

## 不在範圍內

- 不掃描 git 歷史或原始碼中的憑證；那是 CI 安全性工作流程的範圍。
- 不改產品的 Secret 儲存方式，除非掃描發現實際洩漏；發現時修正並記在 `## Comments`。

## 驗收條件

- [ ] 所有後端整合測試使用的 Secret 都已登錄，且都是可辨識的字串
- [ ] HTTP 回應、請求 URL、每一封寄出的信、應用程式日誌、稽核與通知資料表都在掃描範圍，任一處出現登錄的 Secret 時測試失敗
- [ ] Secret 沒有以明文出現在任何資料表
- [ ] 分別在一個管道故意洩漏一次（回應、信件、日誌、稽核紀錄），確認掃描器都能抓到；結果記在 `## Comments`
- [ ] 掃描發現的實際洩漏全部修正並逐項記錄
- [ ] 後端 `gofmt`、`go vet`、golangci-lint、`go test -race -count=1 ./...` 通過，CI 通過

## Comments

- 2026-11-13：由第 33 張拆出。
- 2026-11-13：實作完成。

  **機制**
  - 掃描器在 `src/backend/internal/testutil/secretscan`，整套測試共用一份。除了明文，也比對 URL、HTML、JSON 跳脫後的形式。回報只有管道、位置、Secret 的標籤與遮蔽後的片段，不印出值。
  - **Secret 登錄**
    - 經由 API 送出的 Secret 自動登錄：`apiRouter` 讀取每個請求本文，取出 Secret 欄位的值（`password`、`new_password`、`old_password`、`snmp_community`、`community`、`v3_auth_pass`、`v3_priv_pass`、`smtp_password`、`ad_bind_password`、`radius_secret`）。單元測試確認描述檔中所有 `format: password` 的欄位都在這份清單內。
    - 不經 API 寫入的 Secret 以 `useSecret` 登錄：啟動時建立的初始管理員密碼、主機端重設的密碼、驗證來源替身的帳密、直接呼叫 `SetSecret` 的值。
    - `TestMain` 另外登錄測試用的 `WURZBURG_SECRET_KEY`、測試中另外指定的預覽簽章金鑰，以及測試資料庫的密碼。
    - 樣本不足 8 個字元時測試失敗。
  - **掃描的管道**
    - HTTP 回應的本文與標頭、請求 URL：`apiRouter`，每次請求當下掃描。
    - 寄出的信（收件者、副本、主旨、純文字與 HTML 本文）：`newFakeMailer` 在寄出當下掃描，含模擬寄送失敗的信，不受 `PopAll`／`Reset` 影響。`notifications_mailer_test.go` 自行實作的郵件替身也接上了。
    - 應用程式日誌：`TestMain` 把 `slog`（以及經由它輸出的標準 `log`）導到掃描器，等級設為 Debug。
    - 資料庫：`connectTestDB` 在每個測試結束時掃描所有資料表的所有文字欄位，涵蓋稽核紀錄、通知紀錄、通知狀態，也確認 Secret 沒有以明文存放在任何資料表。
    - 日誌與信件會保留到整套測試結束，用完整的 Secret 清單再掃一次，因為 Secret 可能在那段文字產生之後才第一次被登錄。HTTP 回應只在請求當下掃描（規模測試的回應量太大，不保留）。
  - 整合測試原本 16 處 `database.Connect`、4 處 `interfaces.NewFakeMailer()` 都改用上述入口。

  **掃描結果**
  - 整套後端整合測試在五個管道都沒有發現實際洩漏，產品程式不需要修改。
  - 唯一需要調整的是一個樣本：`accounts_login_test.go` 驗證密碼長度政策時用的 `short`（5 個字元）改為 `Tiny#Pw9x`（9 個字元，仍低於政策下限 12）。
  - 產品程式唯一的變更是測試替身 `interfaces.FakeMailer` 新增 `OnSend` 觀察點。

  **掃描器有效性**（逐一修改產品程式、執行對應測試、確認失敗後還原）

  | 管道 | 故意洩漏的方式 | 掃描結果 |
  | --- | --- | --- |
  | HTTP 回應標頭 | 建立交換器時把 community 寫進 `X-Debug-Community` 標頭 | `HTTP 回應｜POST /api/v1/switches 標頭｜snmp_community（長度 20）` |
  | HTTP 回應本文 | 郵件設定回應多帶一個欄位，值為 SMTP 密碼 | `HTTP 回應｜PUT /api/v1/settings/mail → 200｜smtp_password（長度 22）` |
  | 應用程式日誌 | 建立交換器時以 `slog.Debug` 印出 community | `應用程式日誌｜第 10 筆輸出｜snmp_community（長度 20）` |
  | 稽核紀錄 | 建立交換器的稽核摘要附上 community | `資料庫｜資料表 audit_logs 欄位 summary｜snmp_community（長度 20）` |
  | 寄出的信 | 測試信的說明文字附上 SMTP 密碼 | `寄出的信｜主旨「[Würzburg] 郵件通知測試信」｜smtp_password（長度 22）` |

  第一次做信件實驗時沒有被抓到，原因是該測試自行實作郵件替身、沒有經過掃描；接上之後才抓到。

  **驗證**
  - 掃描器有單元測試（明文與各種跳脫形式、保留後重掃、巢狀 Secret 欄位、回應與標頭、信件各部分、日誌、樣本太短、報告不含 Secret）。
  - 後端：`gofmt`、`go vet`、golangci-lint v2.15.0（0 issues）、`go test -race -count=1 ./...` 全部通過。

  **範圍與限制**
  - 掃描只涵蓋後端整合測試（`src/backend/test`）跑到的路徑。各套件內的單元測試沒有接上掃描器。
  - 請求 URL 是測試自己組的，這項檢查只能防止 API 設計成要把 Secret 放在 URL；前端是否把 Secret 放進 URL 要由第 42 張的瀏覽器流程確認。
  - 真正的 SMTP 寄信（`mailer.SMTPMailer`）與 SNMP、LDAP、RADIUS 用戶端在整合測試中由替身取代，它們自己的日誌要等第 41 張接上模擬伺服器後才會被掃描到。
  - `e2e_pipeline_test.go` 原本的 `Security_SecretScan` 子測試保留未動，第 41 張改寫情境時一併移除。

