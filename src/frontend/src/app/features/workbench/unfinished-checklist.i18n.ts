/**
 * 首頁「未完成初始設定」清單的繁體中文（台灣）介面文字。
 * 沿用 auth.i18n.ts 的 `as const` 寫法；需要帶入變數的字串以函式表示。
 */
export const CHECKLIST_TEXT = {
  title: (count: number) => `系統尚未完成的初始設定（${count} 項待處理）`,
  description: '以下項目將影響使用者登入、資料採集與通知發送；建議依序設定或點擊按鈕開啟設定精靈。',
  openWizard: '開啟設定精靈',
  listLabel: '未完成設定項目清單',
  consequence: (consequence: string) => `影響：${consequence}`,
  goToSetting: '前往設定',
  goToSettingLabel: (title: string) => `前往設定 ${title}`,
} as const;
