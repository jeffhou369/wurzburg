/**
 * 外殼與基礎元件之繁體中文（台灣）集中介面文字
 */
export const SHELL_TEXT = {
  appName: 'Würzburg',
  appSubtitle: 'IPv4 位址管理系統',
  nav: {
    workbench: '首頁工作臺',
    ipAddresses: 'IP 位址',
    subnets: '網段',
    switches: '交換器',
    conflicts: '衝突',
    notifications: '通知紀錄',
    audit: '稽核紀錄',
    settings: '系統設定',
    showcase: '元件展示',
  },
  theme: {
    light: '淺色模式',
    dark: '深色模式',
    toggleLabel: '切換外觀主題',
  },
  user: {
    adminBadge: '系統管理員',
    userBadge: '一般使用者',
    logout: '登出',
  },
  actions: {
    close: '關閉',
    cancel: '取消',
    confirm: '確認',
    saving: '儲存中...',
  },
  /** 列表分頁列（各頁共用） */
  pagination: {
    previous: '上一頁',
    next: '下一頁',
    summary: (totalItems: number, page: number, totalPages: number) =>
      `共 ${totalItems} 筆（第 ${page} / ${totalPages} 頁）`,
  },
  /** 瀏覽器分頁標題；帶入園區名稱 */
  documentTitle: (campusName: string) => `[${campusName}] IP 管理系統`,
  /** 純圖示按鈕與徽章的無障礙名稱（aria-label） */
  aria: {
    toggleSidebar: '切換導覽側欄',
    mainNav: '主導覽',
    pendingSubnetsCount: '待檢視網段數量',
    openConflictsCount: '未處理衝突數量',
    closeToast: '關閉通知',
    closeSheet: '關閉視窗',
  },
  /** IP 紀錄右側抽屜 */
  ipRecordSheet: {
    fallbackTitle: 'IP 紀錄詳情',
    tabRegistration: '登記',
    tabObservations: '觀測與歷程',
    registrationPlaceholder: '此處預留給「登記」功能插槽（Ticket 18）。',
    observationsPlaceholder: '此處預留給「觀測與歷程」功能插槽（Ticket 13）。',
  },
} as const;
