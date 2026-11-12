package conflicts

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jeffhou369/wurzburg/backend/internal/audit"
	"github.com/jeffhou369/wurzburg/backend/internal/auth"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
)

type Handler struct {
	db        *sql.DB
	clock     clock.Clock
	audit     *audit.Logger
	ouiLookup interfaces.OUILookup
}

func NewHandler(db *sql.DB, clk clock.Clock, aud *audit.Logger, oui interfaces.OUILookup) *Handler {
	if clk == nil {
		clk = clock.NewRealClock()
	}
	return &Handler{db: db, clock: clk, audit: aud, ouiLookup: oui}
}

func (h *Handler) RegisterRoutes(api chi.Router) {
	api.With(auth.RequireAuth).Get("/conflicts", h.handleList)
	api.With(auth.RequireAuth).Get("/conflicts/{id}", h.handleGet)
	api.With(auth.RequireAdmin).Post("/conflicts/{id}/resolve", h.handleResolve)
}

func (h *Handler) handleList(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	statusFilter := r.URL.Query().Get("status")

	var openCount int
	_ = h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM ip_conflicts WHERE status = 'open'`).Scan(&openCount)

	where := ""
	var args []any
	if statusFilter != "" {
		where = ` WHERE c.status = ?`
		args = append(args, statusFilter)
	}

	var totalItems int
	_ = h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM ip_conflicts c`+where, args...).Scan(&totalItems)

	offset := (page - 1) * pageSize
	query := `SELECT c.id, c.ip_record_id, c.ip_address, c.adopted_mac, c.conflict_macs, c.status, c.resolution,
	                 (r.owner_user_id IS NOT NULL) AS has_owner, COALESCE(u.username, ''),
	                 c.first_detected_at, c.last_detected_at
	          FROM ip_conflicts c
	          JOIN ip_records r ON c.ip_record_id = r.id
	          LEFT JOIN users u ON r.owner_user_id = u.id` + where + ` ORDER BY CASE c.status WHEN 'open' THEN 1 WHEN 'in_progress' THEN 2 ELSE 3 END, c.last_detected_at DESC LIMIT ? OFFSET ?`
	args = append(args, pageSize, offset)

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "查詢衝突清單失敗")
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, ipRecID int64
		var ipAddr, adoptedMAC, macsRaw, status, ownerUsername string
		var resNull sql.NullString
		var hasOwner bool
		var firstDet, lastDet time.Time

		if rows.Scan(&id, &ipRecID, &ipAddr, &adoptedMAC, &macsRaw, &status, &resNull, &hasOwner, &ownerUsername, &firstDet, &lastDet) != nil {
			continue
		}
		var macs []string
		_ = json.Unmarshal([]byte(macsRaw), &macs)
		var resVal any
		if resNull.Valid {
			resVal = resNull.String
		}

		items = append(items, map[string]any{
			"id":                id,
			"ip_record_id":      ipRecID,
			"ip_address":        ipAddr,
			"adopted_mac":       adoptedMAC,
			"conflict_macs":     macs,
			"status":            status,
			"resolution":        resVal,
			"has_owner":         hasOwner,
			"owner_username":    ownerUsername,
			"first_detected_at": firstDet,
			"last_detected_at":  lastDet,
		})
	}

	totalPages := 0
	if totalItems > 0 {
		totalPages = (totalItems + pageSize - 1) / pageSize
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"open_count": openCount,
		"items":      items,
		"pagination": map[string]int{
			"page":        page,
			"page_size":   pageSize,
			"total_items": totalItems,
			"total_pages": totalPages,
		},
	})
}

func (h *Handler) handleGet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的衝突 ID")
		return
	}

	var ipRecID int64
	var ipAddr, adoptedMAC, macsRaw, status, ownerUsername, note, resolvedBy string
	var resNull sql.NullString
	var resolvedAtNull sql.NullTime
	var hasOwner bool
	var firstDet, lastDet time.Time

	err = h.db.QueryRowContext(
		r.Context(),
		`SELECT c.ip_record_id, c.ip_address, c.adopted_mac, c.conflict_macs, c.status, c.resolution,
		        COALESCE(c.note, ''), c.resolved_by, c.resolved_at,
		        (r.owner_user_id IS NOT NULL) AS has_owner, COALESCE(u.username, ''),
		        c.first_detected_at, c.last_detected_at
		 FROM ip_conflicts c
		 JOIN ip_records r ON c.ip_record_id = r.id
		 LEFT JOIN users u ON r.owner_user_id = u.id
		 WHERE c.id = ?`,
		id,
	).Scan(&ipRecID, &ipAddr, &adoptedMAC, &macsRaw, &status, &resNull, &note, &resolvedBy, &resolvedAtNull, &hasOwner, &ownerUsername, &firstDet, &lastDet)

	if errors.Is(err, sql.ErrNoRows) {
		auth.WriteError(w, http.StatusNotFound, "not_found", "找不到衝突紀錄")
		return
	}

	var macs []string
	_ = json.Unmarshal([]byte(macsRaw), &macs)

	sources := make([]map[string]any, 0)
	sRows, err := h.db.QueryContext(
		r.Context(),
		`SELECT s.mac_address, COALESCE(sw.name, ''), s.vrf_name, s.if_name, s.created_at, s.last_seen_at
		 FROM arp_sightings s LEFT JOIN switches sw ON s.switch_id = sw.id
		 WHERE s.ip_record_id = ?`,
		ipRecID,
	)
	if err == nil {
		defer sRows.Close()
		for sRows.Next() {
			var mac, swName, vrfName, ifName string
			var fs, ls time.Time
			if sRows.Scan(&mac, &swName, &vrfName, &ifName, &fs, &ls) == nil {
				sources = append(sources, map[string]any{
					"mac_address":   mac,
					"switch_name":   swName,
					"vrf_name":      vrfName,
					"if_name":       ifName,
					"first_seen_at": fs,
					"last_seen_at":  ls,
				})
			}
		}
	}

	var resVal, resAtVal any
	if resNull.Valid {
		resVal = resNull.String
	}
	if resolvedAtNull.Valid {
		resAtVal = resolvedAtNull.Time
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":                id,
		"ip_record_id":      ipRecID,
		"ip_address":        ipAddr,
		"adopted_mac":       adoptedMAC,
		"conflict_macs":     macs,
		"status":            status,
		"resolution":        resVal,
		"note":              note,
		"resolved_by":       resolvedBy,
		"resolved_at":       resAtVal,
		"has_owner":         hasOwner,
		"owner_username":    ownerUsername,
		"first_detected_at": firstDet,
		"last_detected_at":  lastDet,
		"sources":           sources,
	})
}

func (h *Handler) handleResolve(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的衝突 ID")
		return
	}

	var req struct {
		Resolution string `json:"resolution"`
		AdoptedMAC string `json:"adopted_mac"`
		Note       string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求")
		return
	}

	if req.Resolution != "adopt_new_keep_reg" && req.Resolution != "adopt_new_release_reg" && req.Resolution != "keep_adopted" {
		auth.WriteError(w, http.StatusUnprocessableEntity, "invalid_resolution", "無效的衝突裁決類型")
		return
	}

	ctx := r.Context()
	var ipRecID int64
	var ipAddr, oldMAC, macsRaw, status string
	err = h.db.QueryRowContext(
		ctx,
		`SELECT ip_record_id, ip_address, adopted_mac, conflict_macs, status FROM ip_conflicts WHERE id = ?`,
		id,
	).Scan(&ipRecID, &ipAddr, &oldMAC, &macsRaw, &status)
	if errors.Is(err, sql.ErrNoRows) {
		auth.WriteError(w, http.StatusNotFound, "not_found", "找不到衝突紀錄")
		return
	}
	if status == "resolved" {
		auth.WriteError(w, http.StatusConflict, "already_resolved", "此衝突已經解消，不可重複裁決")
		return
	}

	var macs []string
	_ = json.Unmarshal([]byte(macsRaw), &macs)

	u := auth.UserFromContext(ctx)
	now := h.clock.Now()

	if req.Resolution == "keep_adopted" {
		_, err = h.db.ExecContext(
			ctx,
			`UPDATE ip_conflicts SET status = 'in_progress', resolution = 'keep_adopted', note = ?, resolved_by = ?, updated_at = ? WHERE id = ?`,
			req.Note, u.Username, now, id,
		)
	} else {
		newMAC := strings.ToLower(strings.TrimSpace(req.AdoptedMAC))
		if newMAC == "" {
			// 若未指定，選擇第一個不等於 oldMAC 的衝突 MAC
			for _, m := range macs {
				if m != oldMAC {
					newMAC = m
					break
				}
			}
		}
		if newMAC == "" {
			auth.WriteError(w, http.StatusUnprocessableEntity, "missing_mac", "請指定要採信的新 MAC 位址")
			return
		}

		vendor := "未知"
		if h.ouiLookup != nil {
			vendor = h.ouiLookup.Lookup(newMAC)
		}

		tx, txErr := h.db.BeginTx(ctx, nil)
		if txErr != nil {
			auth.WriteError(w, http.StatusInternalServerError, "db_error", "交易啟動失敗")
			return
		}
		defer func() { _ = tx.Rollback() }()

		if req.Resolution == "adopt_new_keep_reg" {
			_, _ = tx.ExecContext(ctx, `UPDATE ip_records SET mac_address = ?, vendor_name = ?, updated_at = ? WHERE id = ?`, newMAC, vendor, now, ipRecID)
		} else {
			// 採信新 MAC 並釋放登記
			_, _ = tx.ExecContext(
				ctx,
				`UPDATE ip_records SET mac_address = ?, vendor_name = ?, owner_user_id = NULL, owner_mail = '', registration = 'unregistered', expires_at = NULL, updated_at = ? WHERE id = ?`,
				newMAC, vendor, now, ipRecID,
			)
			_, _ = tx.ExecContext(ctx, `DELETE FROM notification_states WHERE dedup_key LIKE CONCAT('unreg|', ?, '|%')`, ipAddr)
		}

		_, err = tx.ExecContext(
			ctx,
			`UPDATE ip_conflicts SET status = 'resolved', resolution = ?, note = ?, resolved_by = ?, resolved_at = ?, updated_at = ? WHERE id = ?`,
			req.Resolution, req.Note, u.Username, now, now, id,
		)
		if err == nil {
			err = tx.Commit()
		}
	}

	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "執行裁決失敗")
		return
	}

	if h.audit != nil && u != nil {
		h.audit.Log(ctx, u.Username, u.Role, r.RemoteAddr, "conflict.resolve", "ip_conflict", fmt.Sprintf("%d", id),
			fmt.Sprintf("裁決衝突 %s (%s)", ipAddr, req.Resolution),
			map[string]any{"before_mac": oldMAC, "after_mac": req.AdoptedMAC, "resolution": req.Resolution, "note": req.Note})
	}

	h.handleGet(w, r)
}
