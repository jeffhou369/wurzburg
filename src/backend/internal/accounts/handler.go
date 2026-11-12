package accounts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jeffhou369/wurzburg/backend/internal/audit"
	"github.com/jeffhou369/wurzburg/backend/internal/auth"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
	"github.com/jeffhou369/wurzburg/backend/internal/settings"
)

// Handler 實作伺服端驗證、工作階段、系統管理員管理與密碼修改之 API。
type Handler struct {
	db          *sql.DB
	clock       clock.Clock
	sessions    *auth.SessionStore
	settings    *settings.Store
	audit       *audit.Logger
	rateLimiter *RateLimiter
	authSource  interfaces.AuthSource
}

func NewHandler(
	db *sql.DB,
	clk clock.Clock,
	sessions *auth.SessionStore,
	sett *settings.Store,
	aud *audit.Logger,
	authSrc interfaces.AuthSource,
) *Handler {
	if clk == nil {
		clk = clock.NewRealClock()
	}
	return &Handler{
		db:          db,
		clock:       clk,
		sessions:    sessions,
		settings:    sett,
		audit:       aud,
		rateLimiter: NewRateLimiter(clk, 10, 1*time.Minute), // 每分鐘單一 IP 最多 10 次嘗試
		authSource:  authSrc,
	}
}

func (h *Handler) SetAuthSource(src interfaces.AuthSource) {
	h.authSource = src
}

func (h *Handler) RegisterRoutes(api chi.Router) {
	// 登入前公開端點
	api.Post("/auth/login/local", h.handleLoginLocal)
	api.Post("/auth/login/org", h.handleLoginOrg)
	api.Post("/auth/logout", h.handleLogout)

	// 需要登入之端點
	api.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth)
		r.Get("/auth/me", h.handleMe)
		r.Post("/auth/change-password", h.handleChangePassword)
	})

	// 系統管理員專屬之管理端點
	api.Group(func(r chi.Router) {
		r.Use(auth.RequireAdmin)
		r.Use(h.requireNoPasswordChangePending)

		r.Get("/admins", h.handleListAdmins)
		r.Post("/admins", h.handleCreateAdmin)
		r.Post("/admins/{id}/toggle", h.handleToggleAdmin)

		r.Get("/settings/auth-source", h.handleGetAuthSourceSettings)
	})
}

// requireNoPasswordChangePending 若管理員被標記為須強制改密碼，阻擋存取其餘管理功能
func (h *Handler) requireNoPasswordChangePending(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := auth.UserFromContext(r.Context())
		if u != nil && h.isPasswordChangeRequired(r.Context(), u.ID) {
			auth.WriteError(w, http.StatusForbidden, "password_change_required", "首次登入必須先修改密碼")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) isPasswordChangeRequired(ctx context.Context, userID int64) bool {
	if h.db == nil {
		return false
	}
	var req bool
	_ = h.db.QueryRowContext(ctx, `SELECT requires_password_change FROM users WHERE id = ?`, userID).Scan(&req)
	return req
}

func (h *Handler) handleLoginLocal(w http.ResponseWriter, r *http.Request) {
	if !h.rateLimiter.Allow(rateLimitKey(r.RemoteAddr)) {
		auth.WriteError(w, http.StatusTooManyRequests, "rate_limited", "嘗試登入過於頻繁，請稍後再試")
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" || req.Password == "" {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "請提供使用者名稱與密碼")
		return
	}

	ctx := r.Context()
	now := h.clock.Now()

	var id int64
	var displayName, hash string
	var enabled, reqPwChange bool
	var failedCount int
	var lockedUntil sql.NullTime

	err := h.db.QueryRowContext(
		ctx,
		`SELECT id, display_name, password_hash, requires_password_change, enabled, failed_login_count, locked_until
		 FROM users
		 WHERE username = ? AND role = 'admin' AND auth_source = 'local'`,
		req.Username,
	).Scan(&id, &displayName, &hash, &reqPwChange, &enabled, &failedCount, &lockedUntil)

	if errors.Is(err, sql.ErrNoRows) {
		h.audit.Log(ctx, req.Username, "admin", r.RemoteAddr, "auth.login_failed", "user", req.Username, "本機管理員帳號不存在", nil)
		auth.WriteError(w, http.StatusUnauthorized, "invalid_credentials", "帳號或密碼錯誤")
		return
	}
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "系統異常")
		return
	}

	if !enabled {
		auth.WriteError(w, http.StatusForbidden, "account_disabled", "此管理員帳號已被停用")
		return
	}

	// 檢查暫時鎖定狀態
	if lockedUntil.Valid && lockedUntil.Time.After(now) {
		h.audit.Log(ctx, req.Username, "admin", r.RemoteAddr, "auth.login_locked", "user", req.Username, "管理員處於暫時鎖定狀態嘗試登入", nil)
		auth.WriteError(w, 423, "account_locked", "連續登入失敗過多，帳號暫時鎖定中，請稍後再試")
		return
	}

	matched, _ := VerifyPassword(req.Password, hash)
	if !matched {
		lockoutLimit := h.settings.GetInt(ctx, "admin_lockout_attempts", 5)
		lockoutMinutes := h.settings.GetInt(ctx, "admin_lockout_minutes", 15)

		failedCount++
		var newLockedUntil *time.Time
		if failedCount >= lockoutLimit {
			t := now.Add(time.Duration(lockoutMinutes) * time.Minute)
			newLockedUntil = &t
			failedCount = 0
		}

		_, _ = h.db.ExecContext(
			ctx,
			`UPDATE users SET failed_login_count = ?, locked_until = ?, updated_at = ? WHERE id = ?`,
			failedCount, newLockedUntil, now, id,
		)

		h.audit.Log(ctx, req.Username, "admin", r.RemoteAddr, "auth.login_failed", "user", req.Username, "本機管理員密碼錯誤", nil)
		auth.WriteError(w, http.StatusUnauthorized, "invalid_credentials", "帳號或密碼錯誤")
		return
	}

	// 登入成功，重設失敗計數
	_, _ = h.db.ExecContext(ctx, `UPDATE users SET failed_login_count = 0, locked_until = NULL, last_login_at = ? WHERE id = ?`, now, id)

	idleHours := h.settings.GetInt(ctx, "session_idle_timeout_hours", 8)
	token, err := h.sessions.CreateSession(ctx, id, idleHours, r.RemoteAddr, r.UserAgent())
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "session_error", "建立工作階段失敗")
		return
	}

	auth.SetSessionCookie(w, r, token, idleHours*3600)
	h.audit.Log(ctx, req.Username, "admin", r.RemoteAddr, "auth.login_success", "user", req.Username, "本機管理員登入成功", nil)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"user": map[string]any{
			"id":                       id,
			"username":                 req.Username,
			"display_name":             displayName,
			"role":                     "admin",
			"auth_source":              "local",
			"requires_password_change": reqPwChange,
		},
	})
}

func (h *Handler) handleLoginOrg(w http.ResponseWriter, r *http.Request) {
	if !h.rateLimiter.Allow(rateLimitKey(r.RemoteAddr)) {
		auth.WriteError(w, http.StatusTooManyRequests, "rate_limited", "嘗試登入過於頻繁")
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" || req.Password == "" {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "請提供帳號與密碼")
		return
	}

	ctx := r.Context()
	activeSource, _ := h.settings.Get(ctx, "active_auth_source")
	if activeSource == "" || activeSource == "none" {
		auth.WriteError(w, http.StatusForbidden, "auth_source_not_configured", "園區尚未啟用組織驗證來源（AD/RADIUS），請聯繫系統管理員")
		return
	}

	if h.authSource == nil {
		auth.WriteError(w, http.StatusServiceUnavailable, "auth_unavailable", "組織驗證服務未就緒")
		return
	}

	userInfo, err := h.authSource.Authenticate(ctx, req.Username, req.Password)
	if err != nil {
		h.audit.Log(ctx, req.Username, "user", r.RemoteAddr, "auth.org_login_failed", "user", req.Username, err.Error(), nil)
		if errors.Is(err, interfaces.ErrGroupRestricted) {
			auth.WriteError(w, http.StatusForbidden, "group_restricted", "此帳號不在允許登入之組織群組中")
			return
		}
		auth.WriteError(w, http.StatusUnauthorized, "invalid_credentials", "組織帳號或密碼錯誤")
		return
	}

	now := h.clock.Now()
	// 自動建立或更新使用者
	var id int64
	var enabled bool
	err = h.db.QueryRowContext(ctx, `SELECT id, enabled FROM users WHERE username = ? AND auth_source = ?`, userInfo.Username, activeSource).Scan(&id, &enabled)

	if errors.Is(err, sql.ErrNoRows) {
		res, insErr := h.db.ExecContext(
			ctx,
			`INSERT INTO users (username, display_name, mail, role, auth_source, password_hash, requires_password_change, enabled, last_login_at, created_at, updated_at)
			 VALUES (?, ?, ?, 'user', ?, '', FALSE, TRUE, ?, ?, ?)`,
			userInfo.Username, userInfo.DisplayName, userInfo.Mail, activeSource, now, now, now,
		)
		if insErr != nil {
			auth.WriteError(w, http.StatusInternalServerError, "db_error", "建立使用者紀錄失敗")
			return
		}
		id, _ = res.LastInsertId()
		enabled = true
	} else if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "查詢使用者失敗")
		return
	} else {
		if !enabled {
			auth.WriteError(w, http.StatusForbidden, "account_disabled", "此帳號已被停用")
			return
		}
		_, _ = h.db.ExecContext(
			ctx,
			`UPDATE users SET display_name = ?, mail = ?, last_login_at = ?, updated_at = ? WHERE id = ?`,
			userInfo.DisplayName, userInfo.Mail, now, now, id,
		)
	}

	idleHours := h.settings.GetInt(ctx, "session_idle_timeout_hours", 8)
	token, err := h.sessions.CreateSession(ctx, id, idleHours, r.RemoteAddr, r.UserAgent())
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "session_error", "建立工作階段失敗")
		return
	}

	auth.SetSessionCookie(w, r, token, idleHours*3600)
	h.audit.Log(ctx, userInfo.Username, "user", r.RemoteAddr, "auth.org_login_success", "user", userInfo.Username, "組織帳號登入成功", nil)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"user": map[string]any{
			"id":           id,
			"username":     userInfo.Username,
			"display_name": userInfo.DisplayName,
			"mail":         userInfo.Mail,
			"role":         "user",
			"auth_source":  activeSource,
		},
	})
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.CookieName); err == nil && cookie.Value != "" {
		_ = h.sessions.RevokeSession(r.Context(), cookie.Value)
	}
	auth.ClearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleMe(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		auth.WriteError(w, http.StatusUnauthorized, "unauthorized", "未登入")
		return
	}

	reqPwChange := h.isPasswordChangeRequired(r.Context(), u.ID)

	var mail string
	_ = h.db.QueryRowContext(r.Context(), `SELECT mail FROM users WHERE id = ?`, u.ID).Scan(&mail)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":                       u.ID,
		"username":                 u.Username,
		"display_name":             u.DisplayName,
		"mail":                     mail,
		"role":                     u.Role,
		"auth_source":              u.AuthSource,
		"requires_password_change": reqPwChange,
	})
}

func (h *Handler) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		auth.WriteError(w, http.StatusUnauthorized, "unauthorized", "未登入")
		return
	}
	if u.AuthSource != "local" {
		auth.WriteError(w, http.StatusBadRequest, "unsupported", "組織帳號密碼請至目錄伺服器修改")
		return
	}

	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求資料")
		return
	}

	ctx := r.Context()
	minLen := h.settings.GetInt(ctx, "admin_min_password_length", 12)
	if err := ValidatePasswordPolicy(req.NewPassword, minLen); err != nil {
		auth.WriteError(w, http.StatusUnprocessableEntity, "password_policy_failed", err.Error())
		return
	}

	var currentHash string
	var reqChange bool
	_ = h.db.QueryRowContext(ctx, `SELECT password_hash, requires_password_change FROM users WHERE id = ?`, u.ID).Scan(&currentHash, &reqChange)

	// 若非首次強制改密碼，需比對舊密碼
	if !reqChange {
		matched, _ := VerifyPassword(req.OldPassword, currentHash)
		if !matched {
			auth.WriteError(w, http.StatusUnauthorized, "invalid_credentials", "目前的密碼不正確")
			return
		}
	}

	newHash, err := HashPassword(req.NewPassword)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "hash_error", "密碼雜湊失敗")
		return
	}

	now := h.clock.Now()
	_, err = h.db.ExecContext(
		ctx,
		`UPDATE users SET password_hash = ?, requires_password_change = FALSE, updated_at = ? WHERE id = ?`,
		newHash, now, u.ID,
	)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "更新密碼失敗")
		return
	}

	h.audit.Log(ctx, u.Username, u.Role, r.RemoteAddr, "auth.password_changed", "user", u.Username, "本機管理員密碼修改成功", nil)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"message":"密碼已成功更新"}`))
}

func (h *Handler) handleListAdmins(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.QueryContext(
		r.Context(),
		`SELECT id, username, display_name, enabled, last_login_at
		 FROM users
		 WHERE role = 'admin' AND auth_source = 'local'
		 ORDER BY id ASC`,
	)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "查詢管理員失敗")
		return
	}
	defer func() { _ = rows.Close() }()

	var items []map[string]any
	for rows.Next() {
		var id int64
		var username, displayName string
		var enabled bool
		var lastLogin sql.NullTime
		if err := rows.Scan(&id, &username, &displayName, &enabled, &lastLogin); err != nil {
			continue
		}
		item := map[string]any{
			"id":           id,
			"username":     username,
			"display_name": displayName,
			"enabled":      enabled,
		}
		if lastLogin.Valid {
			item["last_login_at"] = lastLogin.Time
		} else {
			item["last_login_at"] = nil
		}
		items = append(items, item)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
}

func (h *Handler) handleCreateAdmin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" || req.Password == "" {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "請提供使用者名稱與密碼")
		return
	}

	ctx := r.Context()
	minLen := h.settings.GetInt(ctx, "admin_min_password_length", 12)
	if err := ValidatePasswordPolicy(req.Password, minLen); err != nil {
		auth.WriteError(w, http.StatusUnprocessableEntity, "password_policy_failed", err.Error())
		return
	}

	hash, err := HashPassword(req.Password)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "hash_error", "密碼雜湊失敗")
		return
	}

	if req.DisplayName == "" {
		req.DisplayName = req.Username
	}

	now := h.clock.Now()
	_, err = h.db.ExecContext(
		ctx,
		`INSERT INTO users (username, display_name, mail, role, auth_source, password_hash, requires_password_change, enabled, created_at, updated_at)
		 VALUES (?, ?, '', 'admin', 'local', ?, FALSE, TRUE, ?, ?)`,
		req.Username, req.DisplayName, hash, now, now,
	)
	if err != nil {
		auth.WriteError(w, http.StatusConflict, "username_taken", "此管理員使用者名稱已存在")
		return
	}

	u := auth.UserFromContext(ctx)
	h.audit.Log(ctx, u.Username, u.Role, r.RemoteAddr, "admin.create", "user", req.Username, "建立新系統管理員帳號", nil)
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(`{"message":"管理員帳號建立成功"}`))
}

func (h *Handler) handleToggleAdmin(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	targetID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的管理員 ID")
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求")
		return
	}

	ctx := r.Context()
	// 若欲停用，必須檢查系統是否至少保留一位啟用中的管理員
	if !req.Enabled {
		var activeAdmins int
		_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin' AND enabled = TRUE`).Scan(&activeAdmins)
		if activeAdmins <= 1 {
			auth.WriteError(w, http.StatusConflict, "cannot_disable_last_admin", "無法停用最後一位啟用中的系統管理員")
			return
		}
	}

	now := h.clock.Now()
	res, err := h.db.ExecContext(
		ctx,
		`UPDATE users SET enabled = ?, updated_at = ? WHERE id = ? AND role = 'admin' AND auth_source = 'local'`,
		req.Enabled, now, targetID,
	)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "更新狀態失敗")
		return
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		auth.WriteError(w, http.StatusNotFound, "not_found", "找不到指定管理員")
		return
	}

	u := auth.UserFromContext(ctx)
	h.audit.Log(ctx, u.Username, u.Role, r.RemoteAddr, "admin.toggle", "user", idStr, "切換管理員啟用狀態", map[string]any{"enabled": req.Enabled})
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"message":"狀態已成功更新"}`))
}

func (h *Handler) handleGetAuthSourceSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	activeSource, _ := h.settings.Get(ctx, "active_auth_source")
	if activeSource == "" {
		activeSource = "none"
	}

	// 統計目前登記在既有驗證來源帳號名下的 IP 筆數
	var affectedCount int
	_ = h.db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM ip_records ip
		 JOIN users u ON ip.owner_user_id = u.id
		 WHERE u.auth_source = ?`,
		activeSource,
	).Scan(&affectedCount)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"active_source":             activeSource,
		"affected_registered_count": affectedCount,
	})
}
