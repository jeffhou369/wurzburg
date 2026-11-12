package wizard

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jeffhou369/wurzburg/backend/internal/audit"
	"github.com/jeffhou369/wurzburg/backend/internal/auth"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/settings"
)

type UnfinishedSetupItem struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Consequence string `json:"consequence"`
	Route       string `json:"route"`
}

type WizardStatus struct {
	Completed       bool                  `json:"completed"`
	CurrentStep     int                   `json:"current_step"`
	UnfinishedItems []UnfinishedSetupItem `json:"unfinished_items"`
}

type Handler struct {
	db       *sql.DB
	clock    clock.Clock
	settings *settings.Store
	audit    *audit.Logger
}

func NewHandler(db *sql.DB, clk clock.Clock, sett *settings.Store, aud *audit.Logger) *Handler {
	if clk == nil {
		clk = clock.NewRealClock()
	}
	return &Handler{db: db, clock: clk, settings: sett, audit: aud}
}

func (h *Handler) RegisterRoutes(api chi.Router) {
	// 園區名稱與時區公開讀取供登入頁／側欄使用，或登入後由管理員修改
	api.With(auth.RequireAuth).Get("/settings/campus", h.handleGetCampus)

	api.Group(func(r chi.Router) {
		r.Use(auth.RequireAdmin)

		r.Put("/settings/campus", h.handleUpdateCampus)
		r.Get("/settings/wizard", h.handleGetWizard)
		r.Put("/settings/wizard", h.handleUpdateWizard)
	})
}

func (h *Handler) handleGetCampus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	campus, _ := h.settings.Get(ctx, "campus_name")
	if campus == "" {
		campus = "Würzburg"
	}
	tz, _ := h.settings.Get(ctx, "timezone")
	if tz == "" {
		tz = "Asia/Taipei"
	}

	publicURL, err := h.settings.Get(ctx, "public_base_url")
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "settings_error", "讀取園區設定失敗")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"campus_name":     campus,
		"timezone":        tz,
		"public_base_url": publicURL,
	})
}

func (h *Handler) handleUpdateCampus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CampusName    string  `json:"campus_name"`
		Timezone      string  `json:"timezone"`
		PublicBaseURL *string `json:"public_base_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求格式")
		return
	}
	req.CampusName = strings.TrimSpace(req.CampusName)
	req.Timezone = strings.TrimSpace(req.Timezone)
	if req.CampusName == "" || req.Timezone == "" {
		auth.WriteError(w, http.StatusUnprocessableEntity, "validation_error", "園區顯示名稱與時區不可為空")
		return
	}

	// 驗證時區合法性
	if _, err := time.LoadLocation(req.Timezone); err != nil {
		auth.WriteError(w, http.StatusUnprocessableEntity, "invalid_timezone", fmt.Sprintf("未知的時區識別碼: %s", req.Timezone))
		return
	}

	values := map[string]string{"campus_name": req.CampusName, "timezone": req.Timezone}
	if req.PublicBaseURL != nil {
		normalized, err := settings.NormalizePublicBaseURL(*req.PublicBaseURL)
		if err != nil {
			auth.WriteError(w, http.StatusUnprocessableEntity, "invalid_public_url", err.Error())
			return
		}
		values["public_base_url"] = normalized
	}
	ctx := r.Context()
	if err := h.settings.SetMany(ctx, values); err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "settings_error", "儲存園區設定失敗")
		return
	}

	u := auth.UserFromContext(ctx)
	if h.audit != nil && u != nil {
		h.audit.Log(ctx, u.Username, u.Role, r.RemoteAddr, "settings.campus_update", "settings", "campus",
			fmt.Sprintf("更新園區資訊：名稱 %s，時區 %s", req.CampusName, req.Timezone),
			map[string]any{"campus_name": req.CampusName, "timezone": req.Timezone})
	}

	h.handleGetCampus(w, r)
}

func (h *Handler) handleGetWizard(w http.ResponseWriter, r *http.Request) {
	status := h.evaluateWizardStatus(r)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(status)
}

func (h *Handler) handleUpdateWizard(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentStep int  `json:"current_step"`
		Completed   bool `json:"completed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求格式")
		return
	}
	if req.CurrentStep < 1 || req.CurrentStep > 5 {
		auth.WriteError(w, http.StatusUnprocessableEntity, "validation_error", "步驟必須介於 1 到 5 之間")
		return
	}

	ctx := r.Context()
	_ = h.settings.Set(ctx, "wizard_current_step", fmt.Sprintf("%d", req.CurrentStep))
	compStr := "false"
	if req.Completed {
		compStr = "true"
	}
	_ = h.settings.Set(ctx, "wizard_completed", compStr)

	u := auth.UserFromContext(ctx)
	if h.audit != nil && u != nil {
		h.audit.Log(ctx, u.Username, u.Role, r.RemoteAddr, "settings.wizard_update", "wizard", fmt.Sprintf("step_%d", req.CurrentStep),
			fmt.Sprintf("更新設定精靈狀態（步驟 %d，完成: %v）", req.CurrentStep, req.Completed),
			map[string]any{"current_step": req.CurrentStep, "completed": req.Completed})
	}

	status := h.evaluateWizardStatus(r)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(status)
}

func (h *Handler) evaluateWizardStatus(r *http.Request) WizardStatus {
	ctx := r.Context()
	curStep := h.settings.GetInt(ctx, "wizard_current_step", 1)
	if curStep < 1 || curStep > 5 {
		curStep = 1
	}

	completedVal, _ := h.settings.Get(ctx, "wizard_completed")
	completed := (completedVal == "true")

	items := make([]UnfinishedSetupItem, 0)

	// 1. 驗證來源
	authSrc, _ := h.settings.Get(ctx, "active_auth_source")
	if authSrc == "" || authSrc == "none" {
		items = append(items, UnfinishedSetupItem{
			Key:         "auth_source",
			Title:       "尚未設定驗證來源",
			Consequence: "一般使用者無法登入",
			Route:       "/settings?tab=auth",
		})
	}

	// 2. 郵件設定
	smtpHost, _ := h.settings.Get(ctx, "smtp_host")
	smtpSender, _ := h.settings.Get(ctx, "smtp_sender")
	if smtpHost == "" || smtpSender == "" {
		items = append(items, UnfinishedSetupItem{
			Key:         "mail",
			Title:       "尚未設定郵件",
			Consequence: "三種通知都寄不出去",
			Route:       "/settings?tab=mail",
		})
	} else if _, err := h.settings.PublicLink(ctx, ""); err != nil {
		items = append(items, UnfinishedSetupItem{
			Key: "mail", Title: "尚未設定通知公開網址",
			Consequence: "三種通知都寄不出去", Route: "/settings?tab=campus",
		})
	}

	// 3. 交換器
	if h.db != nil {
		var swCnt int
		_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM switches WHERE enabled = TRUE`).Scan(&swCnt)
		if swCnt == 0 {
			items = append(items, UnfinishedSetupItem{
				Key:         "switches",
				Title:       "尚未新增交換器",
				Consequence: "沒有任何資料",
				Route:       "/switches",
			})
		}

		// 4. 網段檢視與納管
		var pendingCnt, managedCnt int
		_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subnets WHERE state = 'pending'`).Scan(&pendingCnt)
		_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subnets WHERE state = 'managed'`).Scan(&managedCnt)
		if pendingCnt > 0 || managedCnt == 0 {
			items = append(items, UnfinishedSetupItem{
				Key:         "subnets",
				Title:       "有待檢視的網段或無納管網段",
				Consequence: "管理表是空的",
				Route:       "/subnets",
			})
		}
	}

	// 5. HTTPS 檢查
	isHTTPS := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	if !isHTTPS {
		items = append(items, UnfinishedSetupItem{
			Key:         "https",
			Title:       "尚未啟用 HTTPS",
			Consequence: "帳密以明文傳輸",
			Route:       "/settings?tab=campus",
		})
	}

	return WizardStatus{
		Completed:       completed,
		CurrentStep:     curStep,
		UnfinishedItems: items,
	}
}
