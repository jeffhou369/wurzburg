# 前端功能插槽與開發指引

為避免多張工作項目同時修改共用的外殼（`ShellLayoutComponent`）與路由檔案，系統已於第 03 張工作項目建立好以下插槽與集中機制：

## 1. 首頁工作臺插槽（`WorkbenchPage`）
位置：`src/frontend/src/app/features/workbench/workbench.page.ts`
- `[slot="checklist"]`：由第 32 張（尚未完成的設定清單）填入。
- `[slot="dashboard"]`：由第 16 張（統計儀表板）填入。
- `[slot="ip-table"]`：由第 15 張（IP 管理表格與篩選）填入。

## 2. IP 紀錄抽屜插槽（`IpRecordSheetComponent`）
位置：`src/frontend/src/app/shared/components/ip-record-sheet.component.ts`
- `[slot="registration"]`：由第 18 張（登記表單）填入。
- `[slot="observations"]`：由第 13 張（觀測來源與變更歷程）填入。

## 3. 設定頁子分頁插槽（`SettingsPage`）
位置：`src/frontend/src/app/features/settings/settings.page.ts`
- `[slot="settings-campus"]`：第 32 張
- `[slot="settings-auth"]`：第 07 / 21 / 22 張
- `[slot="settings-mail"]`：第 19 / 28 / 29 / 30 張
- `[slot="settings-oui"]`：第 17 張
- `[slot="settings-retention"]`：第 14 / 18 張

## 4. 側欄計數徽章（`SidebarBadgesService`）
位置：`src/frontend/src/app/core/sidebar-badges.service.ts`
- `pendingSubnetsCount`：由第 12 張網段發現更新
- `openConflictsCount`：由第 25 / 31 張衝突功能更新

## 5. 介面文字與樣式規定
- **文字**：各功能於 `src/frontend/src/app/features/<feature>/<feature>.i18n.ts` 建立自己的繁體中文常數檔。
- **顏色**：禁止在元件中使用 hex 色碼，一律使用 `src/styles/tokens.scss` 定義的 CSS 變數。
- **圖示**：一律使用 `@lucide/angular`（如 `<svg lucideServer [size]="18" />`），不可使用 Emoji。
