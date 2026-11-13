package subnets

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/jeffhou369/wurzburg/backend/internal/auth"
)

func (s *ScopeService) handleListSubnets(w http.ResponseWriter, r *http.Request) {
	stateFilter := r.URL.Query().Get("state")

	var pendingCount int
	if err := s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM subnets WHERE state = 'pending'`).Scan(&pendingCount); err != nil {
		writeScopeError(w, r, err, "查詢網段失敗")
		return
	}

	query := `SELECT s.id, s.cidr, s.state, s.exclude_reason, COALESCE(s.exclude_comment, ''), s.is_discovered, s.source_missing, s.description,
	                 (SELECT COUNT(*) FROM ip_records r WHERE r.subnet_id = s.id) AS ip_count,
	                 (SELECT COUNT(*) FROM ip_records r WHERE r.subnet_id = s.id AND r.presence = 'online') AS online_count
	          FROM subnets s`
	var args []any
	if stateFilter != "" {
		query += ` WHERE s.state = ?`
		args = append(args, stateFilter)
	}
	query += ` ORDER BY s.id ASC`

	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeScopeError(w, r, err, "查詢網段失敗")
		return
	}
	defer func() { _ = rows.Close() }()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var cidr, state, exComment, desc string
		var exReason sql.NullString
		var isDisc, srcMissing bool
		var ipCount, onlineCount int
		if err := rows.Scan(&id, &cidr, &state, &exReason, &exComment, &isDisc, &srcMissing, &desc, &ipCount, &onlineCount); err != nil {
			writeScopeError(w, r, err, "查詢網段失敗")
			return
		}
		var er any
		if exReason.Valid {
			er = exReason.String
		}
		items = append(items, map[string]any{
			"id":              id,
			"cidr":            cidr,
			"state":           state,
			"exclude_reason":  er,
			"exclude_comment": exComment,
			"is_discovered":   isDisc,
			"source_missing":  srcMissing,
			"description":     desc,
			"ip_count":        ipCount,
			"online_count":    onlineCount,
		})
	}
	if err := rows.Err(); err != nil {
		writeScopeError(w, r, err, "查詢網段失敗")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"pending_count": pendingCount,
		"items":         items,
	})
}

// loadSubnetView 讀取單一網段的完整檢視（含閘道與排除範圍）。找不到網段回傳 errSubnetNotFound。
func (s *ScopeService) loadSubnetView(ctx context.Context, id int64) (map[string]any, error) {
	var cidr, state, exComment, desc string
	var exReason sql.NullString
	var isDisc, srcMissing bool
	err := s.db.QueryRowContext(
		ctx,
		`SELECT cidr, state, exclude_reason, COALESCE(exclude_comment, ''), is_discovered, source_missing, description FROM subnets WHERE id = ?`,
		id,
	).Scan(&cidr, &state, &exReason, &exComment, &isDisc, &srcMissing, &desc)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errSubnetNotFound()
	}
	if err != nil {
		return nil, fmt.Errorf("查詢網段失敗: %w", err)
	}

	var ipCount, onlineCount int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(CASE WHEN presence='online' THEN 1 ELSE 0 END), 0) FROM ip_records WHERE subnet_id = ?`, id,
	).Scan(&ipCount, &onlineCount); err != nil {
		return nil, fmt.Errorf("統計 IP 紀錄失敗: %w", err)
	}

	gateways, err := s.loadGatewayView(ctx, id)
	if err != nil {
		return nil, err
	}
	exclusions, err := s.loadExclusionView(ctx, id)
	if err != nil {
		return nil, err
	}

	var er any
	if exReason.Valid {
		er = exReason.String
	}
	return map[string]any{
		"id":              id,
		"cidr":            cidr,
		"state":           state,
		"exclude_reason":  er,
		"exclude_comment": exComment,
		"is_discovered":   isDisc,
		"source_missing":  srcMissing,
		"description":     desc,
		"ip_count":        ipCount,
		"online_count":    onlineCount,
		"gateways":        gateways,
		"exclusions":      exclusions,
	}, nil
}

func (s *ScopeService) loadGatewayView(ctx context.Context, id int64) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT g.ip_address, g.if_name, COALESCE(sw.name, ''), g.vrf_name
		 FROM subnet_gateways g LEFT JOIN switches sw ON g.switch_id = sw.id
		 WHERE g.subnet_id = ? ORDER BY g.ip_address ASC`,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("查詢閘道位址失敗: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]map[string]any, 0)
	for rows.Next() {
		var ip, ifn, swn, vrf string
		if err := rows.Scan(&ip, &ifn, &swn, &vrf); err != nil {
			return nil, fmt.Errorf("讀取閘道位址失敗: %w", err)
		}
		out = append(out, map[string]any{
			"ip_address":  ip,
			"if_name":     ifn,
			"switch_name": swn,
			"vrf_name":    vrf,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("迭代閘道位址失敗: %w", err)
	}
	return out, nil
}

func (s *ScopeService) loadExclusionView(ctx context.Context, id int64) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, start_ip, end_ip, reason, COALESCE(comment, '') FROM exclusion_ranges WHERE subnet_id = ? ORDER BY id ASC`,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("查詢排除範圍失敗: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]map[string]any, 0)
	for rows.Next() {
		var eid int64
		var sip, eip, reason, cmt string
		if err := rows.Scan(&eid, &sip, &eip, &reason, &cmt); err != nil {
			return nil, fmt.Errorf("讀取排除範圍失敗: %w", err)
		}
		out = append(out, map[string]any{
			"id":       eid,
			"start_ip": sip,
			"end_ip":   eip,
			"reason":   reason,
			"comment":  cmt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("迭代排除範圍失敗: %w", err)
	}
	return out, nil
}

func (s *ScopeService) handleGetSubnet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的網段 ID")
		return
	}
	s.writeSubnetView(w, r, id, http.StatusOK)
}

func (s *ScopeService) writeSubnetView(w http.ResponseWriter, r *http.Request, id int64, status int) {
	view, err := s.loadSubnetView(r.Context(), id)
	if err != nil {
		writeScopeError(w, r, err, "查詢網段失敗")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(view)
}

func (s *ScopeService) handleListUndetermined(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(
		r.Context(),
		`SELECT u.ip_address, u.mac_address, COALESCE(sw.name, ''), u.vrf_name, u.if_name, u.last_seen_at
		 FROM undetermined_prefixes u LEFT JOIN switches sw ON u.switch_id = sw.id
		 ORDER BY u.last_seen_at DESC`,
	)
	if err != nil {
		writeScopeError(w, r, err, "查詢未定前綴失敗")
		return
	}
	defer func() { _ = rows.Close() }()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var ip, mac, swName, vrf, ifn string
		var lastSeen any
		if err := rows.Scan(&ip, &mac, &swName, &vrf, &ifn, &lastSeen); err != nil {
			writeScopeError(w, r, err, "查詢未定前綴失敗")
			return
		}
		items = append(items, map[string]any{
			"ip_address":   ip,
			"mac_address":  mac,
			"switch_name":  swName,
			"vrf_name":     vrf,
			"if_name":      ifn,
			"last_seen_at": lastSeen,
		})
	}
	if err := rows.Err(); err != nil {
		writeScopeError(w, r, err, "查詢未定前綴失敗")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
}

// ---------------------------------------------------------------------------
// 新增網段
// ---------------------------------------------------------------------------

func (s *ScopeService) handleCreateSubnet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CIDR        string `json:"cidr"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CIDR == "" {
		auth.WriteError(w, http.StatusUnprocessableEntity, "validation_error", "請提供 CIDR")
		return
	}

	ip, ipNet, err := net.ParseCIDR(strings.TrimSpace(req.CIDR))
	if err != nil || ip.To4() == nil {
		auth.WriteError(w, http.StatusUnprocessableEntity, "invalid_cidr", "CIDR 格式無效，必須為 IPv4 網段")
		return
	}
	canonicalCIDR := ipNet.String()

	var id int64
	err = s.runScopeTx(r.Context(), func(tx *sql.Tx, _ []scopeRow) error {
		now := s.clock.Now()
		res, err := tx.ExecContext(
			r.Context(),
			`INSERT INTO subnets (cidr, state, is_discovered, source_missing, description, created_at, updated_at)
			 VALUES (?, 'managed', FALSE, FALSE, ?, ?, ?)`,
			canonicalCIDR, req.Description, now, now,
		)
		if isDuplicateEntry(err) {
			return newScopeError(http.StatusUnprocessableEntity, "duplicate_cidr", "該 CIDR 網段已存在")
		}
		if err != nil {
			return fmt.Errorf("新增網段失敗: %w", err)
		}
		id, err = res.LastInsertId()
		if err != nil {
			return fmt.Errorf("取得新網段 ID 失敗: %w", err)
		}
		return s.auditTx(r.Context(), tx, r, "subnet.create_manual", "subnet", strconv.FormatInt(id, 10),
			fmt.Sprintf("手動新增網段 %s", canonicalCIDR),
			map[string]any{"subnet_id": id, "cidr": canonicalCIDR})
	})
	if err != nil {
		writeScopeError(w, r, err, "新增網段失敗")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "cidr": canonicalCIDR, "state": StateManaged})
}

// ---------------------------------------------------------------------------
// 修正 CIDR
// ---------------------------------------------------------------------------

func parseSubnetID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的網段 ID")
		return 0, false
	}
	return id, true
}
