export const AUTH_TEXT = {
  title: '登入 Würzburg',
  subtitle: 'IPv4 位址管理系統',
  plaintextWarningTitle: '目前為非加密（HTTP）連線',
  plaintextWarningDesc:
    '帳號與密碼將以明文在網路上傳輸。建議僅於受信任的內部網路使用，並盡快依照部署文件啟用 HTTPS。',
  tabs: {
    org: '組織帳號（AD / RADIUS）',
    local: '系統管理員（本機帳號）',
  },
  fields: {
    username: '使用者名稱',
    password: '密碼',
    oldPassword: '目前密碼',
    newPassword: '新密碼（至少 12 個字元）',
    confirmPassword: '確認新密碼',
  },
  buttons: {
    loginOrg: '以組織帳號登入',
    loginLocal: '以系統管理員登入',
    changePassword: '更新密碼並繼續',
  },
  changePassword: {
    title: '首次登入強制變更密碼',
    subtitle: '為確保系統安全，您必須先更換環境變數指定的初始密碼，才能使用 Würzburg 管理介面。',
    mismatchError: '兩次輸入的新密碼不相符',
    lengthError: '密碼長度至少需要 12 個字元',
  },
  errors: {
    loginFailed: '登入失敗，請檢查帳號密碼',
    changePasswordFailed: '密碼更新失敗',
    sessionExpired: '登入工作階段已失效，請重新登入後再試',
  },
} as const;
