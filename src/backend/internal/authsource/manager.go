package authsource

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jeffhou369/wurzburg/backend/internal/audit"
	"github.com/jeffhou369/wurzburg/backend/internal/auth"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
	"github.com/jeffhou369/wurzburg/backend/internal/settings"
)

// DynamicManager 動態根據 settings.Store 設定的 active_auth_source 呼叫對應的 AD 或 RADIUS 驗證客戶端。
type DynamicManager struct {
	db       *sql.DB
	settings *settings.Store
	audit    *audit.Logger
}

func NewDynamicManager(db *sql.DB, sett *settings.Store, aud *audit.Logger) *DynamicManager {
	return &DynamicManager{db: db, settings: sett, audit: aud}
}

func (m *DynamicManager) Authenticate(ctx context.Context, username, password string) (*interfaces.OrgUserInfo, error) {
	source, _ := m.settings.Get(ctx, "active_auth_source")
	switch source {
	case "ad":
		client, err := m.buildADClient(ctx)
		if err != nil {
			return nil, err
		}
		return client.Authenticate(ctx, username, password)
	case "radius":
		client, err := m.buildRADIUSClient(ctx)
		if err != nil {
			return nil, err
		}
		return client.Authenticate(ctx, username, password)
	default:
		return nil, errors.New("園區尚未設定啟用之組織驗證來源")
	}
}

func (m *DynamicManager) buildADClient(ctx context.Context) (*ADClient, error) {
	adURL, _ := m.settings.Get(ctx, "ad_url")
	useTLSStr, _ := m.settings.Get(ctx, "ad_use_tls")
	bindDN, _ := m.settings.Get(ctx, "ad_bind_dn")
	bindPass, _ := m.settings.GetSecret(ctx, "ad_bind_password")
	baseDN, _ := m.settings.Get(ctx, "ad_base_dn")
	allowedGroup, _ := m.settings.Get(ctx, "ad_allowed_group")
	userAttr, _ := m.settings.Get(ctx, "ad_user_attr")
	mailAttr, _ := m.settings.Get(ctx, "ad_mail_attr")
	nameAttr, _ := m.settings.Get(ctx, "ad_name_attr")

	return NewADClient(ADConfig{
		URL:          adURL,
		UseTLS:       useTLSStr != "false",
		BindDN:       bindDN,
		BindPassword: bindPass,
		BaseDN:       baseDN,
		AllowedGroup: allowedGroup,
		UserAttr:     userAttr,
		MailAttr:     mailAttr,
		NameAttr:     nameAttr,
	}), nil
}

func (m *DynamicManager) buildRADIUSClient(ctx context.Context) (*RADIUSClient, error) {
	host, _ := m.settings.Get(ctx, "radius_host")
	port := m.settings.GetInt(ctx, "radius_port", 1812)
	secret, _ := m.settings.GetSecret(ctx, "radius_secret")
	timeoutMs := m.settings.GetInt(ctx, "radius_timeout_ms", 5000)

	return NewRADIUSClient(RADIUSConfig{
		Host:      host,
		Port:      port,
		Secret:    secret,
		TimeoutMs: timeoutMs,
	}), nil
}

func (m *DynamicManager) RegisterRoutes(api chi.Router) {
	api.With(auth.RequireAdmin).Get("/settings/auth-source", m.handleGetConfig)
	api.With(auth.RequireAdmin).Put("/settings/auth-source", m.handleUpdateConfig)
	api.With(auth.RequireAdmin).Post("/settings/auth-source/test", m.handleTestAuth)
}

func (m *DynamicManager) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	activeSource, _ := m.settings.Get(ctx, "active_auth_source")
	if activeSource == "" {
		activeSource = "none"
	}
	adURL, _ := m.settings.Get(ctx, "ad_url")
	adUseTLS, _ := m.settings.Get(ctx, "ad_use_tls")
	adBindDN, _ := m.settings.Get(ctx, "ad_bind_dn")
	adBaseDN, _ := m.settings.Get(ctx, "ad_base_dn")
	adAllowedGroup, _ := m.settings.Get(ctx, "ad_allowed_group")
	adUserAttr, _ := m.settings.Get(ctx, "ad_user_attr")
	adMailAttr, _ := m.settings.Get(ctx, "ad_mail_attr")
	adNameAttr, _ := m.settings.Get(ctx, "ad_name_attr")
	adHasBindPass, _ := m.settings.HasSecret(ctx, "ad_bind_password")

	radHost, _ := m.settings.Get(ctx, "radius_host")
	radPort := m.settings.GetInt(ctx, "radius_port", 1812)
	radTimeoutMs := m.settings.GetInt(ctx, "radius_timeout_ms", 5000)
	radHasSecret, _ := m.settings.HasSecret(ctx, "radius_secret")

	var affectedCount int
	if m.db != nil && activeSource != "none" {
		_ = m.db.QueryRowContext(
			ctx,
			`SELECT COUNT(*) FROM ip_records ip JOIN users u ON ip.owner_user_id = u.id WHERE u.auth_source = ?`,
			activeSource,
		).Scan(&affectedCount)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"active_source":             activeSource,
		"affected_registered_count": affectedCount,
		"ad_url":                    adURL,
		"ad_use_tls":                adUseTLS != "false",
		"ad_bind_dn":                adBindDN,
		"ad_has_bind_password":      adHasBindPass,
		"ad_base_dn":                adBaseDN,
		"ad_allowed_group":          adAllowedGroup,
		"ad_user_attr":              adUserAttr,
		"ad_mail_attr":              adMailAttr,
		"ad_name_attr":              adNameAttr,
		"radius_host":               radHost,
		"radius_port":               radPort,
		"radius_timeout_ms":         radTimeoutMs,
		"radius_has_secret":         radHasSecret,
	})
}

func (m *DynamicManager) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ActiveSource   string `json:"active_source"`
		ADURL          string `json:"ad_url"`
		ADUseTLS       *bool  `json:"ad_use_tls"`
		ADBindDN       string `json:"ad_bind_dn"`
		ADBindPassword string `json:"ad_bind_password"`
		ADBaseDN       string `json:"ad_base_dn"`
		ADAllowedGroup string `json:"ad_allowed_group"`
		ADUserAttr     string `json:"ad_user_attr"`
		ADMailAttr     string `json:"ad_mail_attr"`
		ADNameAttr     string `json:"ad_name_attr"`
		RadiusHost     string `json:"radius_host"`
		RadiusPort     int    `json:"radius_port"`
		RadiusSecret   string `json:"radius_secret"`
		RadiusTimeout  int    `json:"radius_timeout_ms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求格式")
		return
	}
	if req.ActiveSource != "ad" && req.ActiveSource != "radius" {
		auth.WriteError(w, http.StatusUnprocessableEntity, "invalid_source", "驗證來源必須為 ad 或 radius")
		return
	}

	ctx := r.Context()
	_ = m.settings.Set(ctx, "active_auth_source", req.ActiveSource)

	if req.ActiveSource == "ad" {
		_ = m.settings.Set(ctx, "ad_url", req.ADURL)
		useTLS := "true"
		if req.ADUseTLS != nil && !*req.ADUseTLS {
			useTLS = "false"
		}
		_ = m.settings.Set(ctx, "ad_use_tls", useTLS)
		_ = m.settings.Set(ctx, "ad_bind_dn", req.ADBindDN)
		_ = m.settings.Set(ctx, "ad_base_dn", req.ADBaseDN)
		_ = m.settings.Set(ctx, "ad_allowed_group", req.ADAllowedGroup)
		_ = m.settings.Set(ctx, "ad_user_attr", req.ADUserAttr)
		_ = m.settings.Set(ctx, "ad_mail_attr", req.ADMailAttr)
		_ = m.settings.Set(ctx, "ad_name_attr", req.ADNameAttr)
		if req.ADBindPassword != "" {
			if err := m.settings.SetSecret(ctx, "ad_bind_password", req.ADBindPassword); err != nil {
				auth.WriteError(w, http.StatusInternalServerError, "secret_error", err.Error())
				return
			}
		}
	} else {
		_ = m.settings.Set(ctx, "radius_host", req.RadiusHost)
		if req.RadiusPort > 0 {
			_ = m.settings.Set(ctx, "radius_port", fmt.Sprintf("%d", req.RadiusPort))
		}
		if req.RadiusTimeout > 0 {
			_ = m.settings.Set(ctx, "radius_timeout_ms", fmt.Sprintf("%d", req.RadiusTimeout))
		}
		if req.RadiusSecret != "" {
			if err := m.settings.SetSecret(ctx, "radius_secret", req.RadiusSecret); err != nil {
				auth.WriteError(w, http.StatusInternalServerError, "secret_error", err.Error())
				return
			}
		}
	}

	u := auth.UserFromContext(ctx)
	if m.audit != nil && u != nil {
		m.audit.Log(ctx, u.Username, u.Role, r.RemoteAddr, "settings.auth_source_update", "settings", "auth_source",
			fmt.Sprintf("更新驗證來源為 %s", req.ActiveSource), map[string]any{"active_source": req.ActiveSource})
	}

	m.handleGetConfig(w, r)
}

func (m *DynamicManager) handleTestAuth(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" || req.Password == "" {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "請輸入測試帳號與密碼")
		return
	}

	info, err := m.Authenticate(r.Context(), req.Username, req.Password)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":      true,
		"message":      "驗證測試成功",
		"mail":         info.Mail,
		"display_name": info.DisplayName,
	})
}
