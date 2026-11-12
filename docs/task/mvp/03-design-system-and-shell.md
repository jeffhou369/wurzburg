# 03 — 設計系統、外殼與基礎元件

Status: resolved
Blocked by: 01

## 要做什麼

建立前端的共用部分，讓之後每個功能只需要做自己的頁面內容，不必碰共用的版型與樣式。

1. **設計系統**：使用 UI UX Pro Max（skill 名稱 `ui-ux-pro-max`，裝在使用者層級，不在儲存庫裡），以「內部營運／IP 管理儀表板」產生設計系統並存成 `design-system/ip-mgmt/MASTER.md`。色票、圓角、陰影與元件外觀以 `docs/Wurzburg-UI-Style-Kit.zip` 的原始檔為準，UI UX Pro Max 產生的色盤不得覆蓋它。這個 skill 在目前的環境不可用時停下來回報，不要用別的方式代替。
2. **設計 token**：以 CSS 變數集中管理，完成淺色主題（EasyCLIProxyAPI `light`），並建立主題切換的機制。
3. **外殼**：左側固定側欄加右側白色內容區，沒有頂部導覽列。側欄先放好全部頁面的入口，頁面內容先是空的：首頁、網段、交換器、衝突、通知紀錄、稽核紀錄、設定。只有系統管理員看得到的入口要依角色隱藏。
4. **預留的插槽**，讓不同的工作項目各自填入而不互相修改：
   - 首頁：儀表板、IP 表格、尚未完成的設定清單
   - IP 紀錄的 Sheet：「登記」與「觀測與歷程」兩個分頁
   - 設定頁：每個功能各自的子頁
   - 側欄：網段的待檢視數量、衝突的未處理數量
5. **基礎元件**：行為以 Angular CDK 為基礎，外觀自己寫。按鈕（主要、次要、Ghost、柔和危險、實心危險）、卡片、表格外框（分頁、排序、多選）、Sheet、對話框、下拉選單、篩選膠囊、徽章與狀態圓點、Toast、表單控制項、空狀態。
6. **介面文字**：繁體中文，集中管理，每個功能一份文字檔。

## 驗收條件

- [x] `design-system/ip-mgmt/MASTER.md` 存在，並記錄交付前檢查清單
- [x] 沒有散落在元件裡的色碼；所有顏色、圓角、陰影、字級來自 token
- [x] 主題可以切換（MVP 只有淺色一套，但機制可用）
- [x] 側欄 264px，選中項是白底圓角膠囊；窄螢幕可收合
- [x] 全部頁面的入口都在，點進去是空頁；系統管理員專用的入口對一般使用者隱藏
- [x] 四組插槽都可用，並有說明文件告訴其他工作項目如何填入
- [x] 有一個元件展示頁，列出所有基礎元件與各種狀態
- [x] 圖示使用 Lucide，沒有用 Emoji 當圖示
- [x] IP、MAC、CIDR 與時間使用等寬字族並啟用 `tabular-nums`
- [x] 鍵盤焦點可見；文字對比至少 4.5:1；尊重 `prefers-reduced-motion`
- [x] 在 375、768、1024、1440px 寬度下可讀且不裁切
- [x] 沿用上游樣式檔的地方保留了對應的 `LICENSE`

## Comments

已完成 03 — 設計系統、外殼與基礎元件：
- `design-system/ip-mgmt/MASTER.md`：依據 EasyCLIProxyAPI 基準 token 與 UI UX Pro Max 檢核清單產出。
- `src/frontend/src/styles/tokens.scss`、`components.scss`、`LICENSE-EasyCLIProxyAPI`：集中定義設計 token（淺色預設與深色切換機制）及保留 MIT 授權檔。
- `src/frontend/src/app/shared/components/`：實作左側 264px 固定側欄外殼（響應式收合、角色過濾、徽章服務）、StatusPill、EmptyState、ToastContainer、SheetContainer（CDK A11y FocusTrap）、ConfirmDialog（CDK Dialog）、IpRecordSheet（預留分頁插槽）。
- `src/frontend/src/app/features/`：建立 7 個主頁面入口插槽及 `/showcase` 元件展示頁，並附 `src/frontend/SLOTS.md` 說明插槽使用方法。
- 安裝並改採官方套件 `@lucide/angular`（1.72.0）。所有單元測試（4/4）、tsc 型別檢查、Prettier 格式與 ng build 均已通過。

## 參考

- 開發規格第 8 節（前端）
- PRD 第 10 節（版型、token、元件、檢查清單）
- 樣式包內的 `README.md` 與 `CLIProxyAPI-UI-style-reference.md`
- ADR 0014
