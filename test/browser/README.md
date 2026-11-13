# 瀏覽器關鍵流程

以 Playwright 對一套隔離環境走第 42 張工作項目的流程：登入（含初次改密碼與登入後導向）、設定精靈、單筆與批次登記、批次重新確認、裁決衝突、把網段改為不納管。

環境是真實的 Go 後端（內嵌剛建置的前端）、MariaDB，以及模擬的 SNMP、LDAP、郵件伺服器。不攔截網路、不偽造 API 回應、不使用測試身分標頭。容器是拋棄式的，不會動到既有部署。

## 執行

需要 Docker、Go、Node.js。第一次：

```sh
cd test/browser
npm ci
npx playwright install chromium
```

之後在儲存庫根目錄：

```sh
test/browser/run.sh
```

`run.sh --hold` 只啟動環境並保持運作，供手動看畫面；位址寫在 `test/browser/.env.json`，刪除該檔案即結束。

CI（`.github/workflows/code-quality.yml` 的 Browser Flows）會執行同一套流程。
