# 22 — RADIUS 登入

Status: resolved
Blocked by: 07

## 要做什麼

讓園區可以選擇 RADIUS 作為驗證來源，一般使用者用 RADIUS 的帳密登入。

- 實作驗證來源介面的 RADIUS 版本：以 PAP 驗證帳密。
- 設定頁「驗證來源」底下的 RADIUS 表單：伺服器位址與埠、shared secret、逾時。
- Shared secret 加密保存，儲存後不回顯。
- 「測試」按鈕：以一組帳密試著登入，顯示成功或失敗的原因。
- RADIUS 只回答帳密對不對：帳號的顯示名稱就是帳號本身，沒有信箱；登記時擁有者信箱由使用者自己填。

開始前查最新文件，決定 RADIUS 程式庫與版本。

## 驗收條件

- [x] 對模擬的 RADIUS，正確的帳密可以登入，錯誤的帳密不行
- [x] 登入後帳號的顯示名稱是帳號本身，沒有信箱
- [x] RADIUS 的帳號登記時，擁有者信箱不會自動帶入
- [x] 「測試」成功或失敗都有清楚的結果；shared secret 錯誤與逾時能區分
- [x] RADIUS 連不上時，一般使用者看到清楚的訊息；系統管理員仍然可以登入
- [x] Shared secret 在 API 回應、日誌與稽核紀錄中都讀不到
- [x] 設定的變更寫入稽核紀錄，且不含 secret
- [x] 安裝文件用的說明片段：RADIUS 伺服器端要允許本系統的來源 IP，以及 PAP 的安全性限制

## Comments

已完成 22 — RADIUS 登入：
- 採用 `layeh.com/radius` 實作 `internal/authsource/radius.go` PAP 驗證，正確區分密碼拒絕、連線逾時與 Shared Secret 驗證不符 (`NonAuthenticResponseError`)。
- 驗證成功後固定設定顯示名稱為帳號本身，信箱為空字串，登記時由使用者自行填寫。
- `internal/authsource/authsource_test.go` 啟動本機 UDP `PacketServer` 完成完整 RFC 2865 交換測試。
- 說明文件備忘：部署時 RADIUS 伺服器（如 NPS／FreeRADIUS）須將 Würzburg 主機來源 IP 加入允許的 RADIUS Client 清單；因採用 PAP 協定，強烈建議部署於受保護之管理網段或經由安全通道傳輸。

## 參考

- 開發規格第 5 節
- PRD ADR-003
- `docs/plan/mvp-spec.md` 使用者故事 14、15、128、132
