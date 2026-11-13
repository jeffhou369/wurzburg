# 41 — 13 個端到端情境只用 API 與時鐘產生狀態

Status: ready-for-agent
Blocked by: 39, 40

## 要做什麼

由第 33 張拆出（待補驗證第 1 項）。`TestFullE2EIntegration13Scenarios`（`src/backend/test/e2e_pipeline_test.go`）雖然從全新的 MariaDB 容器開始，13 個子測試也都通過，但不符合第 33 張「所有狀態都由 API 的操作與時鐘的推進產生」的要求：

- 部分情境直接以 SQL 修改或建立產品狀態，或以 SQL 讀取識別碼。
- 管理請求在改完密碼後附帶測試身分標頭（`X-Test-User-ID` 等），繞過真正的工作階段。
- 驗證來源以 `srv.Settings.Set` 直接寫入，沒有經過設定 API。
- 外部系統使用介面替身（`FakeCollector`、`FakeAuthSource`、`FakeMailer`），不是模擬伺服器。

1. **消除直接操作資料庫**
   - 13 個情境中不得出現 `db.Exec`、`db.Query` 或直接呼叫內部服務方法來建立、修改或讀取產品狀態。識別碼從 API 回應取得；驗證結果只看 API 回應、信件、匯出檔與稽核 API。
   - 排程到期的工作以推進測試時鐘並觸發到期工作的方式執行，與正式背景迴圈共用同一套到期判定。
2. **真正的登入與工作階段**
   - 不使用測試身分標頭。系統管理員與一般使用者都以登入取得的 cookie 操作；時鐘大幅推進造成工作階段逾時時重新登入。
3. **外部系統改用模擬伺服器**
   - SNMP：以模擬器回放 `test/testdata/snmp/` 的測試資料檔，經過真正的 SNMP 採集模組。
   - 驗證來源：模擬的 LDAP（AD）或 RADIUS 伺服器，經過真正的驗證模組與設定 API。
   - 郵件：收信用的測試 SMTP 伺服器，經過真正的 SMTP 寄信模組。
   - 模擬伺服器只在測試中啟動；不新增只供測試呼叫的公開 HTTP 端點。
4. **沿用第 39、40 張的檢查**
   - 這套情境的每個回應都經過 OpenAPI 驗證與 Secret 掃描。

## 不在範圍內

- 瀏覽器流程（第 42 張）。
- 實體交換器驗證（第 36 張）。
- 規模驗證維持在 `scale_poll_cycle_test.go`，不併入本張。

## 驗收條件

- [ ] 13 個情境各有自動化測試，從空的系統開始，不擺前置資料，全部通過
- [ ] 情境程式中沒有直接操作資料庫或內部服務來建立、修改、讀取產品狀態
- [ ] 沒有使用測試身分標頭；所有請求都帶登入取得的 cookie
- [ ] SNMP、驗證來源、郵件都經過真正的用戶端模組連到模擬伺服器
- [ ] 每個回應都通過 OpenAPI 驗證與 Secret 掃描
- [ ] 改寫過程中發現的產品問題，修正並記在 `## Comments`，或另開工作項目
- [ ] 後端 `gofmt`、`go vet`、golangci-lint、`go test -race -count=1 ./...` 通過，CI 通過

## Comments

- 2026-11-13：由第 33 張拆出。
- 2026-11-13：實作完成。

  **做法**
  - 舊的 `TestFullE2EIntegration13Scenarios`（`e2e_pipeline_test.go`）整個移除，改寫為 `TestE2EScenarios`（`src/backend/test/e2e_scenarios_test.go`），環境在 `e2e_env_test.go`。
  - **以正式方式組裝**：模組組裝由 `cmd/wurzburg/modules.go` 移到可匯入的 `internal/app`（`app.Mount`、`StartBackgroundLoops`），主程式與測試共用同一份。測試的 `AppEnv` 不是 `test`，測試身分標頭不會生效。`TestRegisteredRoutesMatchSpec` 也改用 `app.Mount`，不再手寫模組清單。
  - **真正的登入**：每個請求都帶登入取得的 cookie。時鐘大幅推進使工作階段逾時時，收到 401 就重新登入一次，與使用者在瀏覽器上的行為相同。
  - **狀態只由 API 與時鐘產生**：情境程式沒有任何資料庫存取，也不呼叫內部服務；識別碼從 API 回應取得，結果只看 API 回應、收到的信與匯出檔。排程到期的工作以推進測試時鐘後呼叫與背景迴圈相同的入口觸發（`Scheduler.RunCycle`、`Notifications.Tick`、`Maintenance.Tick`）。
    - 到期催認：真的登記後把時鐘推進約一年，依序收到到期前 14 天、到期當天、逾期 7／14／21／28 天共 6 封信。
    - 自動清除：讓位址真的離線，再推進 91 天。
  - **模擬伺服器**（Testcontainers，映像都鎖定 digest）
    - SNMP：`ghcr.io/lextudio/docker-snmpsim`。IOS-XE 那台用 SNMPv2c，資料由情境動態產生並在執行中更換；NX-OS 那台用 SNMPv3（SHA 驗證加 AES 加密），預設 VRF 與 VRF PROD 回放 `test/testdata/snmp/nx-os/` 的兩個資料檔。
    - 驗證來源：`vegardit/openldap`，以自備的設定啟動成 AD 形狀的目錄（`user` 類別、`sAMAccountName`、`memberOf`），經過真正的 AD 驗證模組與設定 API。
    - 郵件：`axllent/mailpit`，SMTP 需要帳密驗證；以它的錯誤注入模擬郵件伺服器暫時拒收。
    - 三個模擬伺服器各有一個冒煙測試，直接以真正的用戶端模組驗證（`sim_*_smoke_test.go`），端到端失敗時可分辨是模擬器還是產品的問題。
  - 每個回應都經過第 39、40 張的 OpenAPI 驗證與 Secret 掃描；模擬伺服器端的 Secret 也登錄到掃描器，Mailpit 收到的每封信都經過掃描。
  - 情境依時間先後執行（時鐘只能往前），順序是 1、2、3、4、5、6、8、11、7、9、10、12、13。

  **改寫過程中發現並修正的產品問題**

  | 問題 | 修正 |
  | --- | --- |
  | 「即將到期」的判斷不一致：到期催認信依園區時區的日期，在到期日前 14 天的早上寄出；清單的 `pending_confirmation`、`expiring_soon` 篩選與狀態標示卻依精確時間。登記時間在當天較晚時，擁有者早上收到信、依信中連結打開待確認清單會是空的，要等到登記的時刻才出現 | 新增 `state.ExpiringSoonBefore`，清單篩選、狀態標示與催認信都以園區時區的日期為準；`state.Compute` 多一個時區參數。兩個以舊規則寫的單元測試案例改為新規則，另加邊界與時區測試 |
  | 驗證來源連不上時，組織帳號登入回 401「組織帳號或密碼錯誤」，使用者會誤以為自己打錯密碼 | 改回 503 `auth_unavailable`，訊息說明是驗證服務暫時無法使用；描述檔的 `POST /auth/login/org` 明確列出 503 |
  | 建立 SNMPv3 交換器的回應帶有描述檔沒有的 `v3_username`（先前沒有測試經 API 建過 v3 交換器） | 描述檔的 `Switch` 補上 `v3_username` |

  **觀察到但沒有處理的事項**（不影響情境通過；是否要處理由第 33 張收尾時決定）
  - 未設定信任代理時，稽核紀錄的來源位址帶有埠號（例如 `192.0.2.1:1234`），設定信任代理後則沒有，兩種情況格式不一致。
  - 單筆 VRF 讀取失敗只記在資料庫與 API（交換器頁面、儀表板看得到），應用程式日誌沒有對應的輸出。
  - `PUT /ip-records/{id}` 先驗證輸入才檢查擁有權，非擁有者送出不完整的內容會得到 422 而不是 403。

  **模擬的範圍與限制**
  - LDAP 模擬伺服器是 AD 形狀的 OpenLDAP，不是真正的 Active Directory：巢狀群組、帳號停用、密碼過期等行為不在範圍。`memberOf` 直接寫在使用者項目上。
  - RADIUS 沒有納入端到端情境；RADIUS 用戶端在 `internal/authsource` 有對真實 UDP 伺服器的測試。
  - SMTP 連線不加密（`smtp_tls_mode` 為 `none`）；STARTTLS 與 SMTPS 需要受信任的憑證，沒有涵蓋。
  - SNMP 的資料來自原廠文件與公開範例，模擬器只是照實回放；交換器的實際行為仍要由第 36 張的實機驗證確認。`ios-xe/default-vrf.walk` 沒有在端到端情境中回放（IOS-XE 那台用動態產生的資料），`ios-xe/vrf-corp.walk` 只在冒煙測試使用。
  - 兩台模擬交換器共用同一個模擬器，以不同的管理 IP 區分，所以執行環境需要 127.0.0.1 以外的第二個 IPv4 位址；CI 上找不到時測試失敗，本機找不到時略過情境 12。

  **驗證**
  - `TestE2EScenarios` 13 個情境全部通過，約 32 秒。
  - 後端：`gofmt`、`go vet`、golangci-lint v2.15.0（0 issues）、`go test -race -count=1 ./...` 全部通過（`test` 套件約 221 秒）；API 契約涵蓋率與 Secret 掃描沒有發現問題。
  - 前端：產生型別一致，`lint`、`format:check`、`build` 通過，單元測試 155／155。

