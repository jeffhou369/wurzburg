# Würzburg 安裝與維運手冊

本文件專為沒有參與系統開發的安裝人員與系統管理員撰寫，說明如何於單一園區部署、設定與長期維運 Würzburg IPv4 管理系統。

> [!WARNING]
> **資料安全與備份重要聲明**
> - 本系統**不內建自動備份與還原功能**。
> - 刪除 Docker Compose 所管理的具名 volume（`wurzburg-db-data`）將會**永久遺失所有 IP 紀錄、登記資料與稽核紀錄**。
> - 若遺失加密金鑰（`WURZBURG_SECRET_KEY`），資料庫內的 SNMP 憑證、AD 查詢密碼、RADIUS 共用金鑰與 SMTP 密碼將無法解密，必須透過網頁介面**重新輸入所有機敏憑證**。

---

## 1. 部署前提條件

在進行安裝步驟之前，請先確認您的園區網路環境符合下列前提條件：

1. **ARP 來源交換器與 L3 閘道位置**
   - 欲追蹤之各網段的 L3 閘道必須位於 **Cisco IOS-XE** 或 **Cisco NX-OS** 交換器上。
   - 若某個網段的 L3 閘道設在防火牆或非 Cisco 路由器上，本系統無法從交換器取得該網段的 ARP table，因此無法追蹤該網段的位址。
2. **園區 IPv4 位址空間**
   - 單一園區內的 IPv4 位址不得重疊（包含不同 VRF 之間）。
   - 一個園區獨立安裝一套 Würzburg，彼此之間不共用資料。
3. **交換器端唯讀 SNMP 與 VRF 對應設定**
   - 擔任 ARP 來源交換器的設備必須已開啟唯讀 SNMP（支援 SNMPv2c 或 SNMPv3，建議採用 SNMPv3 AuthPriv）。
   - **有使用 VRF 時，交換器端必須先替每個 VRF 設定好 SNMP context 對應**，系統才能依 VRF 讀取設定分別讀取不同 VRF 的 ARP table：

   **Cisco IOS-XE（範例設定）：**
   ```ios
   ! 1. 為指定 VRF 建立 SNMP context（假設 VRF 名稱為 CORP）
   snmp-server context vrf-corp vrf CORP
   ! 2. 若使用 SNMPv3，將群組綁定對應 context（或透過 MIB view 控制）
   snmp-server group READ_ONLY v3 priv context vrf-corp read V1DEFAULT
   ! 3. 若使用 SNMPv2c，設定 community 對應至 VRF context
   snmp-server community YOUR_RO_COMMUNITY@vrf-corp RO
   ```

   **Cisco NX-OS（範例設定）：**
   ```ios
   ! 1. 在 NX-OS 上為目標 VRF 綁定 SNMP context
   snmp-server context vrf-prod vrf PROD
   ! 2. 若使用 SNMPv2c，設定 community 與 VRF context 對應
   snmp-server community YOUR_RO_COMMUNITY group network-operator
   snmp-server mib community-map YOUR_RO_COMMUNITY@vrf-prod context vrf-prod
   ```

---

## 2. 安裝步驟

### 2.1 所需軟體

- Linux 主機（建議 Ubuntu 22.04+ / RHEL 9+ / Debian 12+）或可執行容器之伺服器
- **Docker Engine** 24.0 以上版本
- **Docker Compose** v2.20 以上版本（`docker compose` 指令）

### 2.2 取得程式碼與設定環境變數

1. 取得儲存庫並進入 `infra/` 目錄：
   ```sh
   git clone <儲存庫網址> wurzburg
   cd wurzburg/infra
   cp .env.example .env
   ```

2. 產生一組隨機加密金鑰（供 `WURZBURG_SECRET_KEY` 使用）：
   ```sh
   openssl rand -hex 32
   ```

3. 編輯 `infra/.env`，依照環境需求填入以下環境變數：

| 環境變數名稱 | 說明 | 是否必填 | 預設值 |
| --- | --- | --- | --- |
| `WURZBURG_SECRET_KEY` | 用於 AES-256-GCM 加密儲存資料庫內 SNMP/AD/RADIUS/SMTP 機敏密碼之金鑰（建議 32 位元組以上或 64 字元 Hex） | **必填** | 無（空字串將無法儲存任何 Secret） |
| `WURZBURG_ADMIN_USERNAME` | 首次啟動時自動建立的第一位系統管理員帳號（資料庫已有系統管理員後此變數即不再生效） | 首次必填 | 無 |
| `WURZBURG_ADMIN_PASSWORD` | 第一位系統管理員之初始密碼（須至少 12 個字元；**首次登入改完密碼後請自 `.env` 移除**） | 首次必填 | 無 |
| `WURZBURG_DB_NAME` | MariaDB 資料庫名稱 | 選填 | `wurzburg` |
| `WURZBURG_DB_USER` | MariaDB 應用程式連線帳號 | 選填 | `wurzburg` |
| `WURZBURG_DB_PASSWORD` | MariaDB 應用程式連線密碼 | **建議修改** | `wurzburg_dev_password` |
| `WURZBURG_DB_ROOT_PASSWORD` | MariaDB root 帳號密碼 | **建議修改** | `wurzburg_root_dev_password` |
| `WURZBURG_HTTP_PORT` | 主機對外提供 Web 與 API 之 HTTP 連接埠 | 選填 | `8080` |
| `WURZBURG_CAMPUS_NAME` | 園區顯示名稱（亦可在設定精靈或設定頁動態修改） | 選填 | `Würzburg` |
| `WURZBURG_TIMEZONE` | 園區顯示與排程時區（IANA 格式，資料庫一律存 UTC） | 選填 | `Asia/Taipei` |
| `WURZBURG_ENV` | 執行環境模式（`production` 或 `development`） | 選填 | `production` |

### 2.3 建置與啟動容器

請回到儲存庫根目錄執行建置並啟動服務（注意：`infra/docker-compose.yml` 已設定 `name: wurzburg` 確保隔離性）：

```sh
cd ..
docker build -t wurzburg:dev .
docker compose -f infra/docker-compose.yml --env-file infra/.env up -d --no-build
```

確認服務狀態皆為健康（`healthy` / `running`）：

```sh
docker compose -f infra/docker-compose.yml ps
```

---

## 3. 首次登入與設定精靈

1. 開啟瀏覽器前往 `http://<主機IP>:8080/`。
2. 於登入頁面選擇「**系統管理員**」頁籤，輸入 `.env` 所設定的 `WURZBURG_ADMIN_USERNAME` 與初始密碼。
3. 系統會強制要求修改密碼，請輸入至少 12 個字元的新密碼並送出。
4. **重要安全性動作**：改完密碼後，請立即開啟伺服器上的 `infra/.env`，將 `WURZBURG_ADMIN_USERNAME` 與 `WURZBURG_ADMIN_PASSWORD` 移除或清空。
5. 改完密碼後會自動開啟五步驟**初始設定精靈**（可隨時按「稍後再設定」跳過並於首頁「尚未完成的設定」清單繼續）：
   - **步驟 1 — 園區資訊**：設定顯示名稱與時區。
   - **步驟 2 — 驗證來源**：準備 AD (LDAPS 網址、查詢帳號、Base DN、允許群組) 或 RADIUS (伺服器 IP、連接埠、Shared Secret) 資料並執行測試登入。
   - **步驟 3 — 郵件**：準備 SMTP 主機、連接埠、加密方式、寄件人信箱與系統管理員通知清單，並寄出測試信。
   - **步驟 4 — 第一台交換器**：準備 ARP 來源交換器管理 IP、作業平台（IOS-XE/NX-OS）、SNMP 版本與憑證，新增後點擊「測試讀取」。
   - **步驟 5 — 檢視網段**：系統會觸發同步探索交換器介面上的網段，請將需要追蹤的網段設為「納管」，動態位址或訪客區設為「不納管」（並註記原因）。

---

## 4. 啟用 HTTPS / TLS 傳輸加密

> [!CAUTION]
> 在尚未啟用 HTTPS 之前，系統僅限於**完全受信任的內部隔離網段**中使用。以 HTTP 明文連線時，帳號密碼與工作階段 Cookie 皆有遭網路竊聽之風險（系統管理員首頁「尚未完成的設定」亦會持續提醒「尚未啟用 HTTPS」）。

請將組織核發之憑證與私鑰放置於 `infra/certs/`（`wurzburg.crt` 與 `wurzburg.key`），並搭配選用的 Nginx Compose 疊加設定啟動：

```sh
docker compose -f infra/docker-compose.yml -f infra/docker-compose.https.yml --env-file infra/.env up -d
```

詳細憑證製作與埠號調整參數請見 [infra/HTTPS-README.md](HTTPS-README.md)。

---

## 5. 「在線」的意義與交換器 ARP 逾時

在 Würzburg 中，IP 紀錄狀態為「**在線**」代表至少一台 ARP 來源交換器目前的 ARP table 中仍包含該 IP 位址。**「在線」不保證裝置此刻在實體層可回應 ping**；當實體設備關機、拔除網路線或離開園區後，交換器必須等待內部的 ARP 條目逾時老化移除，且 Würzburg 連續 2 輪（約 6 分鐘）未觀測到該位址，才會將其判定為「**離線**」。

各平台預設 ARP 逾時時間如下：

| 平台 | 預設 ARP 逾時 | 建議做法 |
| --- | --- | --- |
| **Cisco IOS-XE** | **4 小時（14,400 秒）** | 預設時間較長；若希望更快偵測裝置離線，建議於各使用者 VLAN 介面 (`interface VlanX`) 調整：`arp timeout 300`（5 分鐘）或 `600`（10 分鐘）。 |
| **Cisco NX-OS** | **25 分鐘（1,500 秒）** | 預設較為適中，通常可維持預設；若有更即時的需求，可於介面下調整 `ip arp timeout 300`。 |

---

## 6. 資料與備份

> [!WARNING]
> **請務必建立外部營運備份機制**
> 1. Würzburg **不內建備份或還原功能**。
> 2. 所有 IP 紀錄、登記資料、系統設定與稽核紀錄皆存放在 MariaDB 的具名 volume `wurzburg-db-data` 中。執行 `docker compose down -v` 或手動刪除 volume 將導致**資料永久遺失**。
> 3. 請連同 `infra/.env` 中的 `WURZBURG_SECRET_KEY` 一起妥善異地備份。

以下提供營運側自行進行邏輯資料庫備份與還原的範例指令（此為營運維護建議，非產品內建功能保證）：

### 6.1 匯出資料庫備份（`mariadb-dump`）

```sh
# 將資料庫完整匯出為 SQL 備份檔
docker compose -f infra/docker-compose.yml exec -T db \
  mariadb-dump -u wurzburg -p"$WURZBURG_DB_PASSWORD" --single-transaction --routines --triggers wurzburg \
  > wurzburg_backup_$(date +%Y%m%d_%H%M%S).sql
```

### 6.2 從備份檔還原資料庫

```sh
# 1. 暫停應用程式寫入（保持資料庫運行）
docker compose -f infra/docker-compose.yml stop app

# 2. 匯入備份檔
docker compose -f infra/docker-compose.yml exec -T db \
  mariadb -u wurzburg -p"$WURZBURG_DB_PASSWORD" wurzburg \
  < wurzburg_backup_YYYYMMDD_HHMMSS.sql

# 3. 重新啟動應用程式
docker compose -f infra/docker-compose.yml start app
```

---

## 7. 日常維運

### 7.1 忘記系統管理員密碼（主機端重設）

本系統不提供網頁版的「忘記密碼」功能。若所有系統管理員均忘記密碼，請由具有主機權限的管理者執行：

```sh
docker compose -f infra/docker-compose.yml exec app \
  /app/wurzburg reset-password <系統管理員帳號> "<新密碼(至少12字元)>"
```

### 7.2 更新本地 OUI 廠商資料

Würzburg 內建一份離線 IEEE OUI 快照且全程不連外網。如需更新廠商資料庫，請自 IEEE 官網下載最新 `oui.csv`（標準 MA-L / MA-M / MA-S CSV 格式），於「系統設定 → 本地 OUI 資料庫」上傳匯入，系統會自動重新計算既有 IP 紀錄的廠商名稱。

### 7.3 查看應用程式日誌

系統所有結構化日誌皆輸出至標準輸出（stdout），不會包含密碼明文：

```sh
docker compose -f infra/docker-compose.yml logs -f --tail=200 app
```

### 7.4 系統升級步驟

1. 先依照第 6 節完成資料庫備份。
2. 拉取新版程式碼或新版本映像檔：
   ```sh
   git pull
   docker build -t wurzburg:dev .
   ```
3. 重新建立並啟動容器（系統啟動時會自動套用新版本的資料庫遷移檔）：
   ```sh
   docker compose -f infra/docker-compose.yml --env-file infra/.env up -d --no-build
   ```

---

## 8. 驗證來源的注意事項

- **Active Directory (AD)**：
  - 強烈建議使用加密連線（`ldaps://` 連接埠 636 或啟用 StartTLS），避免組織密碼於網路明文傳輸。
  - 若「允許登入的群組 (Base/Group DN)」留空，所有通過驗證的 AD 帳號皆可登入並登記未登記之 IP 紀錄。
- **RADIUS**：
  - 僅支援 **PAP** 驗證機制（因需於系統端取得使用者輸入的密碼向伺服器驗證）。
  - 請務必在 RADIUS 伺服器端將 Würzburg 主機／容器所屬的對外來源 IP 列入允許的 NAS Client 清單，並確認 Shared Secret 一致。
  - RADIUS 成功驗證後僅建立帳號，不具備信箱資訊，一般使用者登記時須自行填寫擁有者信箱。
- **上線後更改驗證來源之後果**：
  - 由於擁有者權限嚴格綁定帳號，一旦在系統正式運作後切換驗證來源（例如從 AD 切換成 RADIUS，或更換目錄網域），原有驗證來源的使用者帳號將無法再登入，其名下登記需由系統管理員於系統中「改派」給新帳號。

---

## 9. 郵件軟體的已知限制

本系統寄發的通知信採用標準 `multipart/alternative`（HTML + 對等純文字），並採用最大相容性的表格版型與 inline CSS（600px 寬度）。受限於部分桌面郵件用戶端（特別是 **Microsoft Outlook 傳統桌面版**使用 Word 引擎渲染 HTML）之特性：

- 圓角按鈕與卡片邊框在 Outlook 中將呈現為直角，但不影響排版對齊、按鈕點擊與表格閱讀。
- 所有主要動作連結於純文字區塊中皆會再完整顯示一次網址，確保極端環境下仍可複製使用。

---

## 10. 目前的限制

- **SNMP 實機驗證狀態**：目前採集模組（IOS-XE 與 NX-OS）之行為與測試資料係依據 Cisco 原廠公開 MIB 規範與文件範例製作，**尚未經實體 Cisco 交換器實機環境確認**（待實機驗證完成並確認無誤後將移除本項聲明）。
- **明確不支援的項目**：IPv6 位址追蹤、DHCP／DNS 伺服器整合、非 Cisco 廠牌交換器（如 Aruba / Juniper / Arista）、透過 CLI/SSH 抓取 ARP、任何外部 OUI 或 Whois 連線查詢、跨園區集中管理與內建備份功能。
