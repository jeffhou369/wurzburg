# AGENTS.md

本檔案提供 Claude Code（claude.ai/code）與其他程式代理在此儲存庫工作時的指引。

## 語言

一律使用繁體中文（台灣用語）：

- 與使用者的對話
- 所有文件，包含 `README.md`
- 程式註解

程式識別碼（變數、函式、檔名、資料表、環境變數、容器名稱）維持 ASCII 英文，專案名在識別碼中寫作 `wurzburg`（PRD ADR-008）。

## 儲存庫現況與開發指令

基礎骨架已建立（Go 後端、Angular 20 前端、MariaDB Compose 部署），目前正依 `docs/task/mvp/` 進行平行實作。

儲存庫目錄名稱含 `ü`，在 shell 指令中路徑要加引號；且使用 Docker Compose 時須以 `name: wurzburg` 或 `COMPOSE_PROJECT_NAME=wurzburg` 避免路徑非 ASCII 字元觸發 buildx gRPC 標頭錯誤。

### 常用開發指令

**後端（`src/backend`）：**
```sh
cd src/backend
go build -o /dev/null ./cmd/wurzburg          # 建置執行檔
gofmt -l . && go vet ./...                    # 排版與靜態檢查
go test ./...                                 # 執行全部測試（含 Testcontainers 啟動 MariaDB）
go test -short ./...                          # 只執行不需 Docker 的單元／快速測試
go test -v -run TestName ./test/...           # 執行單一測試
```

**前端（`src/frontend`）：**
```sh
cd src/frontend
npm ci                                        # 安裝相依套件
npm run build                                 # 建置 Angular 靜態檔案（輸出至 dist/frontend/browser）
npm run lint                                  # TypeScript 型別檢查
npm run format:check                          # Prettier 排版檢查
npm run generate:api                          # 依 docs/api/openapi.yaml 產生前端 TypeScript 型別
npm test                                      # 以 ChromeHeadless 執行全部前端單元測試
npx ng test --watch=false --browsers=ChromeHeadless --include=src/app/app.spec.ts  # 執行單一前端測試檔
```

**容器建置與啟動：**
```sh
docker build -t wurzburg:dev .
docker compose -f infra/docker-compose.yml up -d --no-build
```

## 目錄慣例與平行作業規則

為了讓多個工作項目（07 到 32）能安全且最大化平行開發而不互相衝突，請嚴格遵守下列目錄邊界：

- **共用的地基檔案**（如資料庫遷移基礎結構、全域 Router 初始化、Angular 側欄外殼、共用 CSS tokens 與 OpenAPI 總入口）：只在地基工作項目（**03 到 06**）中修改；功能工作項目不得隨意改動共用檔案。
- **資料庫遷移檔案**：一律放在 `src/backend/migrations/`，檔名採用 `YYYYMMDDHHMMSS_<slug>.sql` 時間戳記前綴（Goose 格式），避免平行開發產生序號衝突。
- **後端功能模組**：每個功能在 `src/backend/internal/<feature>/` 建立獨立套件，並實作模組提供的路由註冊函式。
- **前端功能模組**：每個功能在 `src/frontend/src/app/features/<feature>/` 建立獨立的頁面元件與自己的文字檔（繁體中文），掛載進第 03 張預留的路由或插槽。
- **API 契約**：每個資源一份獨立的 OpenAPI 描述檔（由第 05 張定義位置）；若功能開發中發現契約缺漏，做最小補充並記在該工作項目檔案的 `## Comments` 中，由第 33 張統一收斂。

## 文件與優先順序

| 檔案 | 說明 |
| --- | --- |
| `docs/plan/dev-spec.md` | 開發規格。**與 PRD 衝突時以它為準**；第 2 節列出所有與 PRD 不一致之處。 |
| `CONTEXT.md` | 名詞定義。文件、介面文字、程式註解都使用這裡的用語，並避開它列為「Avoid」的說法。 |
| `docs/adr/` | 0009 起的架構決策。編號接在 PRD 內的 ADR-001 到 008 之後。 |
| `docs/IP-Mgmt_PRD_ADR.docx` | PRD 與 ADR-001…008，v0.2。背景與設計依據；UI（第 10 節）與郵件版面（第 11 節）仍以它為主。 |
| `docs/Wurzburg-UI-Style-Kit.zip` | 上游樣式原始檔（未修改，MIT 授權）與設計說明 `CLIProxyAPI-UI-style-reference.md`。 |
| `docs/task/` | 工作項目，規格確認後才拆。 |

讀取 docx 與 zip：

```sh
textutil -convert txt -stdout docs/IP-Mgmt_PRD_ADR.docx        # 把 PRD 轉成純文字（macOS）
unzip -l docs/Wurzburg-UI-Style-Kit.zip                        # 列出樣式包內容
unzip -p docs/Wurzburg-UI-Style-Kit.zip README.md              # 樣式包索引：哪個檔案放哪些 token
unzip -p docs/Wurzburg-UI-Style-Kit.zip CLIProxyAPI-UI-style-reference.md
```

PRD 提到的 `/workspace/specs/ip-mgmt/ui-ref/…` 與三張截圖都在 zip 裡；`/workspace/top10/nextlevelbuilder_ui-ux-pro-max-skill.md` 這裡沒有。

## 要做的產品

Würzburg 是內部用的 IPv4 管理系統：定時以 SNMP 讀取 Cisco 交換器（IOS-XE、NX-OS）的 ARP table，為納管網段內的每個位址維護一筆 IP 紀錄；使用者登入後登記自己負責的位址；系統寄信通知未登記的位址與即將到期的登記。一個園區裝一套。

明確不做：IPv6、內建備份與還原、DHCP／DNS 整合、非 Cisco 設備、以 CLI／SSH 抓 ARP、任何外部 OUI／Whois API、NMS 類的監控、多租戶與跨園區管理。

## 架構

- **部署：** Docker Compose。`app` 是單一 Go 執行檔，同時提供 API、排程器與前端靜態檔；`db` 是 MariaDB，只在內部網路。預設 HTTP，另附選用的 Nginx／TLS 範例。設定走環境變數或掛載檔，日誌寫到 stdout。
- **後端：** Go（ADR 0010）。REST 加 JSON，以一份 OpenAPI 描述檔為契約。資料庫結構變更用有版本編號的 SQL 檔，啟動時自動套用。
- **前端：** Angular，元件自己寫，行為用 Angular CDK，不用 Angular Material 的外觀（ADR 0014）。API 型別由 OpenAPI 描述檔產生。
- **倉庫配置：** `src/backend`、`src/frontend`、`infra/`。
- **採集：** 每 3 分鐘，對每筆 VRF 讀取設定讀 ARP 表、介面位址表與介面名稱。採集模組對外只輸出這三樣，平台差異封在模組內。失敗重試一次，總嘗試次數固定為 2，以 VRF 讀取設定為單位。
- **測試：** 外部系統全部用模擬器，不依賴實體交換器。SNMP 的測試資料來自原廠文件與公開範例，尚未經實機確認（開發規格第 12 節）。

實際採用的程式庫與版本，在動手前查最新文件後確定。

## 容易違反的規則

完整規則在開發規格第 3 到 6 節，下面是最容易做錯的。

- **IP 紀錄只存在於管理範圍內。** 位址要落在納管的網段、不在排除範圍內、不是閘道位址，才有 IP 紀錄。網段改為不納管或位址被劃入排除範圍時，IP 紀錄直接刪除（ADR 0013），刪除前要確認並寫入稽核明細。
- **離線不等於刪除。** 有擁有者的 IP 紀錄離線多久都保留；只有未登記且離線超過設定天數的才自動清除。
- **在線、登記、衝突是三個獨立的面向。** 「在線 · 未登記」這類組合在介面、郵件、匯出都要能表達。「即將到期」是推算出來的，不是儲存的狀態。
- **在線來自多個來源。** 一筆 IP 紀錄可以同時被多台交換器看到，任一台看到就算在線；讀取失敗的輪次不能讓 IP 變成離線。
- **衝突只有兩種。** 多個 MAC 並存，或有擁有者的 IP 換了 MAC。未登記的 IP 換 MAC 直接更新採信 MAC。多台回報同 IP 同 MAC 不是衝突。
- **擁有者是帳號。** 權限只看帳號，擁有者信箱不決定任何權限（ADR 0012）。授權一律在 API 逐筆判斷，藏按鈕不算存取控制。
- **網段不靠猜。** 網段來自介面位址表或手動新增，絕不從主機位址假設遮罩（ADR 0011）。
- **狀態不能只靠顏色表達。** 介面、HTML 郵件、純文字郵件、Excel 匯出都要有狀態文字。
- **Secret** 儲存後不回顯，不出現在日誌、稽核紀錄、錯誤頁、URL 與郵件本文。
- **郵件** 一律 `multipart/alternative`，純文字與 HTML 資訊對等；HTML 用表格版型與 inline CSS，寬約 600px，不用 JavaScript、外部字型或背景圖。信中的連結只通往需登入的頁面。使用者輸入的文字要先跳脫。
- **通知去重** 成功寄出才標為已通知；裝置離線不解除已通知的狀態。
- **故障隔離：** SMTP 或驗證來源中斷不得影響採集與 Web；一筆 VRF 讀取設定失敗不得影響其他的。
- **因園區而異的值都是設定**（開發規格第 10 節），不要寫死。資料庫存 UTC，顯示與排程用園區的時區。

## UI

- 來源不一致時的優先順序：樣式包內的原始檔，其次 `CLIProxyAPI-UI-style-reference.md`，最後是 PRD 第 10 節。
- 預設主題是 EasyCLIProxyAPI 的 `light`：純白工作區、冷灰側欄 `#f7f8fa`、近黑強調色 `#222222`。主題必須整套切換，不可跨主題混用強調色或自行調色。
- 樣式包中的 token 來源：`easycliproxyapi/src/styles/tokens/{semantic,base,components}.css` 與 `easycliproxyapi/src/themes/*.ts`。token 以 CSS 變數集中管理，不要到處散落色碼。
- 版型：左側固定側欄（264px）加右側白色內容區，沒有頂部導覽列。首頁是工作臺，上半部 KPI 卡，下半部 IP 表。編輯在右側 Sheet 或對話框完成。
- 介面語言為繁體中文。IP、MAC、CIDR、埠號與時間一律用等寬字族並啟用 `tabular-nums`。圖示用 Lucide。
- 前端設計要使用 UI UX Pro Max skill（`nextlevelbuilder/ui-ux-pro-max-skill`），產出存成 `design-system/ip-mgmt/MASTER.md`；它產生的色盤不得覆蓋樣式包的色票。這個 skill 裝在使用者層級（名稱 `ui-ux-pro-max`），不在儲存庫裡；在別的機器或環境工作時，先確認它可用。
- 樣式包的上游檔案為 MIT 授權，複製使用時要保留 `LICENSE`。

## Agent skills

### Issue tracker

本機 Markdown：spec 放 `docs/plan/`，工作項目放 `docs/task/`。見 `docs/agents/issue-tracker.md`。

### Triage labels

使用預設的五個標籤名稱，寫在每個檔案開頭的 `Status:` 行。見 `docs/agents/triage-labels.md`。

### Domain docs

單一 context：根目錄的 `CONTEXT.md` 與 `docs/adr/`。見 `docs/agents/domain.md`。

## 尚未定案

- 開發規格是 v0.1 草稿，待 Jeff 審閱；標有 ※ 的條目是規格補上的細節，尚未逐項確認。
- PRD 的 Q-10（備份的文件說明）與 Q-16（唯讀稽核角色）沒有討論過，暫依 PRD 的 SA 建議。
- PRD 中的效能數字與里程碑日期是 SA 建議，不是實測值，也不是承諾。
