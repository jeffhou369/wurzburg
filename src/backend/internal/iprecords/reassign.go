package iprecords

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/auth"
)

// reassignOne 鎖定後重新確認登記，擁有者異動與成功稽核必須一起提交。
func (h *Handler) reassignOne(ctx context.Context, id, targetID int64, targetMail, targetUsername string, actor *auth.User, remoteAddr string, now time.Time, batch bool) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var ip string
	var owner sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT ip_address, owner_user_id FROM ip_records WHERE id = ? FOR UPDATE`, id).Scan(&ip, &owner); err != nil {
		return err
	}
	if !owner.Valid {
		return errors.New("未登記之位址無法改派")
	}
	if targetMail != "" {
		_, err = tx.ExecContext(ctx, `UPDATE ip_records SET owner_user_id = ?, owner_mail = ?, updated_at = ? WHERE id = ?`, targetID, targetMail, now, id)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE ip_records SET owner_user_id = ?, updated_at = ? WHERE id = ?`, targetID, now, id)
	}
	if err != nil {
		return err
	}
	if h.audit != nil {
		if err := h.audit.LogTx(ctx, tx, actor.Username, actor.Role, remoteAddr, "registration.reassign", "ip_record", ip, fmt.Sprintf("將 %s 改派予 %s", ip, targetUsername), map[string]any{"batch": batch, "target_username": targetUsername}); err != nil {
			return err
		}
	}
	return tx.Commit()
}
