package database

import (
	"database/sql"
	"embed"
	"fmt"
	"log/slog"

	"github.com/pressly/goose/v3"
)

// RunMigrations 套用內嵌的 SQL 遷移檔至指定之資料庫。
func RunMigrations(db *sql.DB, migrationsFS embed.FS, dir string) error {
	goose.SetBaseFS(migrationsFS)
	defer goose.SetBaseFS(nil)

	if err := goose.SetDialect("mysql"); err != nil {
		return fmt.Errorf("設定資料庫遷移方言失敗: %w", err)
	}

	slog.Info("正在檢查並套用資料庫遷移檔...")
	if err := goose.Up(db, dir); err != nil {
		return fmt.Errorf("套用資料庫遷移失敗: %w", err)
	}

	slog.Info("資料庫遷移已全部套用完成")
	return nil
}
