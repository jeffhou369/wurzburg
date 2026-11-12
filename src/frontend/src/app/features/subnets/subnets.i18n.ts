import { SHELL_TEXT } from '../../core/i18n/shell.i18n';

/**
 * 網段管理頁的繁體中文（台灣）介面文字。
 * 沿用 auth.i18n.ts 的 `as const` 寫法：含變數的字串用函式；取消按鈕共用 SHELL_TEXT.actions。
 */
export const SUBNETS_TEXT = {
  title: '網段管理',
  subtitle: '檢視並決定發現網段的納管狀態與排除範圍（DHCP / 無線 / 其他）',
  addManual: '手動新增網段',
  filters: {
    all: '全部',
    pending: (count: number) => `待檢視 (${count})`,
    managed: '納管',
    unmanaged: '不納管',
  },
  empty: {
    title: '尚無網段資料',
    description: '待交換器讀取介面位址後自動出現，或由系統管理員手動新增。',
  },
  state: {
    pending: '待檢視',
    managed: '納管',
    unmanaged: (reason: string | null | undefined) => `不納管 (${reason ?? ''})`,
  },
  card: {
    sourceMissing: '來源已消失',
    recorded: (count: number) => `已記錄 ${count} 筆`,
    online: (count: number) => `在線 ${count} 筆`,
  },
  undetermined: {
    title: (count: number) => `未定前綴清單（${count}）`,
    description: '下列 ARP 觀測的位址不落在任何已知網段內，請確認並手動建立對應 CIDR。',
    columns: {
      ipAddress: 'IP 位址',
      macAddress: 'MAC 位址',
      switchName: '交換器',
      vrf: 'VRF',
      interface: '介面',
      lastSeen: '最後見到',
    },
  },
  detail: {
    title: '網段詳情',
    titleWithCidr: (cidr: string) => `網段詳情：${cidr}`,
    stateHeading: '網段納管狀態',
    setManaged: '設為納管',
    setUnmanaged: '設為不納管',
    reasons: {
      dhcp: 'DHCP 網段',
      wireless: '無線網段',
      other: '其他原因',
    },
    gatewayHeading: '閘道位址（不建立 IP 紀錄）',
    noGateways: '無交換器介面位址紀錄。',
    exclusionHeading: '排除範圍（DHCP / 無線 / 其他）',
    removeExclusion: '移除排除範圍',
    exclusionStart: '起始 IP',
    exclusionEnd: '結束 IP',
    exclusionReasons: {
      dhcp: 'DHCP',
      wireless: '無線',
      other: '其他',
    },
    addExclusion: '新增排除',
  },
  manual: {
    title: '手動新增網段',
    description: '網段說明',
    submit: '建立並設為納管',
  },
  actions: SHELL_TEXT.actions,
  toast: {
    created: '網段新增完成',
    createFailed: '新增失敗',
    setManaged: '網段已設為納管',
    setUnmanaged: '網段已設為不納管',
    changeFailed: '變更失敗',
    exclusionAdded: '排除範圍已新增',
    exclusionAddFailed: '新增排除失敗',
    exclusionInvalid: '排除範圍格式錯誤',
    exclusionRemoved: '已移除排除範圍',
  },
  unmanageDialog: {
    title: '確認將網段改為不納管',
    message: (total: number, owned: number) =>
      `此變更將直接刪除 ${total} 筆 IP 紀錄（其中 ${owned} 筆已有擁有者）且無法復原。確認執行？`,
    confirm: '確認刪除並設為不納管',
  },
  exclusionDialog: {
    title: '確認新增排除範圍',
    message: (total: number, owned: number) =>
      `此排除範圍內共有 ${total} 筆現有 IP 紀錄（其中 ${owned} 筆已登記），確認後將直接刪除且無法復原。是否繼續？`,
    confirm: '確認刪除並新增排除',
  },
} as const;
