# 領域文件

各個 skill 在探索這個儲存庫時，應該如何使用領域文件。

## 探索之前先讀

- 儲存庫根目錄的 **`CONTEXT.md`**。
- **`docs/adr/`**：讀與你要處理的範圍相關的 ADR。編號 0009 起在這個目錄；ADR-001 到 008 在 `docs/IP-Mgmt_PRD_ADR.docx` 裡。
- **`docs/plan/dev-spec.md`**：開發規格，與 PRD 衝突時以它為準。

這些檔案如果不存在，直接繼續，不必指出，也不必建議先建立。`/domain-modeling`（經由 `/grill-with-docs` 與 `/improve-codebase-architecture` 進入）會在名詞或決定確定時才建立它們。

## 檔案配置

單一 context：

```
/
├── CONTEXT.md
├── docs/
│   ├── adr/
│   ├── plan/
│   └── task/
└── src/
```

## 使用名詞表的用語

輸出中提到領域概念時（工作項目的標題、重構提案、假設、測試名稱），使用 `CONTEXT.md` 定義的名詞，不要改用它列為「Avoid」的同義詞。

需要的概念不在名詞表裡時，這是一個訊號：要嘛你在發明這個專案不用的說法（重新考慮），要嘛名詞表確實有缺口（記下來交給 `/domain-modeling`）。

## 指出與 ADR 的牴觸

輸出與既有的 ADR 牴觸時，要明確指出，不要默默推翻：

> _與 ADR 0012（擁有者是帳號）牴觸，但值得重新討論，因為…_
