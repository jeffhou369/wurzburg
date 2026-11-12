/**
 * 驗證來源設定分頁的繁體中文（台灣）介面文字。
 * 沿用 auth.i18n.ts 的 `as const` 寫法；需要帶入變數的字串以函式表示。
 * 純 ASCII 的技術範例值（如 `ldaps://dc01.corp.local:636`）與語言無關，保留在樣板中。
 */
const SECRET_KEPT_HINT = '（已設定，留空則保持不變）';

export const AUTH_SETTINGS_TEXT = {
  title: '一般使用者驗證來源設定',
  description: '一個園區同時僅啟用一種外部驗證來源（AD 或 RADIUS），系統管理員帳號不經此路徑驗證。',
  switchWarning: {
    title: '切換驗證來源警告：',
    body: (affectedCount: number) =>
      `目前共有 ${affectedCount} 筆登記屬於原驗證來源帳號；更換後原擁有者將無法登入，名下登記需由系統管理員改派。`,
  },
  sources: {
    adDescription: '支援群組限制，登入自動帶入信箱與顯示名稱',
    radiusDescription: '僅驗證帳密，使用者登記時自行填寫信箱',
  },
  ad: {
    url: '伺服器網址（建議 ldaps://）',
    useTls: '啟用 TLS 加密連線（強烈建議生產環境保持勾選）',
    plaintextWarning: '未加密的目錄綁定將以明文傳輸組織密碼，僅供受控實驗室使用。',
    bindDn: '查詢帳號 Bind DN',
    bindPassword: (hasPassword: boolean) => `查詢密碼 ${hasPassword ? SECRET_KEPT_HINT : ''}`,
    baseDn: '搜尋起點 Base DN',
    allowedGroup: '允許登入群組 DN（選填，留空則全部允許）',
    save: '儲存 AD 設定',
  },
  radius: {
    host: 'RADIUS 伺服器 IP / 主機名稱',
    port: '驗證埠（預設 1812）',
    secret: (hasSecret: boolean) => `Shared Secret ${hasSecret ? SECRET_KEPT_HINT : ''}`,
    timeout: '逾時（毫秒）',
    save: '儲存 RADIUS 設定',
  },
  test: {
    title: '測試外部驗證連線',
    usernamePlaceholder: '測試帳號',
    passwordPlaceholder: '測試密碼',
    run: '測試登入',
    running: '測試中...',
    success: '測試登入成功',
    failure: '測試登入失敗',
    error: '測試失敗',
    errorHint: '連線異常',
  },
  saved: '驗證來源設定已儲存',
  saveFailed: '儲存失敗',
  saveFailedHint: '請檢查欄位',
} as const;
