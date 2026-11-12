import { SHELL_TEXT } from '../../core/i18n/shell.i18n';

/**
 * 交換器與 ARP 同步頁的繁體中文（台灣）介面文字。
 * 沿用 auth.i18n.ts 的 `as const` 寫法：含變數的字串用函式；取消與確認用語共用 SHELL_TEXT.actions。
 * Cisco 平台名稱、SNMP 版本與 Community 等技術名詞維持原文。
 */
export const SWITCHES_TEXT = {
  title: '交換器與 ARP 同步',
  subtitle: '管理 ARP 來源交換器（IOS-XE / NX-OS）、VRF 讀取設定與即時輪詢狀態',
  sync: {
    running: '同步中',
    roundIncomplete: '本輪未完成',
    now: '立即同步',
    notStarted: '尚未同步',
  },
  addSwitch: '新增交換器',
  loading: '正在載入交換器設定...',
  empty: {
    title: '尚未新增 ARP 來源交換器',
    description: '新增至少一台擔任 L3 閘道的 Cisco 交換器以開始探索網段與追蹤 IP。',
  },
  card: {
    disabled: '已停用',
    addVrf: '新增 VRF 設定',
    stopPolling: '停止輪詢',
    resumePolling: '恢復輪詢',
    deleteSwitch: '刪除交換器',
    globalVrfOnly: '僅設定預設全域 VRF，交換器上非預設 VRF 內的 ARP 位址將不會被看到。',
    credentialSet: '憑證已設定',
    success: (count: number | null | undefined) => `成功（${count ?? ''} 筆）`,
    failed: (errorClass: string | null | undefined) => `失敗：${errorClass ?? ''}`,
    testRead: '測試讀取',
    deleteVrf: '刪除 VRF 設定',
  },
  addSwitchForm: {
    title: '新增 ARP 來源交換器',
    name: '顯示名稱',
    mgmtIp: '管理 IPv4',
    platform: '作業系統平台',
    snmpVersion: 'SNMP 版本',
    v3Recommended: 'SNMPv3（生產建議）',
    v2cReadOnly: 'SNMPv2c（僅限唯讀）',
    community: '唯讀 Community',
    v3Username: 'SNMPv3 使用者名稱',
    authPassphrase: '驗證密語（Auth Passphrase）',
    privPassphrase: '加密密語（Priv Passphrase）',
    submit: '儲存交換器',
  },
  addVrfForm: {
    title: '新增非全域 VRF 讀取設定',
    name: 'VRF 顯示名稱',
    v3Context: 'SNMPv3 Context 名稱',
    community: '該 VRF 專屬唯讀 Community',
    submit: '新增 VRF',
  },
  actions: SHELL_TEXT.actions,
  toast: {
    syncTriggered: '已觸發立即同步',
    syncTriggerFailed: '無法觸發同步',
    switchCreated: '交換器新增成功',
    switchCreateFailed: '新增交換器失敗',
    vrfCreated: 'VRF 讀取設定已新增',
    vrfCreateFailed: '新增失敗',
    vrfDeleted: 'VRF 讀取設定已刪除',
    vrfDeleteFailed: '刪除失敗',
    updateFailed: '更新失敗',
    switchDeleted: '交換器已刪除',
    testReadSucceeded: '測試讀取成功',
    testReadCounts: (arpCount: number, subnetCount: number) =>
      `讀取到 ${arpCount} 筆 ARP、${subnetCount} 個介面網段`,
    testReadFailed: (errorClass: string | null | undefined) => `測試讀取失敗 (${errorClass ?? ''})`,
    testRunFailed: '測試執行失敗',
  },
  deleteDialog: {
    title: (name: string) => `確認刪除交換器「${name}」`,
    message: '刪除交換器會立即移除其名下的 ARP 觀測來源，是否確定刪除？',
    confirm: '確認刪除',
  },
} as const;
