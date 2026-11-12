# Würzburg 選用 HTTPS / TLS 部署指南

預設的 `infra/docker-compose.yml` 僅提供 HTTP 連線（供內部實驗室快速啟動）。在正式或非完全受信任的內網環境中，建議掛載此選用的 Nginx 反向代理設定進行 TLS 終止，保護組織帳號密碼、SNMP 憑證與工作階段 Cookie。

## 1. 準備憑證

倉庫內**不包含**任何憑證或私鑰檔案。請將組織核發之憑證與私鑰放入 `infra/certs/`（已被 `.gitignore` 排除）：

- 憑證檔案：`infra/certs/wurzburg.crt`（需包含完整憑證鏈）
- 私鑰檔案：`infra/certs/wurzburg.key`

或透過環境變數指定主機上的其他路徑：
- `WURZBURG_TLS_CERT_PATH`
- `WURZBURG_TLS_KEY_PATH`

### 產生測試用自簽憑證（僅供測試／實驗室）

```sh
mkdir -p infra/certs
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout infra/certs/wurzburg.key \
  -out infra/certs/wurzburg.crt \
  -subj "/CN=wurzburg.local" \
  -addext "subjectAltName=DNS:wurzburg.local,IP:127.0.0.1"
```

## 2. 啟用 HTTPS

使用兩份 Compose 設定檔啟動服務：

```sh
docker compose -f infra/docker-compose.yml -f infra/docker-compose.https.yml up -d
```

啟用後的效果：
1. `app` 容器停止對主機直接開放 8080 埠，只能透過內部網路的 `nginx` 存取。
2. `nginx` 容器監聽 80 與 443 埠（主機埠可透過 `WURZBURG_HTTPS_HTTP_PORT` 與 `WURZBURG_HTTPS_PORT` 調整），並將所有 HTTP 請求轉址（301）至設定的 HTTPS 主機埠。
3. 反向代理自動附帶 `X-Forwarded-Proto: https` 標頭，通知 Würzburg 應用程式目前處於加密連線後面（應用程式將自動對 Cookie 加上 `Secure` 屬性並隱藏登入頁明文警告）。

## 3. 停用並切回預設 HTTP

```sh
docker compose -f infra/docker-compose.yml -f infra/docker-compose.https.yml down
docker compose -f infra/docker-compose.yml up -d
```
