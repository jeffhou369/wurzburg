# Würzburg 邏輯資料模型與限制條件說明

本文件說明 MariaDB 資料庫之完整結構設計，並統一採用 `CONTEXT.md` 定義之領域名詞。

## 資料表總覽

| 資料表 | 用途 | 關鍵限制條件與規則 |
| --- | --- | --- |
| `switches` | ARP 來源交換器端點設定 | `mgmt_ip` 唯一；平台限 `ios-xe` / `nx-os` |
| `vrf_read_configs` | 交換器底下之 VRF 讀取設定 | `(switch_id, vrf_name)` 唯一；每台至少有一筆預設 `default` |
| `poll_runs` | 單次輪詢讀取執行紀錄 | 記錄起訖時間（UTC）、重試與錯誤類別 |
| `subnets` | IPv4 網段 | `cidr` 唯一；單一狀態 `pending`（待檢視）／`managed`（納管）／`unmanaged`（不納管）；`CHECK` 限制 `unmanaged` 必帶原因 |
| `subnet_gateways` | 閘道位址（交換器 L3 介面 IP） | `(subnet_id, ip_address)` 唯一；不建立 IP 紀錄 |
| `exclusion_ranges` | 納管網段下之排除範圍 | 連續起訖位址與排除原因（`dhcp` / `wireless` / `other`） |
| `users` | 系統管理員與一般使用者帳號 | `(auth_source, username)` 唯一；區分 `admin` 與 `user` |
| `ip_records` | 納管範圍內之 IP 紀錄 | `ip_address` 全域唯一（ADR 0009）；擁有者為 `owner_user_id`（ADR 0012），`owner_mail` 僅為聯絡欄位 |
| `arp_sightings` | 目前有效之 ARP 觀測 | `(ip_record_id, vrf_config_id)` 唯一；記錄目前各來源之 MAC、介面名稱與未見輪數 |
| `ip_events` | IP 變更事件歷程 | 記錄 `first_seen`、`online`、`offline`、`mac_changed` |
| `ip_conflicts` | MAC 衝突紀錄 | 透過產生欄位 `active_ip_record_id` 與唯一鍵 `uq_active_conflict_per_ip` 保證每筆 IP 紀錄最多只有一筆未解消衝突；`other_mac_absent_rounds` 記錄處理中衝突的其他 MAC 連續未被回報的輪數，達離線判定輪數才自動解消 |
| `undetermined_prefixes` | 未定前綴清單 | `ip_address` 唯一；暫存不屬於任何已知網段的 ARP 觀測 |
| `sessions` | 伺服端工作階段 | `token_hash` 唯一；記錄絕對逾時與閒置逾時（UTC） |
| `oui_entries` | 本地 OUI 廠商前綴查詢表 | `(prefix, prefix_length)` 唯一；支援 24/28/36 位元長度 |
| `oui_imports` | 本地 OUI 資料匯入歷史 | 記錄檔名、版本、匯入者與筆數 |
| `notification_logs` | SMTP 郵件寄送紀錄 | 記錄寄送狀態、嘗試次數、重試錯誤原因與內容快照 |
| `notification_states` | 通知去重狀態表 | `dedup_key` 唯一；成功寄出後方標為已通知 |
| `audit_logs` | 系統操作稽核紀錄 | 記錄操作者、來源 IP、對象、操作及脫敏後明細 JSON |
| `settings` | 園區參數與加密後機敏設定 | `setting_key` 主鍵；以 `is_secret` 區分加密保護之設定值 |

## 時間基準

所有資料表之時間欄位一律以 **UTC** 儲存，前端呈現與排程計算時再轉換至園區設定之時區（預設 `Asia/Taipei`）。
