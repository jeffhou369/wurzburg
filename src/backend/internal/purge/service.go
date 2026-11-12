package purge

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/jeffhou369/wurzburg/backend/internal/audit"
	"github.com/jeffhou369/wurzburg/backend/internal/auth"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/dedupkey"
	"github.com/jeffhou369/wurzburg/backend/internal/settings"
	"github.com/jeffhou369/wurzburg/backend/internal/subnets"
)

// Service 負責清除過期未登記離線紀錄與管理保留天數政策。
type Service struct {
	db       *sql.DB
	clock    clock.Clock
	settings *settings.Store
	audit    *audit.Logger
}

func NewService(db *sql.DB, clk clock.Clock, sett *settings.Store, aud *audit.Logger) *Service {
	if clk == nil {
		clk = clock.NewRealClock()
	}
	return &Service{db: db, clock: clk, settings: sett, audit: aud}
}

// RunAutoPurge 依 auto_purge_days 清除「未登記且離線超過天數」的 IP 紀錄，返回刪除筆數。
// 若 auto_purge_days <= 0，停用清除並回傳 0。
func (s *Service) RunAutoPurge(ctx context.Context) (int, error) {
	if s.db == nil {
		return 0, nil
	}

	days := s.settings.GetInt(ctx, "auto_purge_days", 90)
	if days <= 0 {
		return 0, nil
	}

	cutoff := s.clock.Now().AddDate(0, 0, -days)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := subnets.NewResolver(s.db).LockScope(ctx, tx); err != nil {
		return 0, err
	}

	// 1. 先找出所有符合清除條件的紀錄 IP，同步清理其通知去重狀態
	rows, err := tx.QueryContext(
		ctx,
		`SELECT id, ip_address FROM ip_records
		 WHERE presence = 'offline' AND registration = 'unregistered' AND owner_user_id IS NULL AND last_seen_at <= ? ORDER BY id FOR UPDATE`,
		cutoff,
	)
	if err != nil {
		return 0, err
	}
	type target struct {
		id int64
		ip string
	}
	var targets []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.id, &t.ip); err != nil {
			_ = rows.Close()
			return 0, err
		}
		targets = append(targets, t)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}

	if len(targets) == 0 {
		return 0, nil
	}

	for _, t := range targets {
		if _, err := tx.ExecContext(ctx, `DELETE FROM notification_states WHERE dedup_key LIKE ? OR dedup_key LIKE ?`, dedupkey.UnregisteredPattern(t.ip), dedupkey.ExpiryPattern(t.ip)); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM ip_records WHERE id = ?`, t.id); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	if s.audit != nil {
		s.audit.Log(ctx, "system", "system", "127.0.0.1", "ip_records.auto_purge", "ip_record", "batch",
			fmt.Sprintf("自動清除未登記且離線超過 %d 天之 IP 紀錄，共 %d 筆", days, len(targets)),
			map[string]any{"purged_count": len(targets), "purge_days": days})
	}

	return len(targets), nil
}

func (s *Service) RegisterRoutes(api chi.Router) {
	api.With(auth.RequireAdmin).Get("/settings/retention", s.handleGetRetention)
	api.With(auth.RequireAdmin).Put("/settings/retention", s.handleUpdateRetention)
}

type retentionPayload struct {
	OfflineGraceRounds         int `json:"offline_grace_rounds"`
	AutoPurgeDays              int `json:"auto_purge_days"`
	RegistrationValidDays      int `json:"registration_valid_days"`
	ExpiryAdvanceDays          int `json:"expiry_advance_days"`
	ExpiryReminderIntervalDays int `json:"expiry_reminder_interval_days"`
	ExpiryMaxReminders         int `json:"expiry_max_reminders"`
}

func (s *Service) handleGetRetention(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	resp := retentionPayload{
		OfflineGraceRounds:         s.settings.GetInt(ctx, "offline_grace_rounds", 2),
		AutoPurgeDays:              s.settings.GetInt(ctx, "auto_purge_days", 90),
		RegistrationValidDays:      s.settings.GetInt(ctx, "registration_valid_days", 365),
		ExpiryAdvanceDays:          s.settings.GetInt(ctx, "expiry_advance_days", 14),
		ExpiryReminderIntervalDays: s.settings.GetInt(ctx, "expiry_reminder_interval_days", 7),
		ExpiryMaxReminders:         s.settings.GetInt(ctx, "expiry_max_reminders", 4),
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Service) handleUpdateRetention(w http.ResponseWriter, r *http.Request) {
	var req retentionPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的設定資料")
		return
	}
	if req.OfflineGraceRounds < 1 || req.AutoPurgeDays < 0 || req.RegistrationValidDays < 1 ||
		req.ExpiryAdvanceDays < 0 || req.ExpiryReminderIntervalDays < 1 || req.ExpiryMaxReminders < 0 {
		auth.WriteError(w, http.StatusUnprocessableEntity, "validation_error", "數值不可為負或小於合理最小值")
		return
	}

	ctx := r.Context()
	pairs := map[string]int{
		"offline_grace_rounds":          req.OfflineGraceRounds,
		"auto_purge_days":               req.AutoPurgeDays,
		"registration_valid_days":       req.RegistrationValidDays,
		"expiry_advance_days":           req.ExpiryAdvanceDays,
		"expiry_reminder_interval_days": req.ExpiryReminderIntervalDays,
		"expiry_max_reminders":          req.ExpiryMaxReminders,
	}
	for k, v := range pairs {
		if err := s.settings.Set(ctx, k, fmt.Sprintf("%d", v)); err != nil {
			auth.WriteError(w, http.StatusInternalServerError, "db_error", "儲存設定失敗")
			return
		}
	}

	u := auth.UserFromContext(ctx)
	if s.audit != nil {
		s.audit.Log(ctx, u.Username, u.Role, r.RemoteAddr, "settings.retention_update", "settings", "retention",
			"更新保存期與自動清除政策設定", map[string]any{"auto_purge_days": req.AutoPurgeDays})
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(req)
}
