/**
 * MAC 衝突頁的繁體中文（台灣）介面文字。
 * 沿用 auth.i18n.ts 的 `as const` 寫法；需要帶入變數的字串以函式表示。
 */
export const CONFLICTS_TEXT = {
  title: 'MAC 衝突告警',
  subtitle: '檢視並裁決同一位址出現多個 MAC 或已登記位址變更 MAC 之情形',
  empty: {
    title: '目前無衝突紀錄',
    description: '所有納管網段的 IP 與 MAC 對應關係皆一致。',
  },
  columns: {
    status: '狀態',
    ipAddress: 'IP 位址',
    adoptedMac: '採信 MAC',
    observedMacs: '觀測到的 MAC',
    owner: '擁有者',
    lastDetected: '最近發現',
  },
  status: {
    open: '未處理',
    inProgress: '處理中',
    resolved: '已解消',
  },
  unregistered: '未登記',
  sheet: {
    title: (ipAddress: string) => `衝突裁決：${ipAddress}`,
    fallbackTitle: '衝突詳細資料',
    adoptedMac: '採信 MAC：',
    owner: '擁有者：',
    ownerCoexist: '未登記（並存衝突）',
    sources: '觀測來源明細',
  },
  resolve: {
    title: '裁決處置',
    adoptNewKeepReg: '採信新的 MAC，保留既有登記（擁有者更換設備）',
    adoptNewReleaseReg: '採信新的 MAC，並釋放登記（位址已轉由他人使用）',
    adoptNew: '採信新的 MAC',
    keepAdopted: '維持原本的 MAC（進入處理中，等待錯誤裝置撤除）',
    pickMac: '選擇採信之 MAC',
    note: '處置說明（選填）',
    submit: '套用裁決',
    applied: '衝突裁決已套用',
    failed: '裁決失敗',
  },
} as const;
