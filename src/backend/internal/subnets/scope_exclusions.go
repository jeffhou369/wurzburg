package subnets

import (
	"database/sql"
	"encoding/binary"
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

// exclusionRange 是通過語法檢查、已正規化的排除範圍。
type exclusionRange struct {
	startIP, endIP     string
	startUint, endUint uint32
	reason             string
}

// parseExclusionRange 檢查不需要資料庫的部分：原因、IPv4 格式與起訖順序。位址以點分十進位正規化。
func parseExclusionRange(startStr, endStr, reason string) (exclusionRange, error) {
	if reason != "dhcp" && reason != "wireless" && reason != "other" {
		return exclusionRange{}, errors.New("必須選擇排除原因（dhcp、wireless 或 other）")
	}
	sIP := net.ParseIP(strings.TrimSpace(startStr)).To4()
	eIP := net.ParseIP(strings.TrimSpace(endStr)).To4()
	if sIP == nil || eIP == nil {
		return exclusionRange{}, errors.New("起始或結束 IP 必須為有效的 IPv4 位址")
	}
	rng := exclusionRange{
		startIP: sIP.String(), endIP: eIP.String(),
		startUint: binary.BigEndian.Uint32(sIP), endUint: binary.BigEndian.Uint32(eIP),
		reason: reason,
	}
	if rng.startUint > rng.endUint {
		return exclusionRange{}, errors.New("起始 IP 不可大於結束 IP")
	}
	return rng, nil
}

// checkRangeInCIDR 檢查起訖位址完全落在網段 CIDR 內；網段 CIDR 來自資料庫，無法解析時視為系統錯誤。
func checkRangeInCIDR(cidr string, rng exclusionRange) error {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("網段 CIDR %q 無法解析: %w", cidr, err)
	}
	if !ipNet.Contains(net.ParseIP(rng.startIP)) || !ipNet.Contains(net.ParseIP(rng.endIP)) {
		return newScopeError(http.StatusUnprocessableEntity, "validation_error", "排除範圍的起訖位址必須完全落在網段 CIDR 內")
	}
	return nil
}

func (rng exclusionRange) matcher() func(net.IP) bool {
	return func(ip4 net.IP) bool {
		v := binary.BigEndian.Uint32(ip4)
		return v >= rng.startUint && v <= rng.endUint
	}
}

func (s *ScopeService) handlePreviewExclusion(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSubnetID(w, r)
	if !ok {
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
	rng, err := parseExclusionRange(req.StartIP, req.EndIP, req.Reason)
	if err != nil {
		auth.WriteError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}

	var recs []affectedRecord
	var token string
	err = s.runPreviewTx(r.Context(), id, func(tx *sql.Tx, row scopeRow) error {
		if err := checkRangeInCIDR(row.cidr, rng); err != nil {
			return err
		}
		var err error
		recs, err = loadAffectedRecords(r.Context(), tx, id, false, rng.matcher())
		if err != nil {
			return err
		}
		token = s.previewTokenFor(exclusionOpDesc(id, row.cidr, rng.startIP, rng.endIP, rng.reason), recs)
		return nil
	})
	if err != nil {
		writeScopeError(w, r, err, "預覽排除範圍失敗")
		return
	}
	writePreview(w, token, recs)
}

func (s *ScopeService) handleCreateExclusion(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSubnetID(w, r)
	if !ok {
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
	rng, err := parseExclusionRange(req.StartIP, req.EndIP, req.Reason)
	if err != nil {
		auth.WriteError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}

	var exID int64
	err = s.runScopeTx(r.Context(), func(tx *sql.Tx, scope []scopeRow) error {
		row, ok := findScopeRow(scope, id)
		if !ok {
			return errSubnetNotFound()
		}
		if err := checkRangeInCIDR(row.cidr, rng); err != nil {
			return err
		}

		recs, err := loadAffectedRecords(r.Context(), tx, id, true, rng.matcher())
		if err != nil {
			return err
		}
		if len(recs) > 0 {
			if !s.previewTokenMatches(exclusionOpDesc(id, row.cidr, rng.startIP, rng.endIP, rng.reason), recs, req.PreviewToken) {
				return newScopeError(http.StatusConflict, "preview_mismatch", "資料已異動或尚未通過刪除預覽確認，請重新預覽")
			}
			if err := s.deleteWithAudit(r.Context(), tx, r, id, recs,
				"exclusion.create_delete", "exclusion_create",
				fmt.Sprintf("新增排除範圍 %s-%s (%s)", rng.startIP, rng.endIP, rng.reason)); err != nil {
				return err
			}
		}

		now := s.clock.Now()
		res, err := tx.ExecContext(
			r.Context(),
			`INSERT INTO exclusion_ranges (subnet_id, start_ip, end_ip, reason, comment, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, rng.startIP, rng.endIP, rng.reason, req.Comment, now, now,
		)
		if err != nil {
			return fmt.Errorf("建立排除範圍失敗: %w", err)
		}
		exID, err = res.LastInsertId()
		if err != nil {
			return fmt.Errorf("取得排除範圍 ID 失敗: %w", err)
		}

		return s.auditTx(r.Context(), tx, r, "exclusion.create", "subnet", strconv.FormatInt(id, 10),
			fmt.Sprintf("新增排除範圍 %s - %s (%s)", rng.startIP, rng.endIP, rng.reason),
			map[string]any{"subnet_id": id, "exclusion_id": exID, "start_ip": rng.startIP, "end_ip": rng.endIP, "reason": rng.reason, "deleted_count": len(recs)})
	})
	if err != nil {
		writeScopeError(w, r, err, "建立排除範圍失敗")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"id": exID, "start_ip": rng.startIP, "end_ip": rng.endIP, "reason": rng.reason})
}

func (s *ScopeService) handleDeleteExclusion(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSubnetID(w, r)
	if !ok {
		return
	}
	exID, err := strconv.ParseInt(chi.URLParam(r, "exclusionId"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的 ID")
		return
	}

	err = s.runScopeTx(r.Context(), func(tx *sql.Tx, scope []scopeRow) error {
		if _, ok := findScopeRow(scope, id); !ok {
			return errSubnetNotFound()
		}
		res, err := tx.ExecContext(r.Context(), `DELETE FROM exclusion_ranges WHERE id = ? AND subnet_id = ?`, exID, id)
		if err != nil {
			return fmt.Errorf("刪除排除範圍失敗: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("確認刪除筆數失敗: %w", err)
		}
		if affected == 0 {
			return newScopeError(http.StatusNotFound, "not_found", "找不到該排除範圍")
		}
		return s.auditTx(r.Context(), tx, r, "exclusion.delete", "subnet", strconv.FormatInt(id, 10),
			fmt.Sprintf("移除排除範圍 #%d", exID),
			map[string]any{"subnet_id": id, "exclusion_id": exID})
	})
	if err != nil {
		writeScopeError(w, r, err, "刪除排除範圍失敗")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
