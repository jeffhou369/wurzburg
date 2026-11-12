package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/clock"
)

const CookieName = "wurzburg_session"

type User struct {
	ID                     int64
	Username               string
	DisplayName            string
	Role                   string // admin | user
	AuthSource             string // local | ad | radius
	RequiresPasswordChange bool
}

type SessionStore struct {
	db    *sql.DB
	clock clock.Clock
}

func NewSessionStore(db *sql.DB, clk clock.Clock) *SessionStore {
	if clk == nil {
		clk = clock.NewRealClock()
	}
	return &SessionStore{db: db, clock: clk}
}

// CreateSession 建立伺服端工作階段，回傳原始 token（送進 Cookie）與雜湊後之紀錄。
func (s *SessionStore) CreateSession(
	ctx context.Context,
	userID int64,
	idleHours int,
	ipAddress, userAgent string,
) (string, error) {
	if s.db == nil {
		return "", errors.New("資料庫未連線")
	}

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("產生隨機 token 失敗: %w", err)
	}
	rawToken := hex.EncodeToString(tokenBytes)
	tokenHash := hashToken(rawToken)

	now := s.clock.Now()
	idleTimeout := now.Add(time.Duration(idleHours) * time.Hour)
	absoluteExpiry := now.Add(24 * time.Hour) // 絕對上限 24 小時

	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO sessions (token_hash, user_id, expires_at, idle_timeout_at, ip_address, user_agent, created_at, last_active_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		tokenHash, userID, absoluteExpiry, idleTimeout, ipAddress, userAgent, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("儲存工作階段失敗: %w", err)
	}

	return rawToken, nil
}

// ValidateSession 查驗工作階段是否有效且未閒置逾時，並回傳所屬使用者資訊。
func (s *SessionStore) ValidateSession(
	ctx context.Context,
	rawToken string,
	idleHours int,
) (*User, error) {
	if s.db == nil || rawToken == "" {
		return nil, errors.New("無效的工作階段")
	}

	tokenHash := hashToken(rawToken)
	now := s.clock.Now()

	var u User
	var expiresAt, idleTimeoutAt time.Time
	var userEnabled bool

	err := s.db.QueryRowContext(
		ctx,
		`SELECT u.id, u.username, u.display_name, u.role, u.auth_source, u.enabled, u.requires_password_change, s.expires_at, s.idle_timeout_at
		 FROM sessions s
		 JOIN users u ON s.user_id = u.id
		 WHERE s.token_hash = ?`,
		tokenHash,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.AuthSource, &userEnabled, &u.RequiresPasswordChange, &expiresAt, &idleTimeoutAt)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("工作階段不存在或已撤銷")
	}
	if err != nil {
		return nil, fmt.Errorf("查詢工作階段失敗: %w", err)
	}

	if !userEnabled {
		return nil, errors.New("帳號已被停用")
	}

	if now.After(expiresAt) {
		_ = s.RevokeSession(ctx, rawToken)
		return nil, errors.New("工作階段已過期")
	}

	if now.After(idleTimeoutAt) {
		_ = s.RevokeSession(ctx, rawToken)
		return nil, errors.New("工作階段閒置逾時")
	}

	// 活躍延展閒置逾時
	newIdle := now.Add(time.Duration(idleHours) * time.Hour)
	_, _ = s.db.ExecContext(
		ctx,
		`UPDATE sessions SET last_active_at = ?, idle_timeout_at = ? WHERE token_hash = ?`,
		now, newIdle, tokenHash,
	)

	return &u, nil
}

// RevokeSession 撤銷指定工作階段。
func (s *SessionStore) RevokeSession(ctx context.Context, rawToken string) error {
	if s.db == nil || rawToken == "" {
		return nil
	}
	tokenHash := hashToken(rawToken)
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// SetSessionCookie 將 token 寫入 HttpOnly Cookie，若為 HTTPS 則設定 Secure。
func SetSessionCookie(w http.ResponseWriter, r *http.Request, token string, maxAgeSeconds int) {
	isSecure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAgeSeconds,
	})
}

// ClearSessionCookie 清除工作階段 Cookie。
func ClearSessionCookie(w http.ResponseWriter, r *http.Request) {
	SetSessionCookie(w, r, "", -1)
}
