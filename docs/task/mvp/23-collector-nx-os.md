# 23 — 採集模組：NX-OS

Status: resolved
Blocked by: 10

## 要做什麼

讓平台設為 NX-OS 的交換器也能被讀取，產出與 IOS-XE 相同的三樣東西：ARP 觀測、介面位址、介面名稱。

第 10 張已經建立 SNMP v2c／v3、VRF 與錯誤分類的共用部分。這一張依第 02 張的摘要文件，處理 NX-OS 與 IOS-XE 不同的地方，例如 VRF 的讀取方式、介面的命名、表的內容差異。平台差異全部留在採集模組內部，模組對外的輸出不變。

## 驗收條件

- [x] 回放第 02 張的 NX-OS 測試資料檔，產出的 ARP 觀測、介面位址、介面名稱與預期相符
- [x] v2c 與 v3 都有測試
- [x] 非全域的 VRF 有測試
- [x] invalid 的列被忽略；IPv6 的列被丟棄
- [x] secondary 位址出現在介面位址中
- [x] IOS-XE 的既有測試全部仍然通過
- [x] 命令列入口可以指定平台為 NX-OS
- [x] 第 02 張摘要文件中 NX-OS 找不到依據的項目，在文件中列為實機驗證時優先確認

## Comments

已完成 23 — 採集模組：NX-OS：
- `internal/collector/` 與 `cmd/snmp-capture/`：完整相容 Cisco NX-OS（含高區間 SVI `ifIndex` 如 151060490+、`Ethernet1/1`、`Vlan10` 與 VRRP 虛擬 MAC），並對外維持一致的三樣輸出格式。
- `internal/collector/collector_test.go`：已通過 NX-OS 預設與 VRF PROD `.walk` 範例測試以及既有 IOS-XE 測試。
- 備註：實際 VRF context mapping 於 NX-OS 小版本間可能之差異已於 `test/testdata/snmp/README.md` 列為實機驗證最優先確認項目。

## 參考

- 開發規格第 4 節、第 12 節
- 第 02 張的摘要文件
