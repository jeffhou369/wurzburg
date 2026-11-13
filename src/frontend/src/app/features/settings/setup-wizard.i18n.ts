/**
 * 初始設定精靈的繁體中文（台灣）介面文字。
 * 沿用 auth.i18n.ts 的 `as const` 寫法：含變數的字串用函式。
 * 技術性的 ASCII 範例值（例如 ldaps://ad.example.com:636、10.0.0.1）屬於格式提示，仍留在範本中。
 */
export const SETUP_WIZARD_TEXT = {
  title: '初始設定精靈',
  closeAria: '離開精靈（進度自動保存）',
  stepsAria: '設定步驟',
  steps: [
    { index: 1, label: '園區資訊' },
    { index: 2, label: '驗證來源' },
    { index: 3, label: '郵件通知' },
    { index: 4, label: '第一台交換器' },
    { index: 5, label: '檢視網段' },
  ],
  placeholders: {
    password: '密碼',
  },
  step1: {
    title: '步驟 1：園區基本資訊',
    description: '設定系統識別名稱與顯示時區。名稱用於側欄與信件，時區用於全系統介面顯示。',
    campusName: '園區顯示名稱 *',
    timezone: '顯示時區 (IANA Timezone) *',
    timezoneHelp: '例如 Asia/Taipei、UTC 或 America/New_York。內部存儲一律使用 UTC。',
    publicBaseUrl: '系統公開網址',
    publicBaseUrlHelp:
      '通知信件內的連結使用這個網址，請填使用者實際開啟本系統的網址。留空代表尚未設定，系統將停止寄送通知信（含步驟 3 的測試信）。',
    publicBaseUrlInvalid:
      '網址格式不正確：請輸入 HTTP 或 HTTPS 根網址，例如 https://ip.example.com，且不可含帳密、路徑、查詢參數或片段。',
  },
  step2: {
    title: '步驟 2：一般使用者驗證來源',
    description: '選擇並設定組織驗證來源（AD 或 RADIUS）。使用者登入後登記自己負責的位址。',
    adUrl: 'AD 伺服器網址 *',
    adUseTls: '啟用 TLS 加密連線（強烈建議生產環境保持勾選）',
    adBindDn: '查詢帳號 DN',
    adBindPassword: '查詢密碼',
    adBaseDn: '搜尋 Base DN *',
    radiusHost: 'RADIUS 主機 *',
    radiusPort: '連接埠',
    radiusSecret: '共用密碼 (Shared Secret) *',
    radiusSecretPlaceholder: '共用金鑰',
    testTitle: '連線測試（以一組測試帳密驗證登入）',
    testUsername: '測試帳號',
    testPassword: '測試密碼',
    testRunning: '驗證中...',
    testRun: '儲存並測試登入',
    testFailed: '測試失敗',
  },
  step3: {
    title: '步驟 3：郵件通知設定 (SMTP)',
    description:
      '設定系統發送未登記位址彙整、每週總表與到期催認信件的 SMTP 伺服器與管理員收件清單。',
    host: 'SMTP 伺服器主機 *',
    port: '埠號',
    tlsMode: '加密模式',
    tlsNone: '無加密',
    username: '認證帳號',
    password: '認證密碼',
    sender: '寄件人信箱 (From) *',
    adminRecipients: '系統管理員收件清單（多筆請用逗號隔開）*',
    testTitle: '寄送測試信',
    testRecipient: '收件信箱',
    testRunning: '寄送中...',
    testRun: '儲存並寄出測試信',
    testFailed: '測試失敗',
  },
  step4: {
    title: '步驟 4：新增第一台 ARP 來源交換器',
    description:
      '新增擔任 L3 閘道的 Cisco 交換器（IOS-XE 或 NX-OS），系統將定時透過 SNMP 讀取其 ARP 表與介面位址。',
    name: '交換器名稱 *',
    mgmtIp: '管理 IP 位址 *',
    platform: '作業平台',
    snmpVersion: 'SNMP 版本',
    community: 'Community 字串 *',
    communityPlaceholder: '例如 public',
    v3Username: 'v3 帳號 (Username) *',
    authProtocol: '認證協定',
    authPassword: '認證密碼',
    privProtocol: '加密協定',
    privPassword: '加密密碼',
    testTitle: '測試讀取（確認 SNMP 憑證與連線）',
    testRunning: '讀取中...',
    testRun: '儲存並測試讀取',
    testSucceeded: (arpCount: number, subnetCount: number) =>
      `讀取成功，共讀到 ${arpCount} 筆 ARP、${subnetCount} 個介面位址`,
    testReadFailed: (errorClass: string, errorSummary: string) =>
      `讀取失敗（${errorClass}）：${errorSummary}`,
    testRequestFailed: '讀取測試失敗',
  },
  step5: {
    title: '步驟 5：檢視並設定納管網段',
    description:
      '檢視由交換器介面探索之網段，將需要追蹤的網段設為「納管」，非管理網段（如 DHCP / 訪客無線）設為「不納管」。',
    skippedNoticeTitle: '提醒：',
    skippedNotice:
      '因為步驟 4 略過了交換器設定，系統目前尚未探索到任何交換器介面位址。您可以直接完成精靈，稍後再於系統中新增。',
    syncRunning: '同步讀取中...',
    syncRun: '觸發同步探索網段',
    noSubnets: '目前尚未發現任何網段。您可以點擊「觸發同步探索網段」讀取交換器，或直接結束精靈。',
    subnetCounts: (ipCount: number, onlineCount: number) =>
      `IP 數：${ipCount}，在線：${onlineCount}`,
    stateOptions: {
      pending: '待檢視 (pending)',
      managed: '納管 (managed)',
      unmanaged: '不納管 (unmanaged)',
    },
  },
  footer: {
    leave: '離開（保留目前進度）',
    skip: '稍後再設定（跳過此步）',
    saving: '處理中...',
    saveAndContinue: '儲存並繼續',
    finish: '完成精靈，進入系統',
  },
  toast: {
    saveFailed: '儲存失敗',
    checkFieldsCorrect: '請確認欄位正確',
    checkFields: '請確認欄位',
    nameAndIpRequired: '請輸入名稱與 IP',
    switchCreateFailed: '新增交換器失敗',
    subnetStateUpdated: '網段狀態已更新',
    subnetStateUpdateFailed: '更新失敗',
    wizardCompleted: '初始設定精靈已完成！',
  },
} as const;
