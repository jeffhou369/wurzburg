package subnets

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/jeffhou369/wurzburg/backend/internal/auth"
)

func (s *ScopeService) handlePreviewStateChange(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSubnetID(w, r)
	if !ok {
		return
	}

	var req struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求格式")
		return
	}

	var recs []affectedRecord
	var token string
	err := s.runPreviewTx(r.Context(), id, func(tx *sql.Tx, row scopeRow) error {
		if req.State != StateUnmanaged {
			return nil // 只有改為不納管才會刪除 IP 紀錄
		}
		var err error
		recs, err = loadAffectedRecords(r.Context(), tx, id, false, allRecords)
		if err != nil {
			return err
		}
		token = s.previewTokenFor(stateOpDesc(id, row.cidr, row.state, req.State), recs)
		return nil
	})
	if err != nil {
		writeScopeError(w, r, err, "預覽網段狀態變更失敗")
		return
	}
	writePreview(w, token, recs)
}

func (s *ScopeService) handleChangeState(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSubnetID(w, r)
	if !ok {
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

	if req.State != StateManaged && req.State != StateUnmanaged {
		auth.WriteError(w, http.StatusUnprocessableEntity, "invalid_state", "網段僅可設為納管 (managed) 或不納管 (unmanaged)，不可退回待檢視")
		return
	}
	if req.State == StateUnmanaged && req.ExcludeReason != "dhcp" && req.ExcludeReason != "wireless" && req.ExcludeReason != "other" {
		auth.WriteError(w, http.StatusUnprocessableEntity, "reason_required", "設為不納管時必須選擇原因（dhcp、wireless 或 other）")
		return
	}

	err := s.runScopeTx(r.Context(), func(tx *sql.Tx, scope []scopeRow) error {
		row, ok := findScopeRow(scope, id)
		if !ok {
			return errSubnetNotFound()
		}

		if req.State == StateUnmanaged {
			recs, err := loadAffectedRecords(r.Context(), tx, id, true, allRecords)
			if err != nil {
				return err
			}
			if len(recs) > 0 {
				if !s.previewTokenMatches(stateOpDesc(id, row.cidr, row.state, req.State), recs, req.PreviewToken) {
					return newScopeError(http.StatusConflict, "preview_mismatch", "資料已異動或尚未通過刪除預覽確認，請重新預覽")
				}
				if err := s.deleteWithAudit(r.Context(), tx, r, id, recs,
					"subnet.unmanage_delete", "unmanage", fmt.Sprintf("網段 %d 改為不納管 (%s)", id, req.ExcludeReason)); err != nil {
					return err
				}
			}
			if _, err := tx.ExecContext(
				r.Context(),
				`UPDATE subnets SET state = 'unmanaged', exclude_reason = ?, exclude_comment = ?, updated_at = ? WHERE id = ?`,
				req.ExcludeReason, req.ExcludeComment, s.clock.Now(), id,
			); err != nil {
				return fmt.Errorf("更新網段狀態失敗: %w", err)
			}
		} else {
			if _, err := tx.ExecContext(
				r.Context(),
				`UPDATE subnets SET state = 'managed', exclude_reason = NULL, exclude_comment = '', updated_at = ? WHERE id = ?`,
				s.clock.Now(), id,
			); err != nil {
				return fmt.Errorf("更新網段狀態失敗: %w", err)
			}
		}

		return s.auditTx(r.Context(), tx, r, "subnet.state_change", "subnet", strconv.FormatInt(id, 10),
			fmt.Sprintf("變更網段狀態為 %s", req.State),
			map[string]any{"subnet_id": id, "state": req.State, "exclude_reason": req.ExcludeReason})
	})
	if err != nil {
		writeScopeError(w, r, err, "更新網段狀態失敗")
		return
	}

	s.writeSubnetView(w, r, id, http.StatusOK)
}

// ---------------------------------------------------------------------------
// 排除範圍
// ---------------------------------------------------------------------------
