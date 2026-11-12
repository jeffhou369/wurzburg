# 19 — 郵件設定、測試信與共用版面

Status: resolved
Blocked by: 03, 05, 06

## 要做什麼

建立寄信的能力與三種通知共用的版面，讓第 28、29、30 張只需要各自決定「寄給誰、什麼時候、內容是什麼」。

**郵件設定**

- 系統管理員在設定頁設定 SMTP（主機、埠、加密方式、帳號、密碼、寄件人）與系統管理員收件清單。密碼加密保存，儲存後不回顯。
- 「寄測試信」按鈕：寄一封測試信到指定的地址，成功或失敗當場顯示原因。

**寄信**

- 實作寄信的介面：每封信都是 `multipart/alternative`，同時有純文字與 HTML。
- 寄送失敗時以有限次數、間隔遞增地重試；最終失敗要留下紀錄與原因。
- 每一封的結果寫入通知紀錄：種類、去重鍵、收件人、主旨、狀態、嘗試次數、錯誤、時間。
- 通知紀錄超過保留期（預設 1 年）後清除。

**共用版面**

- HTML：表格版型、inline CSS、內容寬約 600px；不用 JavaScript、外部字型、遠端 CSS 或背景圖；顏色沿用淺色主題的 token；根元素標示 `zh-Hant`。
- 提供組成一封通知所需的區塊：標題與摘要、摘要數字列、明細表、主要按鈕（表格做的按鈕，並附同一個網址的純文字連結）、頁尾（系統顯示名稱、為何收到、請勿直接回覆、時區說明）。
- 純文字版本由同一份內容產生，資訊與 HTML 對等；狀態以「[在線]」這類括號標籤呈現。
- 放進信裡的使用者輸入一律跳脫。

**畫面**：「通知紀錄」頁列出寄送結果，失敗的有明顯標示與原因。

## 驗收條件

- [x] 系統管理員可以儲存 SMTP 設定；密碼在 API 回應中讀不到，只看得到「已設定」
- [x] 測試信寄得出去；SMTP 設定錯誤時顯示可理解的原因
- [x] 寄出的信有純文字與 HTML 兩個部分，且兩者資訊對等
- [x] HTML 沒有 `<script>`、外部字型、遠端樣式與背景圖；樣式都在元素上
- [x] 主要按鈕之外，同一個網址以純文字再出現一次
- [x] 狀態在 HTML 與純文字中都有文字
- [x] 內容中帶有 HTML 標籤的使用者輸入在信中被跳脫
- [x] 暫時性的寄送失敗會重試；超過次數後紀錄為失敗並帶原因
- [x] 每一封都在通知紀錄留下一筆；通知紀錄頁看得到，失敗的有標示
- [x] 通知紀錄超過保留期後被清除
- [x] 郵件伺服器連不上時，API 與其他功能照常運作
- [x] SMTP 密碼不出現在日誌、稽核紀錄與信件中
- [x] 設定的變更寫入稽核紀錄，且不含密碼
- [x] 一般使用者不能看設定、不能寄測試信

## Comments

已完成 19 — 郵件設定、測試信與共用版面：
- 後端 `internal/mailer/`：實作符合 `interfaces.Mailer` 的 `SMTPMailer`（`multipart/alternative` MIME、STARTTLS/SMTPS/無加密、指數退避 3 次重試、寫入 `notification_logs` 與保留期清理）、共用 600px 表格 inline-CSS HTML/純文字對等樣板 `RenderEmail`（自動跳脫 XSS），以及 `/settings/mail`、`/settings/mail/test`、`/notifications/logs`、`/notifications/states`、`/notifications/states/{dedupKey}/reset` API。
- API 契約補充：於 `docs/api/resources/notifications.yaml` 與 `docs/api/openapi.yaml` 新增 `GET /api/v1/notifications/states` 供「通知紀錄」頁列出目前已通知的去重狀態並提供重設操作。
- 前端：`settings/mail-settings.component.ts` 掛載於設定頁 `mail` 分頁；`notifications/notifications.page.ts` 實作寄送紀錄查詢、失敗原因標示與去重狀態重設，並完成單元測試驗證。

## 邊界

- 這張不產生任何一種業務通知；三種信由第 28、29、30 張完成。
- 只修改本功能自己的後端模組、設定頁中自己的子頁、通知紀錄頁、介面文字與自己那一份 API 描述。
- 契約或資料庫結構有缺漏時，只做最小的補充，並在本檔案的 `## Comments` 記下。

## 參考

- 開發規格第 6 節
- PRD 第 11.2、11.3 節、附錄 B
- `docs/plan/mvp-spec.md` 使用者故事 16、122–127
