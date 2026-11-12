package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jeffhou369/wurzburg/backend/internal/clock"
)

// EnsureFirstAdmin 在資料庫尚未有任何系統管理員時，使用環境變數提供的帳密建立第一位管理員，
// 並強制其首次登入修改密碼。若資料庫已有系統管理員，此函式不會進行任何變更。
func EnsureFirstAdmin(ctx context.Context, db *sql.DB, clk clock.Clock, username, initialPassword string) error {
	if db == nil {
		return nil
	}
	if clk == nil {
		clk = clock.NewRealClock()
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&count); err != nil {
		return fmt.Errorf("檢查系統管理員數量失敗: %w", err)
	}

	if count > 0 {
		return nil
	}

	if username == "" || initialPassword == "" {
		slog.Warn("資料庫尚無系統管理員且未提供初始管理員環境變數 (WURZBURG_ADMIN_USERNAME / WURZBURG_ADMIN_PASSWORD)")
		return nil
	}

	hash, err := HashPassword(initialPassword)
	if err != nil {
		return err
	}

	now := clk.Now()
	_, err = db.ExecContext(
		ctx,
		`INSERT INTO users (username, display_name, mail, role, auth_source, password_hash, requires_password_change, enabled, created_at, updated_at)
		 VALUES (?, ?, '', 'admin', 'local', ?, TRUE, TRUE, ?, ?)`,
		username, "系統管理員", hash, now, now,
	)
	if err != nil {
		return fmt.Errorf("建立第一位系統管理員失敗: %w", err)
	}

	// 注意：日誌絕對不可印出 initialPassword
	slog.Info("已依環境變數建立第一位系統管理員帳號（首次登入將要求修改密碼）", "username", username)
	return nil
}

// ResetAdminPassword 提供主機端命令列重設本機系統管理員密碼，同時解除鎖定與解除強制改密碼旗標。
func ResetAdminPassword(ctx context.Context, db *sql.DB, clk clock.Clock, username, newPassword string, minLen int) error {
	if db == nil {
		return errors.New("資料庫未連線")
	}
	if err := ValidatePasswordPolicy(newPassword, minLen); err != nil {
		return err
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}

	now := clk.Now()
	res, err := db.ExecContext(
		ctx,
		`UPDATE users
		 SET password_hash = ?, requires_password_change = FALSE, failed_login_count = 0, locked_until = NULL, enabled = TRUE, updated_at = ?
		 WHERE username = ? AND role = 'admin' AND auth_source = 'local'`,
		hash, now, username,
	)
	if err != nil {
		return fmt.Errorf("重設密碼失敗: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("找不到本機系統管理員帳號: %s", username)
	}
	return nil
}
