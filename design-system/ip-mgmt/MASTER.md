# Würzburg Design System (IP Management Dashboard)

本設計系統以開源專案 `EasyCLIProxyAPI`（預設淺色 `light`）為視覺契約基準，結合 `UI UX Pro Max` 之設計規範、反模式檢查與交付前清單制定而成。

> **規則優先順序**：當設計決策有衝突時，依序以：
> 1. `docs/Wurzburg-UI-Style-Kit.zip` 內之原始樣式檔（`easycliproxyapi/src/styles/tokens/`）
> 2. `CLIProxyAPI-UI-style-reference.md`
> 3. 本檔案（`design-system/ip-mgmt/MASTER.md`）
> 4. `UI UX Pro Max` 之自動生成建議（不得覆寫基準色票）

---

## 1. 全域視覺規則（Global Rules）

### 色彩系統（EasyCLIProxyAPI 預設淺色 `light`）

| 語意角色 (Role) | 色碼 (Hex) | CSS Token | 說明 |
| --- | --- | --- | --- |
| 視窗 / 工作區 | `#FFFFFF` | `--color-workspace` | 頂層畫布與背景 |
| 表面 / 卡片底色 | `#FFFFFF` | `--color-surface` | 主要卡片、對話框面 |
| 微弱底色 | `#EFF3F0` | `--color-surface-subtle` | 表頭底色、次級容器 |
| 懸停底色 | `#F6F8F6` | `--color-surface-hover` | 列懸停、按鈕懸停 |
| 按下底色 | `#F0F0F0` | `--color-surface-active` | 點擊作用時底色 |
| 側欄背景 | `#F7F8FA` | `--sidebar-bg` | 左側導覽欄冷灰底色 |
| 主要文字 | `#0F172A` | `--color-text-primary` | 大標題、內文主要資訊 |
| 次要文字 | `#1E293B` | `--color-text-secondary` | 表頭文字、次標 |
| 弱化文字 | `#475569` | `--color-text-muted` | 說明文字、副標、欄位說明 |
| 停用 / 離線文字 | `#64748B` | `--color-text-disabled` | 離線 IP 紀錄文字、停用項目 |
| 強調色 / 主按鈕底 | `#222222` | `--color-accent` | 主要操作（近黑實心） |
| 強調加深 | `#000000` | `--color-accent-strong` | 強調 Hover / 焦點加深 |
| 預設邊框 | `#E2E8F0` | `--color-border-default` | 1px 預設細邊框 |
| 極淡邊框 | `#F1F5F9` | `--color-border-subtle` | 表格列分割線 |
| 焦點外圈 | `#222222` | `--color-border-focus` | 鍵盤導覽可見焦點 |
| 成功 / 在線 | `#2F6B3F` | `--color-success` | 偏低飽和綠（文字） |
| 成功膠囊底 / 邊 | `#F0FDF4` / `#BBF7D0` | `--state-pill-success-bg` / `border` | 在線、已解消膠囊 |
| 狀態圓點（活狀態） | `#10B981` | `--status-dot-success-bg` | 在線狀態指示圓點 |
| 警告 / 未登記 | `#C77B30` | `--color-warning` | 警告主色 |
| 警告膠囊底 / 邊 | `#FFF7E9` / `#DFBF91` | `--color-warning-subtle` / `border` | 未登記、即將到期膠囊 |
| 危險 / 衝突 | `#9F2D20` | `--color-danger` | 危險主色 |
| 危險膠囊底 / 邊 | `#FFF1EE` / `#DFB7B0` | `--color-danger-subtle` / `border` | 衝突、已到期膠囊 |
| 柔和危險按鈕底 / 邊 | `#FFF1F2` / `#FECDD3` | `--button-danger-soft-bg` / `border` | 停用、解綁按鈕 |

### 字型排印（Typography）

- **繁體中文字族（無襯線）**：`'Noto Sans TC', 'Noto Sans CJK TC', 'PingFang TC', 'Microsoft JhengHei', system-ui, sans-serif`
- **等寬字族（IP / MAC / CIDR / 時間 / 埠號）**：`'Cascadia Code', 'SFMono-Regular', Consolas, 'Liberation Mono', monospace` 並啟用 `font-variant-numeric: tabular-nums;`
- **字級階層**：
  - Display / KPI 大數字：`28px` / 字重 `600`
  - Page Title：`24px` / 字重 `600`
  - Dialog / Sheet Title：`20px` / 字重 `600`
  - Panel Title：`18px` / 字重 `600`
  - Section Header：`16px` / 字重 `600`
  - Body / Label：`14px` / 字重 `400` 或 `500`
  - Table Body：`13px` / 字重 `400`
  - Table Header / Meta：`12px` / 字重 `500`
  - Caption：`12px` / 字重 `400`

### 間距與圓角（Spacing & Radii）

- 圓角：
  - 卡片、對話框、Sheet：`12px` (`--radius-lg`)
  - 按鈕、輸入框、選單：`8px` (`--radius-md`)
  - 小標籤 / 徽章：`4px` (`--radius-sm`)
  - 狀態燈號膠囊、圓點：`9999px` (`--radius-full`)
- 陰影（極淡細緻陰影，非厚重模糊）：
  - `--shadow-panel`: `0 1px 3px 0 rgba(15, 23, 42, 0.05), 0 1px 2px -1px rgba(15, 23, 42, 0.03)`
  - `--shadow-dialog`: `0 24px 64px -12px rgba(0, 0, 0, 0.24), 0 8px 24px -8px rgba(0, 0, 0, 0.12)`

---

## 2. 元件規範（Component Specs）

### 側欄（Sidebar）
- 固定寬度：`264px`，冷灰背景 `#F7F8FA`，右側 `1px solid #E2E8F0` 分割線。
- 選中項目：白底圓角膠囊（`#FFFFFF`、圓角 `8px`、極淡陰影），非高飽和藍條。
- 階層：
  1. 頂段：Logo + 產品名稱「Würzburg」+ 副標「IPv4 管理系統」
  2. 中段導覽：工作臺（首頁）、IP 位址、網段（帶待檢視計數）、交換器、衝突（帶未處理計數）、通知紀錄、稽核紀錄、系統設定（僅管理員可見）
  3. 底段：登入者身分標示、主題切換切換按鈕、登出

### 按鈕（Buttons）
- **主要按鈕（Primary）**：底色 `#222222`、文字 `#FFFFFF`、圓角 `8px`、內距 `10px 16px`、字重 `600`。
- **次要按鈕（Secondary）**：白底、`1px solid #E2E8F0`、文字 `#0F172A`。
- **幽靈按鈕（Ghost）**：透明底，懸停時出現 `#F6F8F6`。
- **柔和危險（Danger Soft）**：底色 `#FFF1F2`、邊框 `#FECDD3`、文字 `#E11D48`。
- **實心危險（Danger Solid）**：底色 `#9F2D20`、文字 `#FFFFFF`，僅用於破壞性動作之最終確認。

### 狀態燈號與膠囊（Status Pills）
- 嚴格禁止單獨以顏色傳達狀態。每一種狀態膠囊必須包含文字標籤（例如 `● 在線`、`未登記`、`衝突`）。
- 組合狀態：以中圓點間隔文字表達（例如 `在線 · 未登記`、`離線 · 已到期`）。
- 離線樣式：整列降低飽和度（灰階呈現，文字變更為 `--color-text-disabled`），但資料仍保持可選取並可點擊查看歷史。

### 表格（Data Table）
- 1px 外框與圓角 `8px`，表頭背景 `--color-surface-subtle`（`#EFF3F0`），列高約 `44px`（舒適密度，避免擁擠）。
- 內建搜尋列、狀態膠囊篩選列、分頁控制項、多選全選框。

---

## 3. 反模式與禁用清單（Anti-Patterns）

- ❌ **禁止使用 Emoji 作為功能圖示**：一律使用 Lucide SVG 線條圖示（`stroke-width: 2`，尺寸 `16-20px`）。
- ❌ **禁止將飽和品牌藍設為全域主色**：主色為近黑 `#222222`，藍色僅出現在少數分析圖表。
- ❌ **禁止依賴顏色作為唯一狀態編碼**：色盲與黑白列印必須皆能透過文字清楚判斷狀態。
- ❌ **禁止隱藏鍵盤焦點邊框**：所有可聚焦元素在 `focus-visible` 時必須具備明確對比焦點框。
- ❌ **禁止跳動式 Layout Shift**：懸停縮放不得引起周圍元素排版跳動。
- ❌ **禁止在組件中硬編碼十六進位色碼**：所有樣式必須參照 CSS 變數。

---

## 4. 交付前檢查清單（Pre-Delivery Checklist）

- [ ] 所有圖示均為 Lucide SVG 圖示，無任何 Emoji。
- [ ] 所有可點擊項目均標註 `cursor: pointer` 與清晰 Hover 回饋。
- [ ] 淺色模式文字對比度符合 WCAG AA 4.5:1 標準。
- [ ] 鍵盤 Tab 鍵可完整遍歷所有導覽與操作，焦點樣式清晰可見。
- [ ] 動效時間限制在 150-250ms 之間，並支援 `@media (prefers-reduced-motion: reduce)`。
- [ ] 響應式佈局在 375px（手機）、768px（平板）、1024px（小型筆電）、1440px（桌上螢幕）無任何水平破版與文字裁切。
- [ ] 離線紀錄保持文字可讀且可選取複製。
- [ ] 長 IP、MAC、URL 與識別碼具備安全換行保護（`break-all` 或 `overflow-wrap`）。
