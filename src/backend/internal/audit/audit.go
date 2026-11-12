package audit

import (
	"context"
	"database/sql"
	"encoding/json"
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

// Log 記錄一筆稽核事件。會自動遞迴檢查並遮蔽機敏欄位。
func (l *Logger) Log(
	ctx context.Context,
	username, userRole, ipAddress string,
	action, targetType, targetID, summary string,
	details map[string]any,
) {
	if l.db == nil {
		return
	}

	sanitized := sanitizeMap(details)
	detailsJSON, _ := json.Marshal(sanitized)

	now := l.clock.Now()
	_, err := l.db.ExecContext(
		ctx,
		`INSERT INTO audit_logs (username, user_role, ip_address, action, target_type, target_id, summary, details_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		username, userRole, ipAddress, action, targetType, targetID, summary, string(detailsJSON), now,
	)
	if err != nil {
		slog.ErrorContext(ctx, "寫入稽核日誌失敗", "error", err, "action", action)
	}
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
