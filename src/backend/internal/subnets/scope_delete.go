package subnets

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"github.com/jeffhou369/wurzburg/backend/internal/auth"
	"github.com/jeffhou369/wurzburg/backend/internal/dedupkey"
	"github.com/jeffhou369/wurzburg/backend/internal/sqlutil"
)

// deleteAffectedRecords 在交易內刪除這些 IP 紀錄，以及它們的「未登記」與「到期」通知去重鍵
// （格式見 dedupkey 套件；做法與清除排程相同）。分批執行，任何一批失敗都回傳錯誤。
func deleteAffectedRecords(ctx context.Context, tx *sql.Tx, recs []affectedRecord) error {
	for start := 0; start < len(recs); start += scopeDeleteChunk {
		end := min(start+scopeDeleteChunk, len(recs))
		chunk := recs[start:end]

		conds := make([]string, 0, len(chunk)*2)
		likeArgs := make([]any, 0, len(chunk)*2)
		idArgs := make([]any, 0, len(chunk))
		for _, r := range chunk {
			conds = append(conds, "dedup_key LIKE ?", "dedup_key LIKE ?")
			likeArgs = append(likeArgs, dedupkey.UnregisteredPattern(r.ip), dedupkey.ExpiryPattern(r.ip))
			idArgs = append(idArgs, r.id)
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM notification_states WHERE `+strings.Join(conds, " OR "), likeArgs...); err != nil {
			return fmt.Errorf("刪除通知去重鍵失敗: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM ip_records WHERE id IN (`+sqlutil.Placeholders(len(idArgs))+`)`, idArgs...); err != nil {
			return fmt.Errorf("刪除 IP 紀錄失敗: %w", err)
		}
	}
	return nil
}

// auditTx 在交易內寫入一筆稽核。稽核寫入失敗會回傳錯誤，使整筆範圍變更回滾；
// 沒有設定稽核器（單元測試）時略過。
func (s *ScopeService) auditTx(ctx context.Context, tx *sql.Tx, r *http.Request, action, targetType, targetID, summary string, details map[string]any) error {
	if s.audit == nil {
		return nil
	}
	username, role := "system", "system"
	if u := auth.UserFromContext(ctx); u != nil {
		username, role = u.Username, u.Role
	}
	if err := s.audit.LogTx(ctx, tx, username, role, r.RemoteAddr, action, targetType, targetID, summary, details); err != nil {
		return fmt.Errorf("寫入稽核失敗 (%s): %w", action, err)
	}
	return nil
}

// deleteWithAudit 刪除受影響的紀錄並寫入含已登記者明細的刪除稽核；沒有受影響紀錄時什麼都不做。
func (s *ScopeService) deleteWithAudit(
	ctx context.Context, tx *sql.Tx, r *http.Request,
	subnetID int64, recs []affectedRecord, action, scopeOperation, reasonDesc string,
) error {
	if len(recs) == 0 {
		return nil
	}
	if err := deleteAffectedRecords(ctx, tx, recs); err != nil {
		return err
	}
	owned := ownedDetails(recs)
	return s.auditTx(ctx, tx, r, action, "ip_record", "batch_delete",
		fmt.Sprintf("%s：刪除 %d 筆 IP 紀錄（其中 %d 筆已登記）", reasonDesc, len(recs), len(owned)),
		map[string]any{
			"subnet_id":             subnetID,
			"scope_operation":       scopeOperation,
			"deleted_count":         len(recs),
			"owned_deleted_count":   len(owned),
			"owned_deleted_details": owned,
		})
}

// ---------------------------------------------------------------------------
// 讀取類 API
// ---------------------------------------------------------------------------
