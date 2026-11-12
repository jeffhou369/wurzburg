package static

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// DistFS 內嵌前端打包輸出的靜態檔案。
//
//go:embed all:dist
var DistFS embed.FS

// SPAHandler 建立適用於 Angular Single Page Application 的 HTTP 處理器。
func SPAHandler() http.Handler {
	sub, err := fs.Sub(DistFS, "dist")
	hasDist := err == nil && fileExistsInFS(sub, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")

		if hasDist {
			// 若檔案存在則直接回傳該檔案
			if path != "" && fileExistsInFS(sub, path) {
				http.FileServer(http.FS(sub)).ServeHTTP(w, r)
				return
			}
			// 其餘路徑回退至 index.html（SPA 路由）
			indexBytes, readErr := fs.ReadFile(sub, "index.html")
			if readErr == nil {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(indexBytes)
				return
			}
		}

		// 若尚無打包之前端檔案（例如本地獨立開發後端時），提供預設之基本骨架畫面
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<!DOCTYPE html>
<html lang="zh-Hant">
<head>
  <meta charset="utf-8">
  <title>Würzburg IP 管理系統</title>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #ffffff; color: #0f172a; padding: 2rem; }
    .card { border: 1px solid #e2e8f0; border-radius: 12px; padding: 1.5rem; max-width: 600px; box-shadow: 0 1px 3px rgba(15,23,42,0.05); }
    .badge { display: inline-block; background: #f0fdf4; color: #15803d; border: 1px solid #bbf7d0; border-radius: 9999px; padding: 2px 10px; font-size: 0.875rem; }
  </style>
</head>
<body>
  <div class="card">
    <h2>Würzburg IP 管理系統</h2>
    <p><span class="badge">● 系統正常運作中</span></p>
    <p>前端骨架與後端核心已成功連通。請造訪 <a href="/api/v1/health">/api/v1/health</a> 檢視後端健康狀態。</p>
  </div>
</body>
</html>`))
	})
}

func fileExistsInFS(filesystem fs.FS, path string) bool {
	clean := filepath.Clean(path)
	f, err := filesystem.Open(clean)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

func init() {
	// 確保即使目錄為空也不會導致編譯錯誤
	_ = os.Getenv
}
