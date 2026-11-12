package audit

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jeffhou369/wurzburg/backend/internal/auth"
)

type Handler struct {
	db *sql.DB
}

func NewHandler(db *sql.DB) *Handler {
	return &Handler{db: db}
}

func (h *Handler) RegisterRoutes(api chi.Router) {
	api.With(auth.RequireAdmin).Get("/audit/logs", h.handleListLogs)
}

type auditLogResponse struct {
	ID         int64          `json:"id"`
	Username   string         `json:"username"`
	UserRole   string         `json:"user_role"`
	IPAddress  string         `json:"ip_address"`
	Action     string         `json:"action"`
	TargetType string         `json:"target_type"`
	TargetID   string         `json:"target_id"`
	Summary    string         `json:"summary"`
	Details    map[string]any `json:"details_json"`
	CreatedAt  time.Time      `json:"created_at"`
}

func (h *Handler) handleListLogs(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}

	var totalItems int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM audit_logs`).Scan(&totalItems); err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "查詢稽核紀錄總數失敗")
		return
	}

	offset := (page - 1) * pageSize
	rows, err := h.db.QueryContext(
		r.Context(),
		`SELECT id, username, user_role, ip_address, action, target_type, target_id, summary, details_json, created_at
		 FROM audit_logs
		 ORDER BY id DESC
		 LIMIT ? OFFSET ?`,
		pageSize, offset,
	)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "查詢稽核紀錄失敗")
		return
	}
	defer func() { _ = rows.Close() }()

	items := make([]auditLogResponse, 0)
	for rows.Next() {
		var item auditLogResponse
		var rawDetails sql.NullString
		if err := rows.Scan(
			&item.ID, &item.Username, &item.UserRole, &item.IPAddress,
			&item.Action, &item.TargetType, &item.TargetID, &item.Summary,
			&rawDetails, &item.CreatedAt,
		); err != nil {
			continue
		}
		if rawDetails.Valid && rawDetails.String != "" && rawDetails.String != "null" {
			var decoded map[string]any
			if jsonErr := json.Unmarshal([]byte(rawDetails.String), &decoded); jsonErr == nil {
				item.Details = decoded
			}
		}
		items = append(items, item)
	}

	totalPages := 0
	if totalItems > 0 {
		totalPages = (totalItems + pageSize - 1) / pageSize
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"items": items,
		"pagination": map[string]int{
			"page":        page,
			"page_size":   pageSize,
			"total_items": totalItems,
			"total_pages": totalPages,
		},
	})
}
