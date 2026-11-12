# SNMP 測試資料與 Cisco 交換器回應範例總結

本目錄存放依據原廠規格與公開 MIB 定義整理之 Cisco IOS-XE 與 NX-OS 回應範例資料，作為自動化測試與 SNMP 模擬器之回放資料。

## 1. 檔案清單

| 目錄 / 檔案 | 平台 | 涵蓋情境 | 狀態 |
| --- | --- | --- | --- |
| `ios-xe/default-vrf.walk` | Cisco IOS-XE (Catalyst 9000 等) | 全域 VRF，包含正常動態 ARP、失效列（invalid）、靜態 ARP、HSRP 虛擬 MAC、次要位址（Secondary IP）、未定前綴主機 | 推定（待實機驗證） |
| `ios-xe/vrf-corp.walk` | Cisco IOS-XE (Catalyst 9000 等) | 非預設 VRF（CORP），驗證 SNMPv3 contextName 或 SNMPv2c community 對應 | 推定（優先實機驗證） |
| `nx-os/default-vrf.walk` | Cisco NX-OS (Nexus 9000 等) | 全域 VRF，包含 Nexus 特有之大編號 ifIndex（0x09000000+）、VRRP 虛擬 MAC 與動態 ARP | 推定（待實機驗證） |
| `nx-os/vrf-prod.walk` | Cisco NX-OS (Nexus 9000 等) | 非預設 VRF（PROD），驗證 Nexus context 對應 | 推定（優先實機驗證） |

---

## 2. 建議採用之 MIB 物件與理由

依據開發規格第 4 節，採集模組每一輪向交換器讀取三樣資料：

### (1) ARP 表：`RFC1213-MIB::ipNetToMediaTable` (`1.3.6.1.2.1.4.22`)
- **推薦物件**：
  - `ipNetToMediaIfIndex` (`.1`)：介面索引
  - `ipNetToMediaPhysAddress` (`.2`)：實體位址（MAC）
  - `ipNetToMediaNetAddress` (`.3`)：IPv4 位址
  - `ipNetToMediaType` (`.4`)：類型（1=other, 2=invalid, 3=dynamic, 4=static）
- **理由**：
  - 專案依 ADR-001 明確限定為 IPv4，不納入 IPv6。`ipNetToMediaTable` 普遍支援於所有 Cisco IOS-XE 與 NX-OS 平台，結構緊湊穩定。
  - 採集時必須主動忽略 `ipNetToMediaType == 2 (invalid)` 的項目，以過濾交換器快取中已失效的條目。

### (2) 介面位址表：`RFC1213-MIB::ipAddrTable` (`1.3.6.1.2.1.4.20`)
- **推薦物件**：
  - `ipAdEntAddr` (`.1`)：介面 IP 位址
  - `ipAdEntIfIndex` (`.2`)：對應之介面索引
  - `ipAdEntNetMask` (`.3`)：子網路遮罩
- **理由**：
  - 直接一對一提供 IP 與遮罩（NetMask），計算網段 CIDR 極為便利且效能最高。
  - 雖然較新之 `IP-MIB`（RFC 4293）提供 `ipAddressTable`，但其遮罩需額外比對 `ipAddressPrefixTable`，查詢複雜度倍增。在實機驗證確認支援性前，優先以 `ipAddrTable` 為主。
  - 次要位址（Secondary IP）在 Cisco 交換器上通常以同一個 `ifIndex` 但不同 IP 呈現在 `ipAddrTable` 中，採集器可正確彙整至同介面。

### (3) 介面名稱：`IF-MIB::ifName` (`1.3.6.1.2.1.31.1.1.1.1`)
- **推薦物件**：`ifName`
- **備援物件**：`IF-MIB::ifDescr` (`1.3.6.1.2.1.2.2.1.2`)
- **理由**：
  - `ifName` 回傳標準簡短名稱（如 `Vlan10`、`GigabitEthernet1/0/1`、`Ethernet1/1`），最適合呈現於網管操作介面。
  - `ifDescr` 在部分平台可能包含冗長的原廠描述字串，若 `ifName` 無資料時再回退採用 `ifDescr`。

---

## 3. Cisco 平台 ARP 逾時預設值與出處

在系統中「在線」意指交換器 ARP 表中尚存該條目。因此實體裝置關機離線後，系統變更為離線狀態之延遲主要取決於交換器原廠 ARP 逾時時間：

| 平台 | 預設 ARP 逾時 | 出處與依據 | 建議作為 |
| --- | --- | --- | --- |
| **Cisco IOS-XE** | **4 小時（14,400 秒）** | Cisco IOS-XE IP Addressing Configuration Guide: `arp timeout` default is 14400 seconds. | 建議網管視園區管理需求，將主要使用者 VLAN 之 `arp timeout` 調短至 300 ~ 600 秒（5 ~ 10 分鐘）。 |
| **Cisco NX-OS** | **25 分鐘（1,500 秒）** | Cisco Nexus 9000 Series NX-OS IP Addresses and Services Configuration Guide: default ARP aging time is 1500 seconds. | 預設值較適中，可維持預設或按需調降。 |

---

## 4. 實機驗證清單對照（開發規格第 12 節）

| 驗證項目 | 目前測試範例依據 | 實機確認重點與風險評估 |
| --- | --- | --- |
| **1. 雙平台 ARP 表格式** | RFC 1213 規範格式 | 確認 MAC 格式是否為標準 6 位元組 Hex-STRING，確認 invalid(2) 條目是否隨逾時自動清空。 |
| **2. VRF 對應機制** | 原廠 context 配置指南 | **最高優先度確認**：<br>1. IOS-XE 是否針對每個 VRF 正確響應 `snmp-server context <name>`。<br>2. SNMPv2c 之 community@context 或 community mapping 語法在各小版本間是否有差異。 |
| **3. 介面位址與 Secondary IP** | RFC 1213 ipAddrTable | 確認次要位址（secondary IP）是否確實出現在 `ipAddrTable`，若特定平台未顯示則需擴充支援 RFC 4293。 |
| **4. 自身閘道與虛擬 MAC** | 原廠 HSRP / VRRP 規格 | 確認自身 SVI IP 是否固定為 `static(4)`，確認 HSRP (`00:00:0c:07:ac:xx`) 與 VRRP (`00:00:5e:00:01:xx`) 是否在 ARP 表中可被辨識。 |
| **5. ARP 逾時實測** | 原廠手冊記載值 | 實測拔線／關機後多久自交換器 ARP 表消失。 |
| **6. 介面索引與名稱對照** | IF-MIB 規範 | 驗證 SVI（VLAN 介面）之 ifIndex 在重開機後是否保持一致。 |
| **7. 大量讀取效能** | 預期設計目標（每輪 3 分鐘） | 實機測試包含 5,000 ~ 20,000 筆 ARP 之 walk 耗時，確認不會對交換器 CPU 產生過度負載。 |
