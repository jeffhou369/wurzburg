/**
 * 系統設定各分頁（園區資訊、OUI、保留期限與設定頁框架）的繁體中文（台灣）介面文字。
 * 沿用 auth.i18n.ts 的 `as const` 寫法；需要帶入變數的字串以函式表示。
 */
import { SHELL_TEXT } from '../../core/i18n/shell.i18n';

export const CAMPUS_TEXT = {
  title: '園區基本資訊與設定精靈',
  description:
    '設定園區識別名稱與顯示時區；名稱將用於側欄標頭與信件主旨，時區用於全系統時間顯示與排程。',
  reopenWizard: '重新開啟設定精靈',
  campusName: {
    label: '園區顯示名稱 *',
    placeholder: '例如 Würzburg 或 新竹二廠',
    help: '用於側欄標題、首頁工作臺與通知信件主旨之系統識別。',
  },
  timezone: {
    label: '園區顯示時區 (IANA Timezone) *',
    placeholder: 'Asia/Taipei',
    help: '例如 Asia/Taipei、UTC 或 America/New_York。資料庫一律儲存 UTC。',
  },
  publicBaseUrl: {
    label: '系統公開網址',
    placeholder: 'https://ip.example.com',
    help: '通知信件內連結使用的對外根網址，只接受 HTTP(S)，不可含帳密、子路徑、查詢參數或片段。留空代表尚未設定，系統將停止寄送通知信。',
    invalid:
      '網址格式不正確：請輸入 HTTP 或 HTTPS 根網址，例如 https://ip.example.com，且不可含帳密、路徑、查詢參數或片段。',
  },
  save: '儲存園區資訊',
  saving: '儲存中...',
  saved: '園區設定已儲存',
  saveFailed: '儲存失敗',
  saveFailedHint: '請確認時區與公開網址格式合法',
  documentTitle: SHELL_TEXT.documentTitle,
} as const;

/** 系統設定頁框架與分頁標籤 */
export const SETTINGS_PAGE_TEXT = {
  title: '系統設定',
  subtitle: '管理園區基本資訊、驗證來源、SMTP 通知、本地 OUI 資料與政策參數',
  tabs: {
    campus: '園區與精靈',
    auth: '驗證來源 (AD / RADIUS)',
    mail: '郵件與通知',
    oui: '本地 OUI 資料庫',
    retention: '自動清除與保存天數',
  },
} as const;

/** 本地 OUI 資料庫分頁 */
export const OUI_TEXT = {
  title: '本地 OUI 廠商資料庫',
  description: 'MAC 廠商名稱完全於本地資料庫比對，不會對外部網路發送任何查詢請求。',
  status: {
    version: '目前快照版本',
    filename: '資料來源檔名',
    entryCount: '收錄前綴數量',
    importedBy: '匯入者與時間',
    /** 匯入者後面以全形括號包住匯入時間 */
    timeOpen: '（',
    timeClose: '）',
  },
  uploadLabel: '上傳並替換 OUI 資料檔（CSV 格式：prefix,bitlen,vendor）',
  upload: '上傳並重新計算廠商',
  uploading: '匯入中...',
  imported: 'OUI 匯入完成',
  importedDetail: (entryCount: number) => `已更新 ${entryCount} 筆廠商前綴並重算現有紀錄`,
  importFailed: 'OUI 匯入失敗',
  importFailedHint: '請檢查檔案格式',
} as const;

/** 自動清除與保存天數分頁 */
export const RETENTION_TEXT = {
  title: '自動清除與保存天數政策',
  description: '設定離線判定輪數、未登記離線紀錄自動清除天數，以及登記有效期與到期催認規則。',
  autoPurgeDays: '未登記離線紀錄自動清除天數（0 代表停用）',
  autoPurgeDaysHelp: '已登記或已到期的 IP 紀錄不論離線多久皆不會被自動清除。',
  graceRounds: '離線判定寬限輪數',
  validDays: '登記有效天數',
  advanceDays: '到期前提前提醒天數',
  intervalDays: '逾期後復催間隔天數',
  maxReminders: '復催次數上限',
  save: '儲存政策設定',
  saved: '政策已更新',
  saveFailed: '儲存失敗',
  saveFailedHint: '請檢查輸入數值',
} as const;
