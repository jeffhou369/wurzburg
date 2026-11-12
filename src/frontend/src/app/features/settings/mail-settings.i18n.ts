/**
 * 郵件與通知設定分頁的繁體中文（台灣）介面文字。
 * 沿用 auth.i18n.ts 的 `as const` 寫法。
 * 純 ASCII 的技術範例值（如 `0 9 * * 1`、`ipmgmt@example.com`）與語言無關，保留在樣板中。
 */
export const MAIL_TEXT = {
  title: 'SMTP 與郵件通知設定',
  description: '設定通知郵件發送伺服器、加密方式、系統管理員收件清單與每週未登記總表寄送排程。',
  smtpHost: {
    label: 'SMTP 伺服器主機 *',
    placeholder: '例如 smtp.example.com',
  },
  smtpPort: '埠號 (Port) *',
  tlsMode: {
    label: '連線加密方式 *',
    starttls: 'STARTTLS（預設，連接 587）',
    smtps: 'SMTPS（直連 SSL/TLS，連接 465）',
    none: '無加密（僅限內部信任網路，連接 25）',
  },
  smtpUser: {
    label: '認證帳號（若無需認證可留空）',
    placeholder: '例如 no-reply@example.com',
  },
  smtpPass: {
    label: '認證密碼',
    placeholderKeep: '已設定（保留原密碼請留空）',
    placeholderNew: '請輸入密碼',
  },
  senderAddress: '寄件人信箱 (From) *',
  weeklyCron: {
    label: '未登記總表寄送排程 (Cron) *',
    help: '預設每週一 09:00（園區時區），格式為 5 欄位標準 Cron',
  },
  adminRecipients: {
    label: '系統管理員通知收件清單（每行一個信箱）*',
    help: '未登記彙整、未登記累積總表與到期催認最終提醒將寄送至此清單。',
  },
  save: '儲存郵件設定',
  saved: '郵件設定已儲存',
  saveFailed: '儲存失敗',
  saveFailedHint: '請檢查輸入欄位',
  test: {
    title: '寄送測試信',
    description: '寄送一封驗證信件至指定信箱，確認 SMTP 連線與 HTML/純文字排版格式正常。',
    recipientPlaceholder: '請輸入測試收件人信箱',
    send: '寄出測試信',
    sending: '寄送中...',
    resultLabel: (success: boolean) => (success ? '寄送成功：' : '寄送失敗：'),
    sent: '測試信已寄出',
    failed: '寄送失敗',
    failedHint: '測試信寄送失敗，請確認 SMTP 設定與網路連線',
  },
} as const;
