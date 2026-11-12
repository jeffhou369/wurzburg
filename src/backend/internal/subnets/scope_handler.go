package subnets

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jeffhou369/wurzburg/backend/internal/audit"
	"github.com/jeffhou369/wurzburg/backend/internal/auth"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
)

// ScopeService 實作網段發現、狀態管理、排除範圍與刪除預覽確認流程（Ticket 12 & 24）。
type ScopeService struct {
	db     *sql.DB
	clock  clock.Clock
	audit  *audit.Logger
	secret []byte
}

func NewScopeService(db *sql.DB, clk clock.Clock, aud *audit.Logger, previewSecret string) *ScopeService {
	if clk == nil {
		clk = clock.NewRealClock()
	}
	if previewSecret == "" {
		previewSecret = "wurzburg_preview_token_hmac_default"
	}
	return &ScopeService{
		db:     db,
		clock:  clk,
		audit:  aud,
		secret: []byte(previewSecret),
	}
}

func (s *ScopeService) RegisterRoutes(api chi.Router) {
	api.With(auth.RequireAuth).Get("/subnets", s.handleListSubnets)
	api.With(auth.RequireAuth).Get("/subnets/undetermined", s.handleListUndetermined)
	api.With(auth.RequireAuth).Get("/subnets/{id}", s.handleGetSubnet)

	api.Group(func(r chi.Router) {
		r.Use(auth.RequireAdmin)
		r.Post("/subnets", s.handleCreateSubnet)
		r.Put("/subnets/{id}", s.handleUpdateSubnet)
		r.Post("/subnets/{id}/state/preview", s.handlePreviewStateChange)
		r.Put("/subnets/{id}/state", s.handleChangeState)
		r.Post("/subnets/{id}/exclusions/preview", s.handlePreviewExclusion)
		r.Post("/subnets/{id}/exclusions", s.handleCreateExclusion)
		r.Delete("/subnets/{id}/exclusions/{exclusionId}", s.handleDeleteExclusion)
	})
}

// ApplyInterfaceAddresses 實作 pipeline.SubnetScopeProcessor，從採集到的介面位址表發現網段與閘道位址。
func (s *ScopeService) ApplyInterfaceAddresses(ctx context.Context, outputs []*interfaces.PollOutput) error {
	if s.db == nil || len(outputs) == 0 {
		return nil
	}

	now := s.clock.Now()
	seenCIDRs := make(map[string]bool)

	for _, out := range outputs {
		if out == nil {
			continue
		}
		for _, addr := range out.InterfaceAddresses {
			cidr, err := toNetworkCIDR(addr.IPAddress, addr.NetMask)
			if err != nil {
				continue
			}
			seenCIDRs[cidr] = true

			// 尋找或建立網段（新發現者預設為 pending 待檢視）
			var subnetID int64
			err = s.db.QueryRowContext(ctx, `SELECT id FROM subnets WHERE cidr = ?`, cidr).Scan(&subnetID)
			if errors.Is(err, sql.ErrNoRows) {
				res, insErr := s.db.ExecContext(
					ctx,
					`INSERT INTO subnets (cidr, state, is_discovered, source_missing, created_at, updated_at)
					 VALUES (?, 'pending', TRUE, FALSE, ?, ?)`,
					cidr, now, now,
				)
				if insErr != nil {
					continue
				}
				subnetID, _ = res.LastInsertId()
			} else if err == nil {
				_, _ = s.db.ExecContext(ctx, `UPDATE subnets SET source_missing = FALSE, updated_at = ? WHERE id = ?`, now, subnetID)
			}

			if subnetID > 0 {
				_, _ = s.db.ExecContext(
					ctx,
					`INSERT INTO subnet_gateways (subnet_id, ip_address, if_index, if_name, switch_id, vrf_name, created_at)
					 VALUES (?, ?, ?, ?, ?, ?, ?)
					 ON DUPLICATE KEY UPDATE if_index = VALUES(if_index), if_name = VALUES(if_name)`,
					subnetID, addr.IPAddress, addr.IfIndex, addr.IfName, out.SwitchID, out.VRFName, now,
				)
			}
		}
	}

	// 標記來源已消失的發現網段（僅在本次確實有成功的讀取輸出時比對）
	if len(seenCIDRs) > 0 {
		rows, err := s.db.QueryContext(ctx, `SELECT id, cidr FROM subnets WHERE is_discovered = TRUE AND source_missing = FALSE`)
		if err == nil {
			var missingIDs []int64
			for rows.Next() {
				var id int64
				var cidr string
				if rows.Scan(&id, &cidr) == nil && !seenCIDRs[cidr] {
					missingIDs = append(missingIDs, id)
				}
			}
			_ = rows.Close()
			for _, id := range missingIDs {
				_, _ = s.db.ExecContext(ctx, `UPDATE subnets SET source_missing = TRUE, updated_at = ? WHERE id = ?`, now, id)
			}
		}
	}

	return nil
}

func toNetworkCIDR(ipStr, maskStr string) (string, error) {
	ip := net.ParseIP(ipStr).To4()
	if ip == nil {
		return "", errors.New("invalid IPv4")
	}
	maskIP := net.ParseIP(maskStr).To4()
	if maskIP == nil {
		return "", errors.New("invalid NetMask")
	}
	mask := net.IPv4Mask(maskIP[0], maskIP[1], maskIP[2], maskIP[3])
	ones, bits := mask.Size()
	if bits != 32 || (ones == 0 && maskStr != "0.0.0.0") {
		return "", errors.New("non-contiguous mask")
	}
	if ones == 32 {
		return "", errors.New("ignore /32 host loopback")
	}
	ipNet := &net.IPNet{IP: ip.Mask(mask), Mask: mask}
	return ipNet.String(), nil
}

func (s *ScopeService) handleListSubnets(w http.ResponseWriter, r *http.Request) {
	stateFilter := r.URL.Query().Get("state")

	var pendingCount int
	_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM subnets WHERE state = 'pending'`).Scan(&pendingCount)

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
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "查詢網段失敗")
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var cidr, state, exComment, desc string
		var exReason sql.NullString
		var isDisc, srcMissing bool
		var ipCount, onlineCount int
		if rows.Scan(&id, &cidr, &state, &exReason, &exComment, &isDisc, &srcMissing, &desc, &ipCount, &onlineCount) != nil {
			continue
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

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"pending_count": pendingCount,
		"items":         items,
	})
}

func (s *ScopeService) handleGetSubnet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的網段 ID")
		return
	}

	var cidr, state, exComment, desc string
	var exReason sql.NullString
	var isDisc, srcMissing bool
	err = s.db.QueryRowContext(
		r.Context(),
		`SELECT cidr, state, exclude_reason, COALESCE(exclude_comment, ''), is_discovered, source_missing, description FROM subnets WHERE id = ?`,
		id,
	).Scan(&cidr, &state, &exReason, &exComment, &isDisc, &srcMissing, &desc)
	if errors.Is(err, sql.ErrNoRows) {
		auth.WriteError(w, http.StatusNotFound, "not_found", "找不到該網段")
		return
	}

	var ipCount, onlineCount int
	_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*), COALESCE(SUM(CASE WHEN presence='online' THEN 1 ELSE 0 END), 0) FROM ip_records WHERE subnet_id = ?`, id).Scan(&ipCount, &onlineCount)

	gateways := make([]map[string]any, 0)
	gwRows, err := s.db.QueryContext(
		r.Context(),
		`SELECT g.ip_address, g.if_name, COALESCE(sw.name, ''), g.vrf_name
		 FROM subnet_gateways g LEFT JOIN switches sw ON g.switch_id = sw.id
		 WHERE g.subnet_id = ? ORDER BY g.ip_address ASC`,
		id,
	)
	if err == nil {
		defer gwRows.Close()
		for gwRows.Next() {
			var ip, ifn, swn, vrf string
			if gwRows.Scan(&ip, &ifn, &swn, &vrf) == nil {
				gateways = append(gateways, map[string]any{
					"ip_address":  ip,
					"if_name":     ifn,
					"switch_name": swn,
					"vrf_name":    vrf,
				})
			}
		}
	}

	exclusions := make([]map[string]any, 0)
	exRows, err := s.db.QueryContext(
		r.Context(),
		`SELECT id, start_ip, end_ip, reason, COALESCE(comment, '') FROM exclusion_ranges WHERE subnet_id = ? ORDER BY id ASC`,
		id,
	)
	if err == nil {
		defer exRows.Close()
		for exRows.Next() {
			var eid int64
			var sip, eip, reason, cmt string
			if exRows.Scan(&eid, &sip, &eip, &reason, &cmt) == nil {
				exclusions = append(exclusions, map[string]any{
					"id":       eid,
					"start_ip": sip,
					"end_ip":   eip,
					"reason":   reason,
					"comment":  cmt,
				})
			}
		}
	}

	var er any
	if exReason.Valid {
		er = exReason.String
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
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
	})
}

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

	now := s.clock.Now()
	res, err := s.db.ExecContext(
		r.Context(),
		`INSERT INTO subnets (cidr, state, is_discovered, source_missing, description, created_at, updated_at)
		 VALUES (?, 'managed', FALSE, FALSE, ?, ?, ?)`,
		canonicalCIDR, req.Description, now, now,
	)
	if err != nil {
		auth.WriteError(w, http.StatusUnprocessableEntity, "duplicate_cidr", "該 CIDR 網段已存在")
		return
	}
	id, _ := res.LastInsertId()

	u := auth.UserFromContext(r.Context())
	if s.audit != nil && u != nil {
		s.audit.Log(r.Context(), u.Username, u.Role, r.RemoteAddr, "subnet.create_manual", "subnet", fmt.Sprintf("%d", id),
			fmt.Sprintf("手動新增網段 %s", canonicalCIDR), nil)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "cidr": canonicalCIDR, "state": "managed"})
}

func (s *ScopeService) handleUpdateSubnet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的網段 ID")
		return
	}

	var req struct {
		CIDR         string `json:"cidr"`
		Description  string `json:"description"`
		PreviewToken string `json:"preview_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求格式")
		return
	}

	_, newNet, err := net.ParseCIDR(strings.TrimSpace(req.CIDR))
	if err != nil || newNet.IP.To4() == nil {
		auth.WriteError(w, http.StatusUnprocessableEntity, "invalid_cidr", "CIDR 格式無效")
		return
	}
	canonical := newNet.String()

	// 計算修改 CIDR 後不再屬於此網段的既有 IP 紀錄
	orphanedIDs, ownedDetails := s.findRecordsOutsideCIDR(r.Context(), id, newNet)
	if len(orphanedIDs) > 0 {
		expectedToken := s.signPreviewToken(fmt.Sprintf("cidr:%d:%s:%d:%d", id, canonical, len(orphanedIDs), len(ownedDetails)))
		if req.PreviewToken != expectedToken {
			auth.WriteError(w, http.StatusConflict, "confirmation_required", fmt.Sprintf("修正 CIDR 將刪除 %d 筆 IP 紀錄（其中 %d 筆已登記），請先預覽並確認", len(orphanedIDs), len(ownedDetails)))
			return
		}
		if err := s.deleteIPRecordsWithAudit(r.Context(), orphanedIDs, ownedDetails, r.RemoteAddr, "subnet.cidr_change_delete", fmt.Sprintf("修正網段 %d CIDR 為 %s", id, canonical)); err != nil {
			auth.WriteError(w, http.StatusInternalServerError, "db_error", "刪除脫離網段紀錄失敗")
			return
		}
	}

	_, err = s.db.ExecContext(r.Context(), `UPDATE subnets SET cidr = ?, description = ?, updated_at = ? WHERE id = ?`,
		canonical, req.Description, s.clock.Now(), id)
	if err != nil {
		auth.WriteError(w, http.StatusUnprocessableEntity, "duplicate_cidr", "CIDR 更新失敗或與現有網段衝突")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "cidr": canonical, "description": req.Description})
}

func (s *ScopeService) handlePreviewStateChange(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的網段 ID")
		return
	}

	var req struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求格式")
		return
	}

	total, owned := 0, 0
	var token string
	if req.State == "unmanaged" {
		ids, ownedList := s.findAllRecordsInSubnet(r.Context(), id)
		total = len(ids)
		owned = len(ownedList)
		if total > 0 {
			token = s.signPreviewToken(fmt.Sprintf("state:%d:unmanaged:%d:%d", id, total, owned))
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"preview_token":    token,
		"total_ip_records": total,
		"owned_ip_records": owned,
		"requires_confirm": total > 0,
	})
}

func (s *ScopeService) handleChangeState(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的網段 ID")
		return
	}

	var req struct {
		State          string `json:"state"`
		ExcludeReason  string `json:"exclude_reason"`
		ExcludeComment string `json:"exclude_comment"`
		PreviewToken   string `json:"preview_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求格式")
		return
	}

	if req.State != "managed" && req.State != "unmanaged" {
		auth.WriteError(w, http.StatusUnprocessableEntity, "invalid_state", "網段僅可設為納管 (managed) 或不納管 (unmanaged)，不可退回待檢視")
		return
	}

	if req.State == "unmanaged" {
		if req.ExcludeReason != "dhcp" && req.ExcludeReason != "wireless" && req.ExcludeReason != "other" {
			auth.WriteError(w, http.StatusUnprocessableEntity, "reason_required", "設為不納管時必須選擇原因（dhcp、wireless 或 other）")
			return
		}

		ids, ownedList := s.findAllRecordsInSubnet(r.Context(), id)
		if len(ids) > 0 {
			expected := s.signPreviewToken(fmt.Sprintf("state:%d:unmanaged:%d:%d", id, len(ids), len(ownedList)))
			if req.PreviewToken != expected {
				auth.WriteError(w, http.StatusConflict, "preview_mismatch", "資料已異動或尚未通過刪除預覽確認，請重新預覽")
				return
			}
			if err := s.deleteIPRecordsWithAudit(r.Context(), ids, ownedList, r.RemoteAddr, "subnet.unmanage_delete", fmt.Sprintf("網段 %d 改為不納管 (%s)", id, req.ExcludeReason)); err != nil {
				auth.WriteError(w, http.StatusInternalServerError, "db_error", "刪除受影響 IP 紀錄失敗")
				return
			}
		}

		_, err = s.db.ExecContext(
			r.Context(),
			`UPDATE subnets SET state = 'unmanaged', exclude_reason = ?, exclude_comment = ?, updated_at = ? WHERE id = ?`,
			req.ExcludeReason, req.ExcludeComment, s.clock.Now(), id,
		)
	} else {
		_, err = s.db.ExecContext(
			r.Context(),
			`UPDATE subnets SET state = 'managed', exclude_reason = NULL, exclude_comment = '', updated_at = ? WHERE id = ?`,
			s.clock.Now(), id,
		)
	}

	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "更新網段狀態失敗")
		return
	}

	u := auth.UserFromContext(r.Context())
	if s.audit != nil && u != nil {
		s.audit.Log(r.Context(), u.Username, u.Role, r.RemoteAddr, "subnet.state_change", "subnet", fmt.Sprintf("%d", id),
			fmt.Sprintf("變更網段狀態為 %s", req.State), map[string]any{"state": req.State, "exclude_reason": req.ExcludeReason})
	}

	s.handleGetSubnet(w, r)
}

func (s *ScopeService) handlePreviewExclusion(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的網段 ID")
		return
	}

	var req struct {
		StartIP string `json:"start_ip"`
		EndIP   string `json:"end_ip"`
		Reason  string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求格式")
		return
	}

	if err := s.validateExclusionRange(r.Context(), id, req.StartIP, req.EndIP, req.Reason); err != nil {
		auth.WriteError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}

	ids, ownedList := s.findRecordsInRange(r.Context(), id, req.StartIP, req.EndIP)
	token := ""
	if len(ids) > 0 {
		token = s.signPreviewToken(fmt.Sprintf("excl:%d:%s-%s:%d:%d", id, req.StartIP, req.EndIP, len(ids), len(ownedList)))
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"preview_token":    token,
		"total_ip_records": len(ids),
		"owned_ip_records": len(ownedList),
		"requires_confirm": len(ids) > 0,
	})
}

func (s *ScopeService) handleCreateExclusion(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的網段 ID")
		return
	}

	var req struct {
		StartIP      string `json:"start_ip"`
		EndIP        string `json:"end_ip"`
		Reason       string `json:"reason"`
		Comment      string `json:"comment"`
		PreviewToken string `json:"preview_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求格式")
		return
	}

	if err := s.validateExclusionRange(r.Context(), id, req.StartIP, req.EndIP, req.Reason); err != nil {
		auth.WriteError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}

	ids, ownedList := s.findRecordsInRange(r.Context(), id, req.StartIP, req.EndIP)
	if len(ids) > 0 {
		expected := s.signPreviewToken(fmt.Sprintf("excl:%d:%s-%s:%d:%d", id, req.StartIP, req.EndIP, len(ids), len(ownedList)))
		if req.PreviewToken != expected {
			auth.WriteError(w, http.StatusConflict, "preview_mismatch", "資料已異動或尚未通過刪除預覽確認，請重新預覽")
			return
		}
		if err := s.deleteIPRecordsWithAudit(r.Context(), ids, ownedList, r.RemoteAddr, "exclusion.create_delete", fmt.Sprintf("新增排除範圍 %s-%s (%s)", req.StartIP, req.EndIP, req.Reason)); err != nil {
			auth.WriteError(w, http.StatusInternalServerError, "db_error", "刪除排除範圍內之 IP 紀錄失敗")
			return
		}
	}

	now := s.clock.Now()
	res, err := s.db.ExecContext(
		r.Context(),
		`INSERT INTO exclusion_ranges (subnet_id, start_ip, end_ip, reason, comment, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, req.StartIP, req.EndIP, req.Reason, req.Comment, now, now,
	)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "建立排除範圍失敗")
		return
	}
	exID, _ := res.LastInsertId()

	u := auth.UserFromContext(r.Context())
	if s.audit != nil && u != nil {
		s.audit.Log(r.Context(), u.Username, u.Role, r.RemoteAddr, "exclusion.create", "subnet", fmt.Sprintf("%d", id),
			fmt.Sprintf("新增排除範圍 %s - %s (%s)", req.StartIP, req.EndIP, req.Reason), nil)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"id": exID, "start_ip": req.StartIP, "end_ip": req.EndIP, "reason": req.Reason})
}

func (s *ScopeService) handleDeleteExclusion(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	exID, err := strconv.ParseInt(chi.URLParam(r, "exclusionId"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的 ID")
		return
	}

	res, err := s.db.ExecContext(r.Context(), `DELETE FROM exclusion_ranges WHERE id = ? AND subnet_id = ?`, exID, id)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "刪除排除範圍失敗")
		return
	}
	if aff, _ := res.RowsAffected(); aff == 0 {
		auth.WriteError(w, http.StatusNotFound, "not_found", "找不到該排除範圍")
		return
	}

	u := auth.UserFromContext(r.Context())
	if s.audit != nil && u != nil {
		s.audit.Log(r.Context(), u.Username, u.Role, r.RemoteAddr, "exclusion.delete", "subnet", fmt.Sprintf("%d", id),
			fmt.Sprintf("移除排除範圍 #%d", exID), nil)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *ScopeService) handleListUndetermined(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(
		r.Context(),
		`SELECT u.ip_address, u.mac_address, COALESCE(sw.name, ''), u.vrf_name, u.if_name, u.last_seen_at
		 FROM undetermined_prefixes u LEFT JOIN switches sw ON u.switch_id = sw.id
		 ORDER BY u.last_seen_at DESC`,
	)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "查詢未定前綴失敗")
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var ip, mac, swName, vrf, ifn string
		var lastSeen any
		if rows.Scan(&ip, &mac, &swName, &vrf, &ifn, &lastSeen) == nil {
			items = append(items, map[string]any{
				"ip_address":   ip,
				"mac_address":  mac,
				"switch_name":  swName,
				"vrf_name":     vrf,
				"if_name":      ifn,
				"last_seen_at": lastSeen,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
}

func (s *ScopeService) validateExclusionRange(ctx context.Context, subnetID int64, startStr, endStr, reason string) error {
	if reason != "dhcp" && reason != "wireless" && reason != "other" {
		return errors.New("必須選擇排除原因（dhcp、wireless 或 other）")
	}
	var cidr string
	if err := s.db.QueryRowContext(ctx, `SELECT cidr FROM subnets WHERE id = ?`, subnetID).Scan(&cidr); err != nil {
		return errors.New("找不到目標網段")
	}
	_, ipNet, _ := net.ParseCIDR(cidr)

	sIP := net.ParseIP(startStr).To4()
	eIP := net.ParseIP(endStr).To4()
	if sIP == nil || eIP == nil {
		return errors.New("起始或結束 IP 必須為有效的 IPv4 位址")
	}
	if !ipNet.Contains(sIP) || !ipNet.Contains(eIP) {
		return errors.New("排除範圍的起訖位址必須完全落在網段 CIDR 內")
	}
	if binary.BigEndian.Uint32(sIP) > binary.BigEndian.Uint32(eIP) {
		return errors.New("起始 IP 不可大於結束 IP")
	}
	return nil
}

type deletedOwnedDetail struct {
	IPAddress  string `json:"ip_address"`
	MACAddress string `json:"mac_address"`
	OwnerUser  string `json:"owner_username"`
	OwnerMail  string `json:"owner_mail"`
	ExpiresAt  any    `json:"expires_at"`
}

func (s *ScopeService) findAllRecordsInSubnet(ctx context.Context, subnetID int64) ([]int64, []deletedOwnedDetail) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT r.id, r.ip_address, r.mac_address, r.owner_user_id, COALESCE(u.username, ''), r.owner_mail, r.expires_at
		 FROM ip_records r LEFT JOIN users u ON r.owner_user_id = u.id
		 WHERE r.subnet_id = ?`,
		subnetID,
	)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()
	return scanRecordDeletionDetails(rows)
}

func (s *ScopeService) findRecordsInRange(ctx context.Context, subnetID int64, startStr, endStr string) ([]int64, []deletedOwnedDetail) {
	startU := binary.BigEndian.Uint32(net.ParseIP(startStr).To4())
	endU := binary.BigEndian.Uint32(net.ParseIP(endStr).To4())

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT r.id, r.ip_address, r.mac_address, r.owner_user_id, COALESCE(u.username, ''), r.owner_mail, r.expires_at
		 FROM ip_records r LEFT JOIN users u ON r.owner_user_id = u.id
		 WHERE r.subnet_id = ?`,
		subnetID,
	)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()

	var ids []int64
	var owned []deletedOwnedDetail
	for rows.Next() {
		var id int64
		var ip, mac, uname, mail string
		var ownerID sql.NullInt64
		var exp sql.NullTime
		if rows.Scan(&id, &ip, &mac, &ownerID, &uname, &mail, &exp) == nil {
			ip4 := net.ParseIP(ip).To4()
			if ip4 == nil {
				continue
			}
			val := binary.BigEndian.Uint32(ip4)
			if val >= startU && val <= endU {
				ids = append(ids, id)
				if ownerID.Valid {
					var expVal any
					if exp.Valid {
						expVal = exp.Time
					}
					owned = append(owned, deletedOwnedDetail{
						IPAddress: ip, MACAddress: mac, OwnerUser: uname, OwnerMail: mail, ExpiresAt: expVal,
					})
				}
			}
		}
	}
	return ids, owned
}

func (s *ScopeService) findRecordsOutsideCIDR(ctx context.Context, subnetID int64, ipNet *net.IPNet) ([]int64, []deletedOwnedDetail) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT r.id, r.ip_address, r.mac_address, r.owner_user_id, COALESCE(u.username, ''), r.owner_mail, r.expires_at
		 FROM ip_records r LEFT JOIN users u ON r.owner_user_id = u.id
		 WHERE r.subnet_id = ?`,
		subnetID,
	)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()

	var ids []int64
	var owned []deletedOwnedDetail
	for rows.Next() {
		var id int64
		var ip, mac, uname, mail string
		var ownerID sql.NullInt64
		var exp sql.NullTime
		if rows.Scan(&id, &ip, &mac, &ownerID, &uname, &mail, &exp) == nil {
			ip4 := net.ParseIP(ip).To4()
			if ip4 != nil && !ipNet.Contains(ip4) {
				ids = append(ids, id)
				if ownerID.Valid {
					var expVal any
					if exp.Valid {
						expVal = exp.Time
					}
					owned = append(owned, deletedOwnedDetail{
						IPAddress: ip, MACAddress: mac, OwnerUser: uname, OwnerMail: mail, ExpiresAt: expVal,
					})
				}
			}
		}
	}
	return ids, owned
}

func scanRecordDeletionDetails(rows *sql.Rows) ([]int64, []deletedOwnedDetail) {
	var ids []int64
	var owned []deletedOwnedDetail
	for rows.Next() {
		var id int64
		var ip, mac, uname, mail string
		var ownerID sql.NullInt64
		var exp sql.NullTime
		if rows.Scan(&id, &ip, &mac, &ownerID, &uname, &mail, &exp) == nil {
			ids = append(ids, id)
			if ownerID.Valid {
				var expVal any
				if exp.Valid {
					expVal = exp.Time
				}
				owned = append(owned, deletedOwnedDetail{
					IPAddress: ip, MACAddress: mac, OwnerUser: uname, OwnerMail: mail, ExpiresAt: expVal,
				})
			}
		}
	}
	return ids, owned
}

func (s *ScopeService) deleteIPRecordsWithAudit(ctx context.Context, ids []int64, owned []deletedOwnedDetail, remoteAddr, action, reasonDesc string) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, id := range ids {
		var ipAddr string
		_ = tx.QueryRowContext(ctx, `SELECT ip_address FROM ip_records WHERE id = ?`, id).Scan(&ipAddr)
		if ipAddr != "" {
			_, _ = tx.ExecContext(ctx, `DELETE FROM notification_states WHERE dedup_key LIKE CONCAT('unreg|', ?, '|%')`, ipAddr)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM ip_records WHERE id = ?`, id); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	u := auth.UserFromContext(ctx)
	uname, urole := "system", "system"
	if u != nil {
		uname, urole = u.Username, u.Role
	}
	if s.audit != nil {
		s.audit.Log(ctx, uname, urole, remoteAddr, action, "ip_record", "batch_delete",
			fmt.Sprintf("%s：刪除 %d 筆 IP 紀錄（其中 %d 筆已登記）", reasonDesc, len(ids), len(owned)),
			map[string]any{
				"deleted_count":         len(ids),
				"owned_deleted_count":   len(owned),
				"owned_deleted_details": owned,
			})
	}
	return nil
}

func (s *ScopeService) signPreviewToken(payload string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}
