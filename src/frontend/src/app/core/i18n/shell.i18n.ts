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
  },
} as const;
