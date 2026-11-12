/**
 * IP 紀錄狀態用語（在線、登記、衝突三個獨立面向共用的狀態文字）。
 * 狀態不得只靠顏色表達，所有狀態膠囊與選單都要使用這裡的文字。
 */
export const STATUS_TEXT = {
  online: '在線',
  offline: '離線',
  unregistered: '未登記',
  registered: '已登記',
  expiring: '即將到期',
  expired: '已到期',
  conflict: '衝突',
} as const;
