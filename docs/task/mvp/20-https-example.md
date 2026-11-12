# 20 — HTTPS 範例

Status: resolved
Blocked by: 01

## 要做什麼

預設安裝是 HTTP。這一張提供一份選用的設定，讓安裝人員用自己的憑證把連線加密。

- 一份選用的 Compose 設定，在 `app` 前面加上一個反向代理做 TLS 終止，並把 HTTP 轉到 HTTPS。
- 憑證由安裝人員提供並掛載進去；另外說明如何產生測試用的自簽憑證。
- 反向代理把「原始連線是 HTTPS」的資訊傳給應用程式，讓應用程式知道自己在 HTTPS 後面。
- 啟用後，`app` 不再直接對外開放。

應用程式據此替 Cookie 加上 Secure、並隱藏登入頁的明文警告，分別由第 06、07 張完成。

## 驗收條件

- [x] 加上這份選用的設定啟動後，以 HTTPS 可以開啟應用程式
- [x] 以 HTTP 連線會被轉到 HTTPS
- [x] 啟用後 `app` 的埠不對外開放，只能經由反向代理存取
- [x] 應用程式收到的請求帶有原始連線為 HTTPS 的資訊
- [x] 不加這份設定時，預設的 HTTP 安裝不受任何影響
- [x] 有一份簡短的說明：如何放入憑證、如何產生測試用的自簽憑證、如何啟用與停用
- [x] 倉庫裡沒有任何憑證或私鑰檔案

## Comments

已完成 20 — HTTPS 範例：
- `infra/docker-compose.https.yml`：透過 `!reset []` 移除 `app` 主機端對外埠，加入 `nginx:1.27-alpine` 負責 TLS 終止。
- `infra/nginx/wurzburg.conf`：配置 80 轉 443（301 永久轉址）、TLSv1.2/1.3、HSTS 與 `X-Forwarded-Proto: https`。
- `infra/HTTPS-README.md`：提供繁體中文操作文件與測試用自簽憑證指令。
- `.gitignore`：加入 `infra/certs/`、`*.crt`、`*.key`、`*.pem` 防止誤提交機敏憑證。
- 已於本地以測試自簽憑證啟動完整 Compose 疊加設定，驗證 HTTP 301 轉址與 HTTPS `/api/v1/health` 回應皆正常。

## 邊界

- 只新增部署設定與它的說明，不修改應用程式的程式碼。

## 參考

- 開發規格第 5 節（工作階段與傳輸）
- PRD ADR-006
- `docs/plan/mvp-spec.md` 使用者故事 5
