package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/jeffhou369/wurzburg/backend/internal/settings"
)

type contextKey string

const userContextKey contextKey = "wurzburg_user"

// WithUser 將通過驗證之使用者放入 Context。
func WithUser(ctx context.Context, user *User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// UserFromContext 從 Context 取出目前登入的使用者，未登入時回傳 nil。
func UserFromContext(ctx context.Context) *User {
	u, _ := ctx.Value(userContextKey).(*User)
	return u
}

// WriteError 以標準格式回傳 API 錯誤 JSON。
func WriteError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":    code,
		"message": message,
	})
}

// AuthenticateMiddleware 解析工作階段 Cookie（或在測試模式解析測試標頭）並注入使用者資訊。
func AuthenticateMiddleware(sessions *SessionStore, cfgStore *settings.Store, allowTestHeaders bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 測試環境專用輔助：允許測試以單一標頭指定呼叫身分而不必每次走完整登入流程
			if allowTestHeaders {
				if testUserIDStr := r.Header.Get("X-Test-User-ID"); testUserIDStr != "" {
					if uid, err := strconv.ParseInt(testUserIDStr, 10, 64); err == nil {
						role := r.Header.Get("X-Test-Role")
						if role == "" {
							role = "user"
						}
						username := r.Header.Get("X-Test-Username")
						if username == "" {
							username = "testuser"
						}
						u := &User{
							ID:          uid,
							Username:    username,
							DisplayName: username,
							Role:        role,
							AuthSource:  "local",
						}
						next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), u)))
						return
					}
				}
			}

			cookie, err := r.Cookie(CookieName)
			if err != nil || cookie.Value == "" {
				next.ServeHTTP(w, r)
				return
			}

			idleHours := 8
			if cfgStore != nil {
				idleHours = cfgStore.GetInt(r.Context(), "session_idle_timeout_hours", 8)
			}

			user, err := sessions.ValidateSession(r.Context(), cookie.Value, idleHours)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
		})
	}
}

// RequireAuth 強制要求使用者必須處於已登入狀態，否則回傳 401。
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserFromContext(r.Context()) == nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "請先登入系統")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdmin 強制要求必須為系統管理員角色，否則回傳 403。
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r.Context())
		if u == nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "請先登入系統")
			return
		}
		if u.Role != "admin" {
			WriteError(w, http.StatusForbidden, "forbidden", "權限不足，僅限系統管理員執行此操作")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// CanEditRegistration 依據 ADR 0012 判斷指定使用者是否有權限編輯屬於 ownerUserID 的登記。
func CanEditRegistration(u *User, ownerUserID *int64) bool {
	if u == nil {
		return false
	}
	if u.Role == "admin" {
		return true
	}
	return ownerUserID != nil && *ownerUserID == u.ID
}

// CSRFMiddleware 對所有變更性 HTTP 方法（POST/PUT/PATCH/DELETE）驗證防偽標頭。
func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			if r.Header.Get("X-Requested-With") == "" && r.Header.Get("X-CSRF-Token") == "" {
				WriteError(w, http.StatusForbidden, "csrf_rejected", "缺少安全性請求標頭（X-Requested-With 或 X-CSRF-Token）")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
