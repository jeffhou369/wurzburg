# 02 — 蒐集兩個平台的 SNMP 回應範例

Status: resolved
Blocked by: 無，可以立即開始

## 要做什麼

我們沒有實體交換器可以先驗證，採集模組要依據文件與公開範例開發。這張工作項目負責把那些範例找齊，整理成自動化測試能回放的測試資料。

從 Cisco 原廠文件、MIB 定義與公開的實際輸出，蒐集 IOS-XE 與 NX-OS 對下列查詢的回應：

- ARP 表（MIB-II `ipNetToMediaTable`）
- 介面位址表（位址與遮罩，含 secondary 位址）
- 介面名稱

並盡可能涵蓋：SNMP v2c 與 v3、以 community 或 context 讀取非全域的 VRF、交換器自己的介面位址與 HSRP／VRRP 虛擬位址在 ARP 表中的樣子、ARP 類型為 invalid 的列、認證失敗與逾時等錯誤情況。

## 驗收條件

- [x] 倉庫裡有 IOS-XE 與 NX-OS 的測試資料檔，格式可由 SNMP 模擬器回放
- [x] 每一份測試資料檔都註明來源網址、平台與版本；自行依 MIB 定義組出來的要明確標示為「推定」
- [x] 有一份摘要文件，逐項對照開發規格第 12 節的實機驗證清單，說明每一項找到了什麼依據、哪些找不到
- [x] 摘要文件決定介面位址表與介面名稱實際採用的 MIB 物件，並說明理由
- [x] 摘要文件記錄兩個平台的 ARP 逾時預設值與出處
- [x] 找不到依據的項目（預期 VRF 相關的最多）列成清單，供實機驗證時優先確認

## Comments

已完成 02 — 蒐集兩個平台的 SNMP 回應範例：
- `test/testdata/snmp/ios-xe/default-vrf.walk`：包含普通動態 ARP、HSRP v1/v2 虛擬 MAC、靜態 ARP、invalid(2) 失效條目、Secondary IP 與未定前綴情境。
- `test/testdata/snmp/ios-xe/vrf-corp.walk`：包含 IOS-XE VRF context 讀取範例。
- `test/testdata/snmp/nx-os/default-vrf.walk`：包含 Nexus 平台特有之大編號 ifIndex（151060481+）、VRRP 虛擬 MAC 與動態 ARP。
- `test/testdata/snmp/nx-os/vrf-prod.walk`：包含 NX-OS VRF context 讀取範例。
- `test/testdata/snmp/README.md`：詳盡總結 MIB 物件選型（`ipNetToMediaTable`、`ipAddrTable`、`ifName`/`ifDescr`）、Cisco 雙平台預設 ARP 逾時出處（IOS-XE 14400s、NX-OS 1500s）與實機驗證清單對照。

## 參考

- 開發規格第 4 節（採集）、第 12 節（實機驗證清單）
- ADR 0011
