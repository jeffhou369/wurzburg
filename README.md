# Würzburg

內部用的 IPv4 管理系統：定時以 SNMP 讀取 Cisco 交換器（IOS-XE、NX-OS）的 ARP table，為納管網段內的每個位址維護一筆 IP 紀錄；使用者登入後登記自己負責的位址；系統寄信通知未登記的位址與即將到期的登記。一個園區裝一套。

---

## 為誰而做

Würzburg 專為企業或機構單一園區的網路管理人員與終端使用者而設計。在不需要導入複雜且昂貴的商業 IPAM / DDI 方案的前提下，以純內部、不連外網、高可靠度的架構，自動追蹤交換器上活躍的所有 IPv4 位址，杜絕幽靈 IP，並建立責任歸屬。

## 運作方式

- **自動採集**：每 3 分鐘透過 SNMPv2c / SNMPv3 讀取擔任 L3 閘道之 Cisco 交換器的 ARP 表、介面位址與介面名稱。
- **狀態整併**：依據最長前綴比對（LPM）將位址歸入納管網段，排除閘道與 DHCP 範圍；維護多來源觀測，連續 2 輪未回報才轉為離線；自動偵測並標示 MAC 衝突。
- **自主登記**：支援整合組織 AD (LDAPS) 或 RADIUS (PAP)，使用者登入後登記自己負責的位址，登記有效期預設一年。
- **主動通知**：每小時寄送「未登記在線位址彙整」、每週寄送「未登記累積總表」、到期前後定時向擁有者「到期催認」，信件均為兼具 HTML 表格與等價純文字之標準 `multipart/alternative` 格式。

---

## 最短啟動步驟（Docker Compose）

### 1. 準備環境變數檔

在專案目錄下複製並設定環境變數：

```sh
cd infra
cp .env.example .env
```

請編輯 `.env`，至少產生一組 32 位元組（64 位元 Hex）之加密金鑰，並指定初始系統管理員密碼：

```env
# 產生金鑰指令：openssl rand -hex 32
WURZBURG_SECRET_KEY=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
WURZBURG_ADMIN_USERNAME=admin
WURZBURG_ADMIN_PASSWORD=YourStrongInitialPassword123!
```

### 2. 建置並啟動服務

```sh
# 從前一步的 infra 目錄回到專案根目錄
cd ..
# 建置容器映像檔
docker build -t wurzburg:dev .

# 啟動 MariaDB 資料庫與 Würzburg 應用程式
docker compose --env-file infra/.env -f infra/docker-compose.yml up -d --no-build
```

### 3. 登入並走完設定精靈

以瀏覽器開啟 `http://<主機IP>:8080/`：

1. 點擊「系統管理員」，以您在環境變數中設定的 `WURZBURG_ADMIN_USERNAME` 與初始密碼登入。
2. 系統將強制要求修改初始密碼（請設為至少 12 個字元之強密碼）。
3. 修改完成後將自動開啟五步驟設定精靈：
   - **步驟 1**：設定園區顯示名稱與時區。並於「系統設定 → 園區資訊」設定收件人實際可連線的公開根網址，例如 `https://ip.example.com`；未設定公開網址時系統會停止寄信並提示，不會以 `localhost` 代替。
   - **步驟 2**：設定組織驗證來源（AD 或 RADIUS，可測試連線）。
   - **步驟 3**：設定 SMTP 郵件伺服器與管理員收件清單（可寄出測試信）。
   - **步驟 4**：新增第一台 ARP 來源交換器（可即時測試讀取）。
   - **步驟 5**：檢視交換器介面探索之網段，將需管理的網段設為「納管」。
4. 首次登入成功後，請將 `.env` 中的初始密碼刪除。

---

## 啟用 HTTPS / TLS 終止

系統預設以 HTTP 啟動。在非信任網路環境中，請掛載選用的 Nginx 容器終止 TLS，保護帳號密碼與機敏憑證：

```sh
docker compose --env-file infra/.env -f infra/docker-compose.yml -f infra/docker-compose.https.yml up -d --no-build
```

詳細憑證準備與設定說明請參閱 [infra/HTTPS-README.md](infra/HTTPS-README.md)。

---

## 其他文件

- **完整安裝與維運手冊**：[docs/install.md](docs/install.md)
- **領域詞彙與概念定義**：[CONTEXT.md](CONTEXT.md)
- **開發規格書**：[docs/plan/dev-spec.md](docs/plan/dev-spec.md)
- **資料模型**：[docs/plan/data-model.md](docs/plan/data-model.md)
- **架構決策紀錄 (ADR)**：[docs/adr/](docs/adr/)
- **REST API 契約 (OpenAPI)**：[docs/api/](docs/api/)
- **SNMP 雙平台回應範例與 MIB**：[test/testdata/snmp/README.md](test/testdata/snmp/README.md)

---

## 授權與致謝

- **授權**：本專案採用 [MIT License](LICENSE)。
- **UI 設計樣式出處**：使用者介面設計 token 與版型基線沿用自 EasyCLIProxyAPI 專案樣式，遵循 MIT 授權條款，原始授權文件留存於 [src/frontend/src/styles/LICENSE-EasyCLIProxyAPI](src/frontend/src/styles/LICENSE-EasyCLIProxyAPI)。
