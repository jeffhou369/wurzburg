package dashboard

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jeffhou369/wurzburg/backend/internal/auth"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/scheduler"
)

type Handler struct {
	db        *sql.DB
	clock     clock.Clock
	scheduler *scheduler.Scheduler
}

func NewHandler(db *sql.DB, clk clock.Clock, sched *scheduler.Scheduler) *Handler {
	if clk == nil {
		clk = clock.NewRealClock()
	}
	return &Handler{db: db, clock: clk, scheduler: sched}
}

func (h *Handler) RegisterRoutes(api chi.Router) {
	api.With(auth.RequireAuth).Get("/dashboard/stats", h.handleGetStats)
}

func (h *Handler) handleGetStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := h.clock.Now()

	var managedTotal, onlineCount, offlineCount int
	var unregOnlineCount, expiredCount int

	if h.db != nil {
		_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ip_records`).Scan(&managedTotal)
		_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ip_records WHERE presence = 'online'`).Scan(&onlineCount)
		offlineCount = managedTotal - onlineCount
		_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ip_records WHERE presence = 'online' AND owner_user_id IS NULL`).Scan(&unregOnlineCount)
		_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ip_records WHERE owner_user_id IS NOT NULL AND expires_at IS NOT NULL AND expires_at <= ?`, now).Scan(&expiredCount)
	}

	var openConflicts int
	if h.db != nil {
		_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ip_conflicts WHERE status = 'open'`).Scan(&openConflicts)
	}

	var switchesCount int
	if h.db != nil {
		_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM switches WHERE enabled = TRUE`).Scan(&switchesCount)
	}

	var inProg, incomplete bool
	var lastSync *time.Time
	lastSuccess := false
	if h.scheduler != nil {
		inProg, incomplete, lastSync, lastSuccess = h.scheduler.Status()
	}

	var failedVrfCount int
	if h.db != nil {
		_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vrf_read_configs WHERE enabled = TRUE AND last_status = 'failed'`).Scan(&failedVrfCount)
	}
	// 單一 VRF 讀取失敗不會讓整輪回傳錯誤，但仍不能顯示為讀取成功。
	if failedVrfCount > 0 {
		lastSuccess = false
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"managed_total":             managedTotal,
		"online_count":              onlineCount,
		"offline_count":             offlineCount,
		"unregistered_online_count": unregOnlineCount,
		"expired_count":             expiredCount,
		"open_conflicts_count":      openConflicts,
		"switches_count":            switchesCount,
		"last_sync_at":              lastSync,
		"last_sync_success":         lastSuccess,
		"failed_vrf_count":          failedVrfCount,
		"sync_in_progress":          inProg,
		"round_incomplete":          incomplete,
	})
}
