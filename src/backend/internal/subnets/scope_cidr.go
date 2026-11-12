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

	"github.com/jeffhou369/wurzburg/backend/internal/auth"
)

// parseTargetCIDR 解析並正規化目標 CIDR，回傳（網段、正規化字串）。
func parseTargetCIDR(raw string) (*net.IPNet, string, error) {
	_, ipNet, err := net.ParseCIDR(strings.TrimSpace(raw))
	if err != nil || ipNet.IP.To4() == nil {
		return nil, "", errors.New("CIDR 格式無效")
	}
	return ipNet, ipNet.String(), nil
}

func outsideCIDR(newNet *net.IPNet) func(net.IP) bool {
	return func(ip4 net.IP) bool { return !newNet.Contains(ip4) }
}

// recordMove 是修正 CIDR 後改歸屬其他納管網段、因此保留不刪的 IP 紀錄。
type recordMove struct {
	recordID int64
	subnetID int64
}

// splitCIDROrphans 把「修正 CIDR 後不在新範圍內」的紀錄分成兩類（開發規格 3.3）：
// 仍落在其他納管網段、且不在排除範圍、不是閘道位址者改歸屬該網段；其餘才刪除。
// 判定使用「套用新 CIDR 後」的範圍快照；shareLock 為 true 時以共享鎖讀取閘道與排除範圍。
func splitCIDROrphans(ctx context.Context, q scopeQuerier, scope []scopeRow, subnetID int64, canonical string, shareLock bool, orphans []affectedRecord) ([]affectedRecord, []recordMove, error) {
	if len(orphans) == 0 {
		return nil, nil, nil
	}
	rows := make([]scopeRow, len(scope))
	copy(rows, scope)
	for i := range rows {
		if rows[i].id == subnetID {
			rows[i].cidr = canonical
		}
	}
	snap, err := loadSnapshot(ctx, q, rows, shareLock)
	if err != nil {
		return nil, nil, err
	}
	var deletes []affectedRecord
	var moves []recordMove
	for _, rec := range orphans {
		res, err := snap.ResolveIP(rec.ip)
		if err != nil {
			return nil, nil, err
		}
		if res.ShouldHaveRecord && res.SubnetID != subnetID {
			moves = append(moves, recordMove{recordID: rec.id, subnetID: res.SubnetID})
			continue
		}
		deletes = append(deletes, rec)
	}
	return deletes, moves, nil
}

// HandlePreviewCIDRChange 預覽「把網段 CIDR 修正為 X」會刪除哪些 IP 紀錄，回傳與其他預覽相同形狀的結果。
// 路徑：POST /subnets/{id}/cidr/preview，與確認用 PUT /subnets/{id} 共用相同刪除指紋。
func (s *ScopeService) HandlePreviewCIDRChange(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSubnetID(w, r)
	if !ok {
		return
	}
	var req struct {
		CIDR string `json:"cidr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求格式")
		return
	}
	newNet, canonical, err := parseTargetCIDR(req.CIDR)
	if err != nil {
		auth.WriteError(w, http.StatusUnprocessableEntity, "invalid_cidr", err.Error())
		return
	}

	var recs []affectedRecord
	var token string
	err = s.runPreviewTx(r.Context(), id, func(tx *sql.Tx, row scopeRow) error {
		orphans, err := loadAffectedRecords(r.Context(), tx, id, false, outsideCIDR(newNet))
		if err != nil {
			return err
		}
		scope, err := queryScopeRows(r.Context(), tx, plainScopeRowsSQL)
		if err != nil {
			return fmt.Errorf("查詢網段範圍失敗: %w", err)
		}
		recs, _, err = splitCIDROrphans(r.Context(), tx, scope, id, canonical, false, orphans)
		if err != nil {
			return err
		}
		token = s.previewTokenFor(cidrOpDesc(id, row.cidr, canonical), recs)
		return nil
	})
	if err != nil {
		writeScopeError(w, r, err, "預覽修正 CIDR 失敗")
		return
	}
	writePreview(w, token, recs)
}

func writePreview(w http.ResponseWriter, token string, recs []affectedRecord) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"preview_token":    token,
		"total_ip_records": len(recs),
		"owned_ip_records": countOwned(recs),
		"requires_confirm": len(recs) > 0,
	})
}

func (s *ScopeService) handleUpdateSubnet(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSubnetID(w, r)
	if !ok {
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

	newNet, canonical, err := parseTargetCIDR(req.CIDR)
	if err != nil {
		auth.WriteError(w, http.StatusUnprocessableEntity, "invalid_cidr", err.Error())
		return
	}

	err = s.runScopeTx(r.Context(), func(tx *sql.Tx, scope []scopeRow) error {
		row, ok := findScopeRow(scope, id)
		if !ok {
			return errSubnetNotFound()
		}

		// 在鎖內重新計算：修正後不再屬於此網段的既有 IP 紀錄；仍屬其他納管網段者改歸屬，其餘才刪除。
		orphans, err := loadAffectedRecords(r.Context(), tx, id, true, outsideCIDR(newNet))
		if err != nil {
			return err
		}
		recs, moves, err := splitCIDROrphans(r.Context(), tx, scope, id, canonical, true, orphans)
		if err != nil {
			return err
		}
		if len(recs) > 0 {
			if !s.previewTokenMatches(cidrOpDesc(id, row.cidr, canonical), recs, req.PreviewToken) {
				return newScopeError(http.StatusConflict, "confirmation_required",
					fmt.Sprintf("修正 CIDR 將刪除 %d 筆 IP 紀錄（其中 %d 筆已登記），請先預覽並確認，或資料已異動請重新預覽", len(recs), countOwned(recs)))
			}
			if err := s.deleteWithAudit(r.Context(), tx, r, id, recs,
				"subnet.cidr_change_delete", "cidr_change", fmt.Sprintf("修正網段 %d CIDR 為 %s", id, canonical)); err != nil {
				return err
			}
		}

		for _, mv := range moves {
			if _, err := tx.ExecContext(r.Context(), `UPDATE ip_records SET subnet_id = ?, updated_at = ? WHERE id = ?`,
				mv.subnetID, s.clock.Now(), mv.recordID); err != nil {
				return fmt.Errorf("調整 IP 紀錄歸屬失敗: %w", err)
			}
		}

		if _, err := tx.ExecContext(r.Context(), `UPDATE subnets SET cidr = ?, description = ?, updated_at = ? WHERE id = ?`,
			canonical, req.Description, s.clock.Now(), id); err != nil {
			if isDuplicateEntry(err) {
				return newScopeError(http.StatusUnprocessableEntity, "duplicate_cidr", "CIDR 更新失敗或與現有網段衝突")
			}
			return fmt.Errorf("更新網段失敗: %w", err)
		}

		return s.auditTx(r.Context(), tx, r, "subnet.update", "subnet", strconv.FormatInt(id, 10),
			fmt.Sprintf("更新網段 %d：CIDR %s → %s", id, row.cidr, canonical),
			map[string]any{"subnet_id": id, "old_cidr": row.cidr, "cidr": canonical, "deleted_count": len(recs), "moved_count": len(moves)})
	})
	if err != nil {
		writeScopeError(w, r, err, "更新網段失敗")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "cidr": canonical, "description": req.Description})
}

// ---------------------------------------------------------------------------
// 網段狀態
// ---------------------------------------------------------------------------

func allRecords(net.IP) bool { return true }
