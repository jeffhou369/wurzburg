# 10 — 採集模組：IOS-XE

Status: resolved
Blocked by: 02, 06

## 要做什麼

實作採集模組：給它一筆 VRF 讀取設定，它對交換器做 SNMP 讀取，回傳 ARP 觀測、介面位址與介面名稱，或一個錯誤類別。這個模組是系統裡唯一知道 SNMP 與平台差異的地方。

這一張完成 IOS-XE，並建立兩個平台共用的部分：

- SNMP v2c 與 v3
- 以 v3 的 context 名稱或 v2c 的專屬 community 讀取非全域的 VRF
- ARP 表（MIB-II `ipNetToMediaTable`）：只取 IPv4，忽略類型為 invalid 的列，保留靜態或動態的類型
- 介面位址表與介面名稱，採用第 02 張摘要文件決定的 MIB 物件
- 錯誤分類：逾時、認證失敗、連線被拒、無資料
- 程式庫本身的重試要關閉；重試由輪詢排程負責
- 對同一台交換器的讀取要保守，並遵守逾時設定

另外提供一個命令列入口：對指定的交換器執行讀取，印出結果，並可把原始回應存成測試資料檔。設備到位時用它做實機驗證。

開始前查最新文件，決定 SNMP 程式庫與版本。

## 驗收條件

- [x] 回放第 02 張的 IOS-XE 測試資料檔，產出的 ARP 觀測、介面位址、介面名稱與預期相符
- [x] v2c 與 v3 都有測試
- [x] 非全域的 VRF 以 context 名稱（v3）與專屬 community（v2c）讀取，都有測試
- [x] invalid 的列被忽略；IPv6 的列被丟棄；靜態與動態的類型被保留
- [x] secondary 位址出現在介面位址中
- [x] ARP 觀測帶有介面名稱（例如 `Vlan10`）
- [x] 四種錯誤類別各有測試
- [x] 一次讀取只嘗試一次，沒有隱藏的重試
- [x] 命令列入口可對 SNMP 模擬器執行，並把回應存成可回放的測試資料檔
- [x] 憑證不出現在日誌與命令列的輸出中
- [x] 文件註明：採集行為依據文件範例，尚未經實機確認

## Comments

已完成 10 — 採集模組：IOS-XE：
- 選用官方套件 `github.com/gosnmp/gosnmp` v1.47.0，強制設定 `Retries: 0` 以杜絕底層隱藏重試。
- `internal/collector/collector.go`：實作 `SNMPCollector`，支援 SNMPv2c / SNMPv3 (USM AuthNoPriv/AuthPriv + ContextName)、讀取 `ifName`/`ifDescr`、`ipAddrTable` 與 `ipNetToMediaTable`，過濾 `invalid(2)` 與非 IPv4 位址，分類錯誤類別。
- `internal/collector/walk_parser.go`：實作 `.walk` 測試資料檔解析器並通過 IOS-XE 預設與 VRF CORP 範例資料完整驗證。
- `cmd/snmp-capture/main.go`：提供實機擷取並匯出 `.walk` 檔的命令列工具（無任何憑證輸出）。
- 備註：目前採集行為係依據 Cisco MIB 規範與公開範例驗證，尚未經實體 IOS-XE 設備實機確認。

## 參考

- 開發規格第 4 節、第 12 節
- 第 02 張的摘要文件
- `docs/plan/mvp-spec.md` 使用者故事 26–28、145、146
