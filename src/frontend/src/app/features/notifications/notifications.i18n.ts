/**
 * 通知紀錄頁的繁體中文（台灣）介面文字。
 * 沿用 auth.i18n.ts 的 `as const` 寫法；需要帶入變數的字串以函式表示。
 * 分頁按鈕文字共用 SHELL_TEXT.pagination。
 */
export const NOTIFICATIONS_TEXT = {
  title: '通知紀錄',
  subtitle: '檢視未登記彙整、每週總表與到期催認之 SMTP 寄送結果與去重狀態',
  tabs: {
    logs: '寄送紀錄',
    states: '去重通知狀態',
  },
  logs: {
    statusFilter: '狀態篩選：',
    statusOptions: {
      all: '全部狀態',
      sent: '成功寄出 (sent)',
      failed: '寄送失敗 (failed)',
      pending: '待處理 (pending)',
    },
    loading: '正在載入通知寄送紀錄...',
    empty: {
      title: '尚無通知寄送紀錄',
      description: '目前無符合篩選條件的通知紀錄。',
    },
    columns: {
      status: '狀態',
      type: '種類',
      recipient: '收件人',
      subject: '主旨',
      attempts: '嘗試次數',
      lastError: '失敗原因',
      time: '寄送 / 建立時間',
    },
    status: {
      sent: '成功',
      failed: '失敗',
      pending: '處理中',
    },
  },
  states: {
    loading: '正在載入通知去重狀態...',
    empty: {
      title: '尚無已通知狀態',
      description: '當未登記在線彙整或到期催認寄送後，其去重追蹤狀態將顯示於此處。',
    },
    columns: {
      dedupKey: '去重鍵 (Dedup Key)',
      type: '通知種類',
      notifiedAt: '首次通知時間',
      updatedAt: '最後更新',
      actions: '操作',
    },
    reset: '重設狀態',
    resetting: '重設中...',
    resetDone: '已重設去重狀態',
    resetDoneDetail: (dedupKey: string) => `${dedupKey} 下次將重新納入通知`,
    resetFailed: '重設失敗',
    resetFailedHint: '無法重設狀態',
  },
  pageSummary: (page: number, totalPages: number, totalItems: number) =>
    `第 ${page} / ${totalPages} 頁（共 ${totalItems} 筆）`,
  types: {
    unregistered_digest: '未登記彙整',
    unregistered_weekly: '未登記總表',
    expiry_reminder: '到期催認',
    test_mail: '測試信',
  } as Record<string, string>,
} as const;
