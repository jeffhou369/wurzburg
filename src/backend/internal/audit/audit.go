package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jeffhou369/wurzburg/backend/internal/clock"
)

// Logger 提供系統全域操作的稽核日誌記錄工具。
type Logger struct {
	db    *sql.DB
	clock clock.Clock
}

func NewLogger(db *sql.DB, clk clock.Clock) *Logger {
	if clk == nil {
		clk = clock.NewRealClock()
	}
	return &Logger{db: db, clock: clk}
}

// auditExecer 抽象出「可執行寫入」的物件，讓 Log 與 LogTx 共用同一份 INSERT，
// 由 *sql.DB（自動提交）或 *sql.Tx（隨交易提交／回滾）各自代入。
type auditExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// insert 是唯一的稽核寫入點：先遞迴遮蔽機敏欄位，再序列化並寫入 audit_logs。
func (l *Logger) insert(
	ctx context.Context,
	exec auditExecer,
	username, userRole, ipAddress string,
	action, targetType, targetID, summary string,
	details map[string]any,
) error {
	sanitized := sanitizeMap(details)
	detailsJSON, err := json.Marshal(sanitized)
	if err != nil {
		return fmt.Errorf("序列化稽核明細失敗: %w", err)
	}

	_, err = exec.ExecContext(
		ctx,
		`INSERT INTO audit_logs (username, user_role, ip_address, action, target_type, target_id, summary, details_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		username, userRole, ipAddress, action, targetType, targetID, summary, string(detailsJSON), l.clock.Now(),
	)
	return err
}

// Log 記錄一筆稽核事件。會自動遞迴檢查並遮蔽機敏欄位。
// 寫入失敗只記錄於系統日誌，不影響呼叫端流程（適用於「事後附記」型的稽核）。
func (l *Logger) Log(
	ctx context.Context,
	username, userRole, ipAddress string,
	action, targetType, targetID, summary string,
	details map[string]any,
) {
	if l.db == nil {
		return
	}
	if err := l.insert(ctx, l.db, username, userRole, ipAddress, action, targetType, targetID, summary, details); err != nil {
		slog.ErrorContext(ctx, "寫入稽核日誌失敗", "error", err, "action", action)
	}
}

// LogTx 在呼叫端的交易內寫入一筆稽核事件，參數與 Log 相同、另多一個 tx。
// 稽核與業務變更同交易提交或同時回滾；寫入失敗會回傳錯誤，由呼叫端決定回滾，
// 因此適用於「沒有稽核就不得變更」的操作（例如刪除已登記的 IP 紀錄）。
// 機敏欄位的遮蔽規則與 Log 完全相同。
func (l *Logger) LogTx(
	ctx context.Context,
	tx *sql.Tx,
	username, userRole, ipAddress string,
	action, targetType, targetID, summary string,
	details map[string]any,
) error {
	if tx == nil {
		return errors.New("LogTx 需要有效的交易")
	}
	return l.insert(ctx, tx, username, userRole, ipAddress, action, targetType, targetID, summary, details)
}

func sanitizeMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		lowerK := strings.ToLower(k)
		if strings.Contains(lowerK, "password") ||
			strings.Contains(lowerK, "secret") ||
			strings.Contains(lowerK, "community") ||
			strings.Contains(lowerK, "token") ||
			strings.Contains(lowerK, "passphrase") {
			out[k] = "[REDACTED]"
			continue
		}

		if subMap, ok := v.(map[string]any); ok {
			out[k] = sanitizeMap(subMap)
		} else {
			out[k] = v
		}
	}
	return out
}
