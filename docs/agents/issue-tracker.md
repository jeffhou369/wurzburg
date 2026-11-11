# Issue tracker：本機 Markdown

這個儲存庫的 spec 與工作項目都是儲存庫裡的 Markdown 檔。

## 慣例

- 每個功能有一個代稱（ASCII，例如 `mvp`）。
- spec 是 `docs/plan/<功能代稱>-spec.md`。
- 工作項目一張一個檔案：`docs/task/<功能代稱>/<NN>-<slug>.md`，從 `01` 編號；不要合併成一個檔案。
- 狀態寫在每個檔案開頭的 `Status:` 行，可用的值見 `triage-labels.md`。
- 留言與討論紀錄附加在檔案最後的 `## Comments` 標題下。

## skill 說「發佈到 issue tracker」時

依上面的慣例建立新檔案，必要時建立目錄。

## skill 說「取得相關的工作項目」時

讀取指定路徑的檔案。使用者通常會直接給路徑或編號。

## Wayfinding 操作

供 `/wayfinder` 使用。**地圖**是一個檔案，每張工作項目是一個**子項目**檔案。

- **地圖**：`docs/plan/<代稱>-map.md`，內容是 Notes、Decisions-so-far、Fog。
- **子項目**：`docs/task/<代稱>/<NN>-<slug>.md`，從 `01` 編號，問題寫在內文。`Type:` 行記錄類型（`research`、`prototype`、`grilling`、`task`），`Status:` 行記錄 `claimed` 或 `resolved`。
- **阻擋關係**：檔案開頭的 `Blocked by: NN, NN` 行。列出的項目都是 `resolved` 時才解除阻擋。
- **待辦前線**：掃描 `docs/task/<代稱>/`，找出尚未完成、未被阻擋、也沒有人認領的檔案；編號最小的優先。
- **認領**：先把 `Status:` 設為 `claimed` 並存檔，再開始工作。
- **完成**：在 `## Answer` 標題下寫入結果，`Status:` 設為 `resolved`，並在地圖的 Decisions-so-far 附上摘要與連結。
