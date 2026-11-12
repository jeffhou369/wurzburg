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

// errRegistrationChanged 表示鎖定後發現登記已不允許此操作（例如被其他人釋放或改派）。
var errRegistrationChanged = errors.New("登記狀態已變更")

// changeRegistrationTx 在單筆交易內鎖定 IP 紀錄、重新檢查擁有者權限，執行異動並寫入成功稽核。
// 任何一步失敗都整筆回滾，呼叫端據此回報略過，不得回報成功或留下成功稽核。
func (h *Handler) changeRegistrationTx(ctx context.Context, id int64, actor *auth.User, remoteAddr, action string,
	mutate func(tx *sql.Tx, ip string) (summary string, details map[string]any, err error)) error {
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
		return errRegistrationChanged
	}
	ownerID := owner.Int64
	if !auth.CanEditRegistration(actor, &ownerID) {
		return errRegistrationChanged
	}
	summary, details, err := mutate(tx, ip)
	if err != nil {
		return err
	}
	if h.audit != nil {
		if err := h.audit.LogTx(ctx, tx, actor.Username, actor.Role, remoteAddr, action, "ip_record", ip, summary, details); err != nil {
			return err
		}
	}
	return tx.Commit()
}
