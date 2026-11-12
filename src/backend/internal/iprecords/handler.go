package iprecords

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jeffhou369/wurzburg/backend/internal/audit"
	"github.com/jeffhou369/wurzburg/backend/internal/auth"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/settings"
	"github.com/jeffhou369/wurzburg/backend/internal/state"
	"github.com/xuri/excelize/v2"
)

type Handler struct {
	db       *sql.DB
	clock    clock.Clock
	settings *settings.Store
	audit    *audit.Logger
}

func NewHandler(db *sql.DB, clk clock.Clock, sett *settings.Store, aud *audit.Logger) *Handler {
	if clk == nil {
		clk = clock.NewRealClock()
	}
	return &Handler{db: db, clock: clk, settings: sett, audit: aud}
}

func (h *Handler) RegisterRoutes(api chi.Router) {
	api.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth)

		r.Get("/ip-records", h.handleList)
		r.Get("/ip-records/export", h.handleExportExcel)
		r.Get("/ip-records/{id}", h.handleGetDetail)
		r.Put("/ip-records/{id}", h.handleUpdateSingle)

		r.Post("/ip-records/actions/register", h.handleBatchRegister)
		r.Post("/ip-records/actions/confirm", h.handleBatchConfirm)
		r.Post("/ip-records/actions/release", h.handleBatchRelease)
		r.Post("/ip-records/actions/reassign", h.handleBatchReassign)
	})
}

type FilterParams struct {
	Search              string `json:"search"`
	SubnetID            int64  `json:"subnet_id"`
	SwitchID            int64  `json:"switch_id"`
	Presence            string `json:"presence"`
	Registration        string `json:"registration"`
	ExpiringSoon        bool   `json:"expiring_soon"`
	PendingConfirmation bool   `json:"pending_confirmation"`
	HasConflict         bool   `json:"has_conflict"`
	OwnerMe             bool   `json:"owner_me"`
	SortBy              string `json:"sort_by"`
	SortDir             string `json:"sort_dir"`
	Page                int    `json:"page"`
	PageSize            int    `json:"page_size"`
}

func parseFiltersFromRequest(r *http.Request) FilterParams {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(q.Get("page_size"))
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	subnetID, _ := strconv.ParseInt(q.Get("subnet_id"), 10, 64)
	switchID, _ := strconv.ParseInt(q.Get("switch_id"), 10, 64)

	return FilterParams{
		Search:              strings.TrimSpace(q.Get("search")),
		SubnetID:            subnetID,
		SwitchID:            switchID,
		Presence:            q.Get("presence"),
		Registration:        q.Get("registration"),
		ExpiringSoon:        q.Get("expiring_soon") == "true",
		PendingConfirmation: q.Get("pending_confirmation") == "true",
		HasConflict:         q.Get("has_conflict") == "true",
		OwnerMe:             q.Get("owner_me") == "true",
		SortBy:              q.Get("sort_by"),
		SortDir:             q.Get("sort_dir"),
		Page:                page,
		PageSize:            pageSize,
	}
}

func (h *Handler) buildWhereClause(f FilterParams, userID int64, now time.Time, advanceDays int) (string, []any) {
	var clauses []string
	var args []any

	if f.Search != "" {
		like := "%" + f.Search + "%"
		clauses = append(clauses, `(r.ip_address LIKE ? OR r.mac_address LIKE ? OR r.owner_mail LIKE ? OR r.hostname LIKE ? OR r.notes LIKE ? OR u.username LIKE ? OR u.display_name LIKE ?)`)
		args = append(args, like, like, like, like, like, like, like)
	}
	if f.SubnetID > 0 {
		clauses = append(clauses, `r.subnet_id = ?`)
		args = append(args, f.SubnetID)
	}
	if f.SwitchID > 0 {
		clauses = append(clauses, `EXISTS (SELECT 1 FROM arp_sightings s WHERE s.ip_record_id = r.id AND s.switch_id = ?)`)
		args = append(args, f.SwitchID)
	}
	if f.Presence == "online" || f.Presence == "offline" {
		clauses = append(clauses, `r.presence = ?`)
		args = append(args, f.Presence)
	}
	switch f.Registration {
	case "unregistered":
		clauses = append(clauses, `r.owner_user_id IS NULL`)
	case "registered":
		clauses = append(clauses, `r.owner_user_id IS NOT NULL AND (r.expires_at IS NULL OR r.expires_at > ?)`)
		args = append(args, now)
	case "expired":
		clauses = append(clauses, `r.owner_user_id IS NOT NULL AND r.expires_at IS NOT NULL AND r.expires_at <= ?`)
		args = append(args, now)
	}
	if f.ExpiringSoon {
		threshold := now.Add(time.Duration(advanceDays) * 24 * time.Hour)
		clauses = append(clauses, `r.owner_user_id IS NOT NULL AND r.expires_at IS NOT NULL AND r.expires_at > ? AND r.expires_at <= ?`)
		args = append(args, now, threshold)
	}
	if f.PendingConfirmation {
		threshold := now.AddDate(0, 0, advanceDays)
		clauses = append(clauses, `r.owner_user_id IS NOT NULL AND r.expires_at IS NOT NULL AND r.expires_at <= ?`)
		args = append(args, threshold)
	}
	if f.HasConflict {
		clauses = append(clauses, `EXISTS (SELECT 1 FROM ip_conflicts c WHERE c.ip_record_id = r.id AND c.status IN ('open', 'in_progress'))`)
	}
	if f.OwnerMe && userID > 0 {
		clauses = append(clauses, `r.owner_user_id = ?`)
		args = append(args, userID)
	}

	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func buildOrderClause(sortBy, sortDir string) string {
	dir := "ASC"
	if strings.EqualFold(sortDir, "desc") {
		dir = "DESC"
	}
	switch sortBy {
	case "last_seen_at":
		return " ORDER BY r.last_seen_at " + dir
	case "expires_at":
		return " ORDER BY r.expires_at " + dir
	case "owner_username":
		return " ORDER BY u.username " + dir
	default:
		return " ORDER BY INET_ATON(r.ip_address) " + dir
	}
}

type ipRecordDTO struct {
	ID               int64      `json:"id"`
	IPAddress        string     `json:"ip_address"`
	MACAddress       string     `json:"mac_address"`
	SubnetID         int64      `json:"subnet_id"`
	SubnetCIDR       string     `json:"subnet_cidr"`
	SwitchNames      []string   `json:"switch_names"`
	IfNames          []string   `json:"if_names"`
	VendorName       string     `json:"vendor_name"`
	DeviceType       string     `json:"device_type"`
	Hostname         string     `json:"hostname"`
	OwnerUsername    string     `json:"owner_username"`
	OwnerDisplayName string     `json:"owner_display_name"`
	OwnerMail        string     `json:"owner_mail"`
	Notes            string     `json:"notes"`
	Presence         string     `json:"presence"`
	Registration     string     `json:"registration"`
	ExpiringSoon     bool       `json:"expiring_soon"`
	HasConflict      bool       `json:"has_conflict"`
	StateLabel       string     `json:"state_label"`
	ExpiresAt        *time.Time `json:"expires_at"`
	FirstSeenAt      time.Time  `json:"first_seen_at"`
	LastSeenAt       time.Time  `json:"last_seen_at"`
}

func (h *Handler) queryRecords(ctx context.Context, f FilterParams, userID int64, paginate bool) ([]ipRecordDTO, int, error) {
	now := h.clock.Now()
	advanceDays := 14
	if h.settings != nil {
		advanceDays = h.settings.GetInt(ctx, "expiry_advance_days", 14)
	}

	whereSQL, args := h.buildWhereClause(f, userID, now, advanceDays)

	var totalItems int
	countSQL := `SELECT COUNT(*) FROM ip_records r LEFT JOIN users u ON r.owner_user_id = u.id` + whereSQL
	if err := h.db.QueryRowContext(ctx, countSQL, args...).Scan(&totalItems); err != nil {
		return nil, 0, err
	}

	querySQL := `SELECT r.id, r.ip_address, r.mac_address, r.subnet_id, sub.cidr,
	                    r.vendor_name, r.device_type, r.hostname,
	                    r.owner_user_id, COALESCE(u.username, ''), COALESCE(u.display_name, ''),
	                    r.owner_mail, COALESCE(r.notes, ''),
	                    r.expires_at, r.first_seen_at, r.last_seen_at,
	                    (SELECT COUNT(*) FROM arp_sightings s WHERE s.ip_record_id = r.id) AS sight_cnt,
	                    EXISTS (SELECT 1 FROM ip_conflicts c WHERE c.ip_record_id = r.id AND c.status IN ('open', 'in_progress')) AS has_conf,
	                    COALESCE((SELECT GROUP_CONCAT(DISTINCT sw.name) FROM arp_sightings s JOIN switches sw ON s.switch_id = sw.id WHERE s.ip_record_id = r.id), ''),
	                    COALESCE((SELECT GROUP_CONCAT(DISTINCT s.if_name) FROM arp_sightings s WHERE s.ip_record_id = r.id AND s.if_name != ''), '')
	             FROM ip_records r
	             JOIN subnets sub ON r.subnet_id = sub.id
	             LEFT JOIN users u ON r.owner_user_id = u.id` + whereSQL + buildOrderClause(f.SortBy, f.SortDir)

	if paginate {
		offset := (f.Page - 1) * f.PageSize
		querySQL += ` LIMIT ? OFFSET ?`
		args = append(args, f.PageSize, offset)
	}

	rows, err := h.db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]ipRecordDTO, 0)
	for rows.Next() {
		var dto ipRecordDTO
		var ownerID sql.NullInt64
		var expAt sql.NullTime
		var sightCnt int
		var swNamesStr, ifNamesStr string

		if err := rows.Scan(
			&dto.ID, &dto.IPAddress, &dto.MACAddress, &dto.SubnetID, &dto.SubnetCIDR,
			&dto.VendorName, &dto.DeviceType, &dto.Hostname,
			&ownerID, &dto.OwnerUsername, &dto.OwnerDisplayName,
			&dto.OwnerMail, &dto.Notes,
			&expAt, &dto.FirstSeenAt, &dto.LastSeenAt,
			&sightCnt, &dto.HasConflict,
			&swNamesStr, &ifNamesStr,
		); err != nil {
			continue
		}

		var expPtr *time.Time
		if expAt.Valid {
			t := expAt.Time
			expPtr = &t
			dto.ExpiresAt = &t
		}

		computed := state.Compute(sightCnt, ownerID.Valid, expPtr, dto.HasConflict, advanceDays, now)
		dto.Presence = string(computed.Presence)
		dto.Registration = string(computed.Registration)
		dto.ExpiringSoon = computed.ExpiringSoon
		dto.StateLabel = computed.Label

		dto.SwitchNames = splitNonEmpty(swNamesStr)
		dto.IfNames = splitNonEmpty(ifNamesStr)

		items = append(items, dto)
	}

	return items, totalItems, nil
}

func splitNonEmpty(s string) []string {
	if s == "" {
		return []string{}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func (h *Handler) handleList(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	f := parseFiltersFromRequest(r)

	items, total, err := h.queryRecords(r.Context(), f, u.ID, true)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "查詢 IP 紀錄失敗")
		return
	}

	totalPages := 0
	if total > 0 {
		totalPages = (total + f.PageSize - 1) / f.PageSize
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"items": items,
		"pagination": map[string]int{
			"page":        f.Page,
			"page_size":   f.PageSize,
			"total_items": total,
			"total_pages": totalPages,
		},
	})
}

func (h *Handler) handleExportExcel(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	f := parseFiltersFromRequest(r)

	items, _, err := h.queryRecords(r.Context(), f, u.ID, false)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "匯出查詢失敗")
		return
	}

	tzName := "Asia/Taipei"
	if h.settings != nil {
		if tz, err := h.settings.Get(r.Context(), "timezone"); err == nil && tz != "" {
			tzName = tz
		}
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc = time.FixedZone("Asia/Taipei", 8*3600)
	}

	xf := excelize.NewFile()
	defer xf.Close()
	sheet := "IP管理表"
	_ = xf.SetSheetName("Sheet1", sheet)

	headers := []string{"狀態", "IP 位址", "MAC 位址", "廠商", "裝置類型", "主機名稱", "網段", "交換器", "介面", "擁有者", "擁有者信箱", "備註", "到期日", "最後見到"}
	for idx, hText := range headers {
		cell, _ := excelize.CoordinatesToCellName(idx+1, 1)
		_ = xf.SetCellStr(sheet, cell, hText)
	}

	for rowIdx, item := range items {
		rNum := rowIdx + 2
		expStr := ""
		if item.ExpiresAt != nil {
			expStr = item.ExpiresAt.In(loc).Format("2006-01-02 15:04") + " " + tzName
		}
		lastSeenStr := item.LastSeenAt.In(loc).Format("2006-01-02 15:04") + " " + tzName

		vals := []string{
			item.StateLabel,
			item.IPAddress,
			item.MACAddress,
			sanitizeExcelCell(item.VendorName),
			sanitizeExcelCell(item.DeviceType),
			sanitizeExcelCell(item.Hostname),
			item.SubnetCIDR,
			sanitizeExcelCell(strings.Join(item.SwitchNames, ", ")),
			sanitizeExcelCell(strings.Join(item.IfNames, ", ")),
			sanitizeExcelCell(item.OwnerUsername),
			sanitizeExcelCell(item.OwnerMail),
			sanitizeExcelCell(item.Notes),
			expStr,
			lastSeenStr,
		}
		for cIdx, v := range vals {
			cell, _ := excelize.CoordinatesToCellName(cIdx+1, rNum)
			_ = xf.SetCellStr(sheet, cell, v)
		}
	}

	filename := fmt.Sprintf("wurzburg-ips-%s.xlsx", h.clock.Now().In(loc).Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)
	_ = xf.Write(w)
}

func sanitizeExcelCell(val string) string {
	if val == "" {
		return ""
	}
	switch val[0] {
	case '=', '+', '-', '@':
		return "'" + val
	default:
		return val
	}
}

func (h *Handler) handleGetDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的 IP 紀錄 ID")
		return
	}

	u := auth.UserFromContext(r.Context())
	now := h.clock.Now()
	advanceDays := 14
	if h.settings != nil {
		advanceDays = h.settings.GetInt(r.Context(), "expiry_advance_days", 14)
	}

	var dto ipRecordDTO
	var ownerID sql.NullInt64
	var expAt sql.NullTime
	var sightCnt int
	var swNamesStr, ifNamesStr string

	err = h.db.QueryRowContext(
		r.Context(),
		`SELECT r.id, r.ip_address, r.mac_address, r.subnet_id, sub.cidr,
		        r.vendor_name, r.device_type, r.hostname,
		        r.owner_user_id, COALESCE(u.username, ''), COALESCE(u.display_name, ''),
		        r.owner_mail, COALESCE(r.notes, ''),
		        r.expires_at, r.first_seen_at, r.last_seen_at,
		        (SELECT COUNT(*) FROM arp_sightings s WHERE s.ip_record_id = r.id),
		        EXISTS (SELECT 1 FROM ip_conflicts c WHERE c.ip_record_id = r.id AND c.status IN ('open', 'in_progress')),
		        COALESCE((SELECT GROUP_CONCAT(DISTINCT sw.name) FROM arp_sightings s JOIN switches sw ON s.switch_id = sw.id WHERE s.ip_record_id = r.id), ''),
		        COALESCE((SELECT GROUP_CONCAT(DISTINCT s.if_name) FROM arp_sightings s WHERE s.ip_record_id = r.id AND s.if_name != ''), '')
		 FROM ip_records r
		 JOIN subnets sub ON r.subnet_id = sub.id
		 LEFT JOIN users u ON r.owner_user_id = u.id
		 WHERE r.id = ?`,
		id,
	).Scan(
		&dto.ID, &dto.IPAddress, &dto.MACAddress, &dto.SubnetID, &dto.SubnetCIDR,
		&dto.VendorName, &dto.DeviceType, &dto.Hostname,
		&ownerID, &dto.OwnerUsername, &dto.OwnerDisplayName,
		&dto.OwnerMail, &dto.Notes,
		&expAt, &dto.FirstSeenAt, &dto.LastSeenAt,
		&sightCnt, &dto.HasConflict,
		&swNamesStr, &ifNamesStr,
	)
	if errors.Is(err, sql.ErrNoRows) {
		auth.WriteError(w, http.StatusNotFound, "not_found", "找不到該筆 IP 紀錄")
		return
	}

	var expPtr *time.Time
	if expAt.Valid {
		t := expAt.Time
		expPtr = &t
		dto.ExpiresAt = &t
	}
	computed := state.Compute(sightCnt, ownerID.Valid, expPtr, dto.HasConflict, advanceDays, now)
	dto.Presence = string(computed.Presence)
	dto.Registration = string(computed.Registration)
	dto.ExpiringSoon = computed.ExpiringSoon
	dto.StateLabel = computed.Label
	dto.SwitchNames = splitNonEmpty(swNamesStr)
	dto.IfNames = splitNonEmpty(ifNamesStr)

	var ownerPtr *int64
	if ownerID.Valid {
		val := ownerID.Int64
		ownerPtr = &val
	}
	canEdit := auth.CanEditRegistration(u, ownerPtr)

	sightings := make([]map[string]any, 0)
	sRows, err := h.db.QueryContext(
		r.Context(),
		`SELECT COALESCE(sw.name, ''), s.vrf_name, s.mac_address, s.if_name, s.arp_type, s.last_seen_at
		 FROM arp_sightings s LEFT JOIN switches sw ON s.switch_id = sw.id
		 WHERE s.ip_record_id = ?`,
		id,
	)
	if err == nil {
		defer sRows.Close()
		for sRows.Next() {
			var sw, vrf, mac, ifn, typ string
			var ls time.Time
			if sRows.Scan(&sw, &vrf, &mac, &ifn, &typ, &ls) == nil {
				sightings = append(sightings, map[string]any{
					"switch_name":  sw,
					"vrf_name":     vrf,
					"mac_address":  mac,
					"if_name":      ifn,
					"arp_type":     typ,
					"last_seen_at": ls,
				})
			}
		}
	}

	events := make([]map[string]any, 0)
	eRows, err := h.db.QueryContext(
		r.Context(),
		`SELECT id, event_type, COALESCE(details, ''), occurred_at FROM ip_events WHERE ip_record_id = ? ORDER BY id DESC LIMIT 50`,
		id,
	)
	if err == nil {
		defer eRows.Close()
		for eRows.Next() {
			var eid int64
			var et, det string
			var occ time.Time
			if eRows.Scan(&eid, &et, &det, &occ) == nil {
				events = append(events, map[string]any{
					"id":          eid,
					"event_type":  et,
					"details":     det,
					"occurred_at": occ,
				})
			}
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":                 dto.ID,
		"ip_address":         dto.IPAddress,
		"mac_address":        dto.MACAddress,
		"subnet_id":          dto.SubnetID,
		"subnet_cidr":        dto.SubnetCIDR,
		"switch_names":       dto.SwitchNames,
		"if_names":           dto.IfNames,
		"vendor_name":        dto.VendorName,
		"device_type":        dto.DeviceType,
		"hostname":           dto.Hostname,
		"owner_username":     dto.OwnerUsername,
		"owner_display_name": dto.OwnerDisplayName,
		"owner_mail":         dto.OwnerMail,
		"notes":              dto.Notes,
		"presence":           dto.Presence,
		"registration":       dto.Registration,
		"expiring_soon":      dto.ExpiringSoon,
		"has_conflict":       dto.HasConflict,
		"state_label":        dto.StateLabel,
		"expires_at":         dto.ExpiresAt,
		"first_seen_at":      dto.FirstSeenAt,
		"last_seen_at":       dto.LastSeenAt,
		"can_edit":           canEdit,
		"sightings":          sightings,
		"events":             events,
	})
}

func validateMail(email string) error {
	trimmed := strings.TrimSpace(email)
	if trimmed == "" {
		return errors.New("擁有者信箱為必填")
	}
	parsed, err := mail.ParseAddress(trimmed)
	if err != nil || !strings.Contains(parsed.Address, "@") || strings.Contains(parsed.Address, " ") {
		return errors.New("擁有者信箱格式不正確")
	}
	parts := strings.Split(parsed.Address, "@")
	if len(parts) != 2 || parts[0] == "" || !strings.Contains(parts[1], ".") {
		return errors.New("擁有者信箱網域格式不正確")
	}
	return nil
}

func (h *Handler) handleUpdateSingle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的 IP 紀錄 ID")
		return
	}

	var req struct {
		OwnerMail  string `json:"owner_mail"`
		DeviceType string `json:"device_type"`
		Hostname   string `json:"hostname"`
		Notes      string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求")
		return
	}
	if err := validateMail(req.OwnerMail); err != nil {
		auth.WriteError(w, http.StatusUnprocessableEntity, "invalid_email", err.Error())
		return
	}

	ctx := r.Context()
	u := auth.UserFromContext(ctx)

	var ipAddr string
	var ownerIDNull sql.NullInt64
	err = h.db.QueryRowContext(ctx, `SELECT ip_address, owner_user_id FROM ip_records WHERE id = ?`, id).Scan(&ipAddr, &ownerIDNull)
	if errors.Is(err, sql.ErrNoRows) {
		auth.WriteError(w, http.StatusNotFound, "not_found", "找不到 IP 紀錄")
		return
	}
	if !ownerIDNull.Valid {
		auth.WriteError(w, http.StatusConflict, "not_registered", "尚未登記的位址請先執行登記動作")
		return
	}

	ownerID := ownerIDNull.Int64
	if !auth.CanEditRegistration(u, &ownerID) {
		auth.WriteError(w, http.StatusForbidden, "forbidden", "僅擁有者或系統管理員可編輯此筆登記")
		return
	}

	_, err = h.db.ExecContext(
		ctx,
		`UPDATE ip_records SET owner_mail = ?, device_type = ?, hostname = ?, notes = ?, updated_at = ? WHERE id = ?`,
		strings.TrimSpace(req.OwnerMail), req.DeviceType, req.Hostname, req.Notes, h.clock.Now(), id,
	)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "更新登記資料失敗")
		return
	}

	if h.audit != nil {
		h.audit.Log(ctx, u.Username, u.Role, r.RemoteAddr, "registration.update", "ip_record", ipAddr,
			fmt.Sprintf("更新 %s 登記備註與聯絡資料", ipAddr), map[string]any{"owner_mail": req.OwnerMail, "device_type": req.DeviceType})
	}

	h.handleGetDetail(w, r)
}

type targetSelector struct {
	IPRecordIDs       []int64       `json:"ip_record_ids"`
	SelectAllFiltered bool          `json:"select_all_filtered"`
	Filter            *FilterParams `json:"filter"`
}

func (h *Handler) resolveTargetIDs(ctx context.Context, sel targetSelector, userID int64) ([]int64, error) {
	if !sel.SelectAllFiltered {
		return sel.IPRecordIDs, nil
	}
	f := FilterParams{}
	if sel.Filter != nil {
		f = *sel.Filter
	}
	items, _, err := h.queryRecords(ctx, f, userID, false)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	return ids, nil
}

type batchItemResult struct {
	IPRecordID int64  `json:"ip_record_id"`
	IPAddress  string `json:"ip_address"`
	Success    bool   `json:"success"`
	Reason     string `json:"reason,omitempty"`
}

func writeBatchResult(w http.ResponseWriter, results []batchItemResult) {
	succ, skip := 0, 0
	for _, res := range results {
		if res.Success {
			succ++
		} else {
			skip++
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"succeeded_count": succ,
		"skipped_count":   skip,
		"results":         results,
	})
}

func (h *Handler) handleBatchRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		targetSelector
		OwnerMail     string `json:"owner_mail"`
		DeviceType    string `json:"device_type"`
		Hostname      string `json:"hostname"`
		Notes         string `json:"notes"`
		OwnerUsername string `json:"owner_username"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求")
		return
	}
	if err := validateMail(req.OwnerMail); err != nil {
		auth.WriteError(w, http.StatusUnprocessableEntity, "invalid_email", err.Error())
		return
	}

	ctx := r.Context()
	u := auth.UserFromContext(ctx)

	targetOwnerID := u.ID
	targetOwnerUsername := u.Username
	if req.OwnerUsername != "" {
		if u.Role != "admin" {
			auth.WriteError(w, http.StatusForbidden, "forbidden", "只有系統管理員可代他人指定擁有者")
			return
		}
		err := h.db.QueryRowContext(ctx, `SELECT id, username FROM users WHERE username = ? AND enabled = TRUE`, req.OwnerUsername).Scan(&targetOwnerID, &targetOwnerUsername)
		if err != nil {
			auth.WriteError(w, http.StatusUnprocessableEntity, "target_user_not_found", "指定的擁有者帳號不存在")
			return
		}
	}

	ids, err := h.resolveTargetIDs(ctx, req.targetSelector, u.ID)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "取得對象失敗")
		return
	}

	validDays := 365
	if h.settings != nil {
		validDays = h.settings.GetInt(ctx, "registration_valid_days", 365)
	}
	now := h.clock.Now()
	expiresAt := now.AddDate(0, 0, validDays)
	isBatch := len(ids) > 1 || req.SelectAllFiltered

	results := make([]batchItemResult, 0, len(ids))
	for _, id := range ids {
		var ipAddr string
		var ownerNull sql.NullInt64
		err := h.db.QueryRowContext(ctx, `SELECT ip_address, owner_user_id FROM ip_records WHERE id = ?`, id).Scan(&ipAddr, &ownerNull)
		if err != nil {
			results = append(results, batchItemResult{IPRecordID: id, Success: false, Reason: "找不到紀錄"})
			continue
		}
		if ownerNull.Valid {
			results = append(results, batchItemResult{IPRecordID: id, IPAddress: ipAddr, Success: false, Reason: "此位址已有擁有者，不可重複登記"})
			continue
		}

		_, err = h.db.ExecContext(
			ctx,
			`UPDATE ip_records SET owner_user_id = ?, owner_mail = ?, device_type = ?, hostname = ?, notes = ?, registration = 'registered', registered_at = ?, confirmed_at = ?, expires_at = ?, updated_at = ? WHERE id = ?`,
			targetOwnerID, strings.TrimSpace(req.OwnerMail), req.DeviceType, req.Hostname, req.Notes, now, now, expiresAt, now, id,
		)
		if err != nil {
			results = append(results, batchItemResult{IPRecordID: id, IPAddress: ipAddr, Success: false, Reason: "資料庫更新失敗"})
			continue
		}

		// 解除此位址未登記去重狀態
		_, _ = h.db.ExecContext(ctx, `DELETE FROM notification_states WHERE dedup_key LIKE CONCAT('unreg|', ?, '|%')`, ipAddr)

		if h.audit != nil {
			h.audit.Log(ctx, u.Username, u.Role, r.RemoteAddr, "registration.create", "ip_record", ipAddr,
				fmt.Sprintf("登記位址 %s 予 %s", ipAddr, targetOwnerUsername),
				map[string]any{"batch": isBatch, "owner": targetOwnerUsername, "owner_mail": req.OwnerMail})
		}
		results = append(results, batchItemResult{IPRecordID: id, IPAddress: ipAddr, Success: true})
	}

	writeBatchResult(w, results)
}

func (h *Handler) handleBatchConfirm(w http.ResponseWriter, r *http.Request) {
	var req targetSelector
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求")
		return
	}

	ctx := r.Context()
	u := auth.UserFromContext(ctx)
	ids, err := h.resolveTargetIDs(ctx, req, u.ID)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "取得對象失敗")
		return
	}

	validDays := 365
	if h.settings != nil {
		validDays = h.settings.GetInt(ctx, "registration_valid_days", 365)
	}
	now := h.clock.Now()
	expiresAt := now.AddDate(0, 0, validDays)
	isBatch := len(ids) > 1 || req.SelectAllFiltered

	results := make([]batchItemResult, 0, len(ids))
	for _, id := range ids {
		var ipAddr string
		var ownerNull sql.NullInt64
		err := h.db.QueryRowContext(ctx, `SELECT ip_address, owner_user_id FROM ip_records WHERE id = ?`, id).Scan(&ipAddr, &ownerNull)
		if err != nil {
			results = append(results, batchItemResult{IPRecordID: id, Success: false, Reason: "找不到紀錄"})
			continue
		}
		if !ownerNull.Valid {
			results = append(results, batchItemResult{IPRecordID: id, IPAddress: ipAddr, Success: false, Reason: "尚未登記之位址無法重新確認"})
			continue
		}
		ownerID := ownerNull.Int64
		if !auth.CanEditRegistration(u, &ownerID) {
			results = append(results, batchItemResult{IPRecordID: id, IPAddress: ipAddr, Success: false, Reason: "無權限重新確認他人之登記"})
			continue
		}

		err = h.changeRegistrationTx(ctx, id, u, r.RemoteAddr, "registration.confirm", func(tx *sql.Tx, ip string) (string, map[string]any, error) {
			if _, err := tx.ExecContext(ctx, `UPDATE ip_records SET registration = 'registered', confirmed_at = ?, expires_at = ?, updated_at = ? WHERE id = ?`, now, expiresAt, now, id); err != nil {
				return "", nil, err
			}
			// 清除舊到期日的催認狀態，使新週期重新從第一輪開始催認
			if _, err := tx.ExecContext(ctx, `DELETE FROM notification_states WHERE dedup_key LIKE CONCAT('expiry|', ?, '|%')`, ip); err != nil {
				return "", nil, err
			}
			return fmt.Sprintf("重新確認位址 %s，到期日延長至 %s", ip, expiresAt.Format("2006-01-02")), map[string]any{"batch": isBatch}, nil
		})
		if err != nil {
			results = append(results, batchItemResult{IPRecordID: id, IPAddress: ipAddr, Success: false, Reason: "重新確認未完成，資料已保留，請稍後重試"})
			continue
		}
		results = append(results, batchItemResult{IPRecordID: id, IPAddress: ipAddr, Success: true})
	}

	writeBatchResult(w, results)
}

func (h *Handler) handleBatchRelease(w http.ResponseWriter, r *http.Request) {
	var req targetSelector
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求")
		return
	}

	ctx := r.Context()
	u := auth.UserFromContext(ctx)
	ids, err := h.resolveTargetIDs(ctx, req, u.ID)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "取得對象失敗")
		return
	}

	now := h.clock.Now()
	isBatch := len(ids) > 1 || req.SelectAllFiltered

	results := make([]batchItemResult, 0, len(ids))
	for _, id := range ids {
		var ipAddr string
		var ownerNull sql.NullInt64
		err := h.db.QueryRowContext(ctx, `SELECT ip_address, owner_user_id FROM ip_records WHERE id = ?`, id).Scan(&ipAddr, &ownerNull)
		if err != nil {
			results = append(results, batchItemResult{IPRecordID: id, Success: false, Reason: "找不到紀錄"})
			continue
		}
		if !ownerNull.Valid {
			results = append(results, batchItemResult{IPRecordID: id, IPAddress: ipAddr, Success: false, Reason: "此位址目前尚未登記"})
			continue
		}
		ownerID := ownerNull.Int64
		if !auth.CanEditRegistration(u, &ownerID) {
			results = append(results, batchItemResult{IPRecordID: id, IPAddress: ipAddr, Success: false, Reason: "無權限釋放他人之登記"})
			continue
		}

		err = h.changeRegistrationTx(ctx, id, u, r.RemoteAddr, "registration.release", func(tx *sql.Tx, ip string) (string, map[string]any, error) {
			if _, err := tx.ExecContext(ctx,
				`UPDATE ip_records SET owner_user_id = NULL, owner_mail = '', registration = 'unregistered', expires_at = NULL, updated_at = ? WHERE id = ?`,
				now, id); err != nil {
				return "", nil, err
			}
			// 清除到期催認狀態
			if _, err := tx.ExecContext(ctx, `DELETE FROM notification_states WHERE dedup_key LIKE CONCAT('expiry|', ?, '|%')`, ip); err != nil {
				return "", nil, err
			}
			return fmt.Sprintf("釋放位址 %s 登記", ip), map[string]any{"batch": isBatch}, nil
		})
		if err != nil {
			results = append(results, batchItemResult{IPRecordID: id, IPAddress: ipAddr, Success: false, Reason: "釋放未完成，資料已保留，請稍後重試"})
			continue
		}
		results = append(results, batchItemResult{IPRecordID: id, IPAddress: ipAddr, Success: true})
	}

	writeBatchResult(w, results)
}

func (h *Handler) handleBatchReassign(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := auth.UserFromContext(ctx)
	if u.Role != "admin" {
		auth.WriteError(w, http.StatusForbidden, "forbidden", "僅限系統管理員執行改派操作")
		return
	}

	var req struct {
		targetSelector
		TargetUsername string `json:"target_username"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TargetUsername == "" {
		auth.WriteError(w, http.StatusUnprocessableEntity, "validation_error", "必須指定改派之目標帳號")
		return
	}

	var targetID int64
	var targetMail string
	err := h.db.QueryRowContext(ctx, `SELECT id, mail FROM users WHERE username = ? AND enabled = TRUE`, req.TargetUsername).Scan(&targetID, &targetMail)
	if err != nil {
		auth.WriteError(w, http.StatusUnprocessableEntity, "target_user_not_found", "指定的目標擁有者帳號不存在或已停用")
		return
	}

	ids, err := h.resolveTargetIDs(ctx, req.targetSelector, u.ID)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "取得對象失敗")
		return
	}

	now := h.clock.Now()
	isBatch := len(ids) > 1 || req.SelectAllFiltered
	results := make([]batchItemResult, 0, len(ids))

	for _, id := range ids {
		var ipAddr string
		var ownerNull sql.NullInt64
		err := h.db.QueryRowContext(ctx, `SELECT ip_address, owner_user_id FROM ip_records WHERE id = ?`, id).Scan(&ipAddr, &ownerNull)
		if err != nil {
			results = append(results, batchItemResult{IPRecordID: id, Success: false, Reason: "找不到紀錄"})
			continue
		}
		if !ownerNull.Valid {
			results = append(results, batchItemResult{IPRecordID: id, IPAddress: ipAddr, Success: false, Reason: "未登記之位址無法改派，請使用登記功能"})
			continue
		}

		if err := h.reassignOne(ctx, id, targetID, targetMail, req.TargetUsername, u, r.RemoteAddr, now, isBatch); err != nil {
			results = append(results, batchItemResult{IPRecordID: id, IPAddress: ipAddr, Success: false, Reason: "改派未完成，資料已保留，請稍後重試"})
			continue
		}
		results = append(results, batchItemResult{IPRecordID: id, IPAddress: ipAddr, Success: true})
	}

	writeBatchResult(w, results)
}
