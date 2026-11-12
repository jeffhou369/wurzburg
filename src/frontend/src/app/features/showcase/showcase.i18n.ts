/**
 * 元件展示頁（設計系統驗證用）的繁體中文（台灣）介面文字。
 * 沿用 auth.i18n.ts 的 `as const` 寫法；狀態用語共用 STATUS_TEXT，角色名稱共用 SHELL_TEXT.user。
 */
export const SHOWCASE_TEXT = {
  title: '設計系統與基礎元件展示',
  subtitle: '驗證 EasyCLIProxyAPI 淺色主題 Token、無障礙對比、狀態膠囊、表格灰階與 CDK 浮層',
  buttons: {
    title: '1. 按鈕規範（Buttons）',
    primary: '主要操作（新增登記）',
    secondary: '次要操作（立即同步）',
    ghost: '幽靈按鈕（檢視明細）',
    dangerSoft: '柔和危險（停止同步）',
    dangerSolid: '實心危險（刪除確認）',
  },
  pills: {
    title: '2. 狀態燈號與膠囊（不可僅靠顏色）',
  },
  interactions: {
    title: '3. 互動測試（CDK 浮層、Toast、角色與側欄徽章）',
    openSheet: '開啟右側 IP Sheet',
    successToast: '觸發成功 Toast',
    successToastTitle: '同步完成',
    successToastDetail: '已成功自 core-sw-01 讀取 1,240 筆 ARP 紀錄',
    conflictToast: '觸發衝突 Toast',
    conflictToastTitle: '偵測到新 MAC 衝突',
    conflictToastDetail: '10.10.10.50 同時出現兩個不同的 MAC 位址',
    toggleRole: (roleLabel: string) => `切換檢視身分（目前：${roleLabel}）`,
    toggleBadges: '切換側欄提示計數',
  },
  table: {
    title: '4. 表格外框與離線灰階樣式',
    columns: {
      status: '狀態',
      ipAddress: 'IP 位址',
      macAddress: 'MAC 位址',
      subnet: '網段',
      owner: '擁有者',
      actions: '動作',
    },
    copyIp: '複製 IP',
  },
  empty: {
    title: '5. 空狀態（Empty State）',
    emptyTitle: '沒有待處理的未登記 IP',
    emptyDescription: '所有納管網段內在線裝置皆已完成負責人登記。',
  },
  sheet: {
    registration: '這裡是預留的登記表單插槽（展示狀態）。',
    observations: '這裡是預留的觀測與歷程插槽（展示狀態）。',
  },
  deleteDialog: {
    title: '確認將網段改為不納管',
    message: '此變更將直接刪除該網段下 42 筆 IP 紀錄（其中 12 筆已登記），且無法復原。是否繼續？',
    confirm: '確認刪除',
    toastTitle: '已確認刪除操作',
    toastDetail: '明細已寫入稽核紀錄',
  },
} as const;
