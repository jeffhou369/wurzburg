/**
 * 稽核紀錄頁的繁體中文（台灣）介面文字。
 * 沿用 auth.i18n.ts 的 `as const` 寫法；分頁列文字共用 SHELL_TEXT.pagination。
 */
export const AUDIT_TEXT = {
  title: '稽核紀錄',
  subtitle: '追查所有登入、登記變更、網段調整、刪除明細與系統設定歷史',
  loading: '正在載入稽核紀錄...',
  empty: {
    title: '尚無稽核紀錄',
    description: '目前尚未記錄任何操作事件。',
  },
  columns: {
    time: '時間',
    actor: '操作者',
    sourceIp: '來源 IP',
    action: '動作',
    target: '對象',
    summary: '變更摘要',
    details: '明細',
  },
  collapse: '收合',
  expand: '展開',
} as const;
