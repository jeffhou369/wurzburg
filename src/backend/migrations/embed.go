package migrations

import "embed"

// FS 內嵌所有資料庫 SQL 遷移檔案。
//
//go:embed *.sql
var FS embed.FS
