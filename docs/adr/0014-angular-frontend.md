---
status: accepted
date: 2026-11-12
supersedes: PRD ADR-007 的前端框架選擇
---

# 前端採用 Angular，元件以 Angular CDK 為基礎自己寫

PRD 的 ADR-007 選的是 React + TypeScript + Vite。Jeff 決定前端改用 Angular。元件方面，PRD 要求介面對齊 EasyCLIProxyAPI 的外觀，並禁止直接套用元件庫的預設主題，因此我們不使用 Angular Material 的外觀，改為自己寫元件：行為的部分（浮層、對話框的焦點管理、鍵盤操作、無障礙屬性、表格）用 Angular CDK，外觀全部依樣式包的設計 token。

## 考慮過的做法

- **React + TypeScript + Vite**（PRD ADR-007）：與上游專案同一個框架，方便對照。但我們只取上游的樣式，不取它的元件，這個優點有限。
- **Angular Material 再換膚**：元件現成，但它的 Material Design 外觀很強，要改到看不出來往往比自己寫費工，改版時覆蓋的樣式也可能失效。
- **其他元件庫的無樣式模式**：元件種類較多，但多一個外部相依，樣式包的 token 不一定能直接套上。

## 後果

- 樣式包中 EasyCLIProxyAPI 的樣式是不綁框架的 CSS 變數，可以直接沿用；Management Center 的 SCSS 也能直接使用。
- 每個元件的外觀都要自己做。系統的畫面不多，需要的元件種類有限。
- 前端建置後的靜態檔仍包進後端的執行檔，API 型別仍由 OpenAPI 描述檔產生。
- PRD ADR-007 的其餘內容仍然有效：視覺基準、主題整套切換、Lucide 圖示、以 UI UX Pro Max 輔助設計流程與交付前檢查。
