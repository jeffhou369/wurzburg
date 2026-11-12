package settings

import (
	"context"
	"errors"
	"sort"
)

// SetMany 以同一筆交易儲存一組非機敏設定，任何一項失敗皆回滾。
func (s *Store) SetMany(ctx context.Context, values map[string]string) error {
	if s.db == nil {
		return errors.New("資料庫未連線，無法儲存設定")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings (setting_key, setting_value, is_secret, updated_at) VALUES (?, ?, FALSE, ?) ON DUPLICATE KEY UPDATE setting_value = VALUES(setting_value), is_secret = FALSE, updated_at = VALUES(updated_at)`, key, values[key], s.clock.Now()); err != nil {
			return err
		}
	}
	return tx.Commit()
}
