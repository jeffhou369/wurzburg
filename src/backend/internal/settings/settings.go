package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/crypto"
)

// 開發規格第 10 節定義之系統設定預設值
var Defaults = map[string]string{
	"campus_name":                   "Würzburg",
	"timezone":                      "Asia/Taipei",
	"poll_interval_minutes":         "3",
	"poll_concurrency":              "12",
	"offline_grace_rounds":          "2",
	"auto_purge_days":               "90",
	"registration_valid_days":       "365",
	"expiry_advance_days":           "14",
	"expiry_reminder_interval_days": "7",
	"expiry_max_reminders":          "4",
	"weekly_summary_cron":           "0 9 * * 1",
	"unreg_digest_max_items":        "50",
	"admin_min_password_length":     "12",
	"admin_lockout_attempts":        "5",
	"admin_lockout_minutes":         "15",
	"session_idle_timeout_hours":    "8",
	"poll_run_retention_days":       "30",
	"notification_retention_days":   "365",
	"ad_allowed_group":              "",
	"active_auth_source":            "none",
}

// Store 封裝系統參數與加密機敏資料的讀寫操作。
type Store struct {
	db       *sql.DB
	cipher   *crypto.Cipher
	clock    clock.Clock
	defaults map[string]string
}

// NewStore 建立設定儲存庫。若不需要存取機敏資料可傳入 nil cipher（寫入 secret 時仍會攔截）。
func NewStore(db *sql.DB, cipher *crypto.Cipher, clk clock.Clock) *Store {
	if clk == nil {
		clk = clock.NewRealClock()
	}
	return &Store{db: db, cipher: cipher, clock: clk}
}

// NewStoreWithDefaults 建立具有部署預設值的設定儲存庫；資料庫已儲存的設定仍優先。
func NewStoreWithDefaults(db *sql.DB, cipher *crypto.Cipher, clk clock.Clock, defaults map[string]string) *Store {
	store := NewStore(db, cipher, clk)
	store.defaults = make(map[string]string, len(defaults))
	for key, value := range defaults {
		if value != "" {
			store.defaults[key] = value
		}
	}
	return store
}

// Get 讀取字串設定，找不到時回傳規格定義的預設值。
func (s *Store) Get(ctx context.Context, key string) (string, error) {
	if s.db != nil {
		var val string
		var isSecret bool
		err := s.db.QueryRowContext(
			ctx,
			`SELECT setting_value, is_secret FROM settings WHERE setting_key = ?`,
			key,
		).Scan(&val, &isSecret)

		if err == nil {
			if isSecret {
				return "", errors.New("請使用 GetSecret 讀取機敏設定")
			}
			return val, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("讀取設定 %s 失敗: %w", key, err)
		}
	}

	if def, ok := s.defaults[key]; ok {
		return def, nil
	}
	if def, ok := Defaults[key]; ok {
		return def, nil
	}
	return "", nil
}

// GetInt 讀取整數設定。
func (s *Store) GetInt(ctx context.Context, key string, fallback int) int {
	val, err := s.Get(ctx, key)
	if err != nil || val == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return fallback
	}
	return parsed
}

// Set 寫入一般文字設定。
func (s *Store) Set(ctx context.Context, key, value string) error {
	if s.db == nil {
		return errors.New("資料庫未連線，無法儲存設定")
	}
	now := s.clock.Now()
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO settings (setting_key, setting_value, is_secret, updated_at)
		 VALUES (?, ?, FALSE, ?)
		 ON DUPLICATE KEY UPDATE setting_value = VALUES(setting_value), is_secret = FALSE, updated_at = VALUES(updated_at)`,
		key, value, now,
	)
	return err
}

// SetSecret 將機敏資料加密後寫入設定表。金鑰未初始化時明確失敗。
func (s *Store) SetSecret(ctx context.Context, key, plaintext string) error {
	if s.cipher == nil {
		return errors.New("加密處理器未初始化，無法儲存機敏資料 (請檢查 WURZBURG_SECRET_KEY)")
	}
	if s.db == nil {
		return errors.New("資料庫未連線，無法儲存機敏設定")
	}
	encrypted, err := s.cipher.Encrypt(plaintext)
	if err != nil {
		return err
	}
	now := s.clock.Now()
	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO settings (setting_key, setting_value, is_secret, updated_at)
		 VALUES (?, ?, TRUE, ?)
		 ON DUPLICATE KEY UPDATE setting_value = VALUES(setting_value), is_secret = TRUE, updated_at = VALUES(updated_at)`,
		key, encrypted, now,
	)
	return err
}

// GetSecret 讀取並解密機敏資料。僅供後端連線元件內部使用，禁止透傳至 API 回應或日誌。
func (s *Store) GetSecret(ctx context.Context, key string) (string, error) {
	if s.cipher == nil {
		return "", errors.New("加密處理器未初始化，無法解密機敏資料")
	}
	if s.db == nil {
		return "", errors.New("資料庫未連線")
	}
	var encrypted string
	err := s.db.QueryRowContext(
		ctx,
		`SELECT setting_value FROM settings WHERE setting_key = ? AND is_secret = TRUE`,
		key,
	).Scan(&encrypted)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("讀取機敏設定 %s 失敗: %w", key, err)
	}
	return s.cipher.Decrypt(encrypted)
}

// HasSecret 回傳指定機敏鍵值是否已設定內容（安全回傳給 API 介面）。
func (s *Store) HasSecret(ctx context.Context, key string) (bool, error) {
	if s.db == nil {
		return false, nil
	}
	var val string
	err := s.db.QueryRowContext(
		ctx,
		`SELECT setting_value FROM settings WHERE setting_key = ? AND is_secret = TRUE`,
		key,
	).Scan(&val)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return val != "", nil
}
