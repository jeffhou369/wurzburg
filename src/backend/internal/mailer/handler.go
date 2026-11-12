package mailer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jeffhou369/wurzburg/backend/internal/audit"
	"github.com/jeffhou369/wurzburg/backend/internal/auth"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
	"github.com/jeffhou369/wurzburg/backend/internal/settings"
)

type Handler struct {
	db           *sql.DB
	clock        clock.Clock
	settings     *settings.Store
	audit        *audit.Logger
	mailer       *SMTPMailer
	customMailer interfaces.Mailer // 若為測試注入替身則優先使用
}

func NewHandler(db *sql.DB, clk clock.Clock, sett *settings.Store, aud *audit.Logger, m *SMTPMailer) *Handler {
	if clk == nil {
		clk = clock.NewRealClock()
	}
	return &Handler{db: db, clock: clk, settings: sett, audit: aud, mailer: m}
}

func (h *Handler) SetTestMailer(m interfaces.Mailer) {
	h.customMailer = m
}

func (h *Handler) RegisterRoutes(api chi.Router) {
	api.Group(func(r chi.Router) {
		r.Use(auth.RequireAdmin)

		r.Get("/settings/mail", h.handleGetMailSettings)
		r.Put("/settings/mail", h.handleUpdateMailSettings)
		r.Post("/settings/mail/test", h.handleSendTestMail)

		r.Get("/notifications/logs", h.handleListLogs)
		r.Get("/notifications/states", h.handleListStates)
		r.Post("/notifications/states/{dedupKey}/reset", h.handleResetDedupState)
	})
}

func (h *Handler) handleGetMailSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	host, _ := h.settings.Get(ctx, "smtp_host")
	port := h.settings.GetInt(ctx, "smtp_port", 587)
	tlsMode, _ := h.settings.Get(ctx, "smtp_tls_mode")
	if tlsMode == "" {
		tlsMode = "starttls"
	}
	user, _ := h.settings.Get(ctx, "smtp_username")
	hasPass, _ := h.settings.HasSecret(ctx, "smtp_password")
	sender, _ := h.settings.Get(ctx, "smtp_sender")
	recipientsRaw, _ := h.settings.Get(ctx, "admin_recipients")
	weeklyCron, _ := h.settings.Get(ctx, "weekly_summary_cron")
	if weeklyCron == "" {
		weeklyCron = "0 9 * * 1"
	}

	var recipients []string
	if recipientsRaw != "" {
		_ = json.Unmarshal([]byte(recipientsRaw), &recipients)
	}
	if recipients == nil {
		recipients = []string{}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"smtp_host":           host,
		"smtp_port":           port,
		"smtp_tls_mode":       tlsMode,
		"smtp_username":       user,
		"has_password":        hasPass,
		"sender_address":      sender,
		"admin_recipients":    recipients,
		"weekly_summary_cron": weeklyCron,
	})
}

func (h *Handler) handleUpdateMailSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SMTPHost          string   `json:"smtp_host"`
		SMTPPort          int      `json:"smtp_port"`
		SMTPTLSMode       string   `json:"smtp_tls_mode"`
		SMTPUsername      string   `json:"smtp_username"`
		SMTPPassword      string   `json:"smtp_password"`
		SenderAddress     string   `json:"sender_address"`
		AdminRecipients   []string `json:"admin_recipients"`
		WeeklySummaryCron string   `json:"weekly_summary_cron"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求格式")
		return
	}
	if req.SMTPHost == "" || req.SenderAddress == "" {
		auth.WriteError(w, http.StatusUnprocessableEntity, "validation_error", "SMTP 主機與寄件人信箱不可為空")
		return
	}
	if req.SMTPPort <= 0 {
		req.SMTPPort = 587
	}
	if req.SMTPTLSMode != "starttls" && req.SMTPTLSMode != "smtps" && req.SMTPTLSMode != "none" {
		req.SMTPTLSMode = "starttls"
	}

	ctx := r.Context()
	if req.WeeklySummaryCron != "" {
		if err := validateWeeklyCron(req.WeeklySummaryCron); err != nil {
			auth.WriteError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
			return
		}
		_ = h.settings.Set(ctx, "weekly_summary_cron", strings.TrimSpace(req.WeeklySummaryCron))
	}

	_ = h.settings.Set(ctx, "smtp_host", req.SMTPHost)
	_ = h.settings.Set(ctx, "smtp_port", fmt.Sprintf("%d", req.SMTPPort))
	_ = h.settings.Set(ctx, "smtp_tls_mode", req.SMTPTLSMode)
	_ = h.settings.Set(ctx, "smtp_username", req.SMTPUsername)
	_ = h.settings.Set(ctx, "smtp_sender", req.SenderAddress)

	rcptJSON, _ := json.Marshal(req.AdminRecipients)
	_ = h.settings.Set(ctx, "admin_recipients", string(rcptJSON))

	if req.SMTPPassword != "" {
		if err := h.settings.SetSecret(ctx, "smtp_password", req.SMTPPassword); err != nil {
			auth.WriteError(w, http.StatusInternalServerError, "secret_error", err.Error())
			return
		}
	}

	u := auth.UserFromContext(ctx)
	if h.audit != nil && u != nil {
		h.audit.Log(ctx, u.Username, u.Role, r.RemoteAddr, "settings.mail_update", "settings", "mail",
			"更新 SMTP 與郵件通知設定", map[string]any{"smtp_host": req.SMTPHost, "smtp_port": req.SMTPPort})
	}

	h.handleGetMailSettings(w, r)
}

func (h *Handler) handleSendTestMail(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Recipient string `json:"recipient"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Recipient) == "" {
		auth.WriteError(w, http.StatusUnprocessableEntity, "validation_error", "請輸入測試信收件人信箱")
		return
	}

	ctx := r.Context()
	campusName, _ := h.settings.Get(ctx, "campus_name")
	if campusName == "" {
		campusName = "Würzburg"
	}
	tz, _ := h.settings.Get(ctx, "timezone")
	if tz == "" {
		tz = "Asia/Taipei"
	}

	data := EmailTemplateData{
		CampusName: campusName,
		Subject:    fmt.Sprintf("[%s] 郵件通知測試信", campusName),
		Preheader:  "確認系統 SMTP 與 HTML 排版顯示正常",
		Greeting:   "系統管理員您好：",
		Intro:      "這是一封由 Würzburg 系統設定頁發送的測試信件，用來確認 SMTP 連線與通知版面運作正常。",
		Timezone:   tz,
		SummaryItems: []SummaryKV{
			{Label: "連線狀態", Value: "測試中", Tone: "success"},
			{Label: "版面格式", Value: "HTML + 純文字", Tone: "default"},
		},
		Columns: []string{"狀態", "項目", "說明"},
		Rows: [][]EmailCell{
			{
				{Text: "[在線]", BadgeTone: "online"},
				{Text: "SMTP 寄送", Monospace: false},
				{Text: "測試 multipart/alternative 傳輸"},
			},
		},
		PrimaryCTA: &CTAButton{
			Label: "前往系統檢視",
			URL:   "http://localhost:8080/",
		},
		FooterLines: []string{"此信為手動觸發之 SMTP 測試信件。"},
	}

	htmlBody, textBody, err := RenderEmail(data)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "template_error", err.Error())
		return
	}

	msg := interfaces.MailMessage{
		To:       []string{strings.TrimSpace(req.Recipient)},
		Subject:  data.Subject,
		TextBody: textBody,
		HTMLBody: htmlBody,
	}

	var sendErr error
	if h.customMailer != nil {
		sendErr = h.customMailer.Send(ctx, msg)
	} else {
		_, sendErr = h.mailer.SendWithTracking(ctx, "test_mail", "", msg)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if sendErr != nil {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"message": sendErr.Error(),
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"message": "測試信已成功寄出",
	})
}

func (h *Handler) handleListLogs(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	statusFilter := r.URL.Query().Get("status")

	where := ""
	var args []any
	if statusFilter != "" {
		where = ` WHERE status = ?`
		args = append(args, statusFilter)
	}

	var totalItems int
	_ = h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM notification_logs`+where, args...).Scan(&totalItems)

	offset := (page - 1) * pageSize
	args = append(args, pageSize, offset)

	rows, err := h.db.QueryContext(
		r.Context(),
		`SELECT id, notif_type, dedup_key, recipient, subject, status, attempt_count, COALESCE(last_error, ''), sent_at, created_at
		 FROM notification_logs`+where+` ORDER BY id DESC LIMIT ? OFFSET ?`,
		args...,
	)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "查詢通知紀錄失敗")
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var nType, dKey, rcpt, subj, status, lastErr string
		var attempts int
		var sentAtNull sql.NullTime
		var createdAt time.Time
		if rows.Scan(&id, &nType, &dKey, &rcpt, &subj, &status, &attempts, &lastErr, &sentAtNull, &createdAt) == nil {
			var sentAtVal any
			if sentAtNull.Valid {
				sentAtVal = sentAtNull.Time
			}
			items = append(items, map[string]any{
				"id":            id,
				"notif_type":    nType,
				"dedup_key":     dKey,
				"recipient":     rcpt,
				"subject":       subj,
				"status":        status,
				"attempt_count": attempts,
				"last_error":    lastErr,
				"sent_at":       sentAtVal,
				"created_at":    createdAt,
			})
		}
	}

	totalPages := 0
	if totalItems > 0 {
		totalPages = (totalItems + pageSize - 1) / pageSize
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"items": items,
		"pagination": map[string]int{
			"page":        page,
			"page_size":   pageSize,
			"total_items": totalItems,
			"total_pages": totalPages,
		},
	})
}

func validateWeeklyCron(expr string) error {
	fields := strings.Fields(strings.TrimSpace(expr))
	if len(fields) != 5 {
		return fmt.Errorf("每週排程 Cron 格式必須為 5 個欄位（如 0 9 * * 1）")
	}
	m, err := strconv.Atoi(fields[0])
	if err != nil || m < 0 || m > 59 {
		return fmt.Errorf("分鐘數必須為 0 到 59")
	}
	hr, err := strconv.Atoi(fields[1])
	if err != nil || hr < 0 || hr > 23 {
		return fmt.Errorf("小時數必須為 0 到 23")
	}
	if fields[4] != "*" {
		d, err := strconv.Atoi(fields[4])
		if err != nil || d < 0 || d > 7 {
			return fmt.Errorf("星期欄位必須為 * 或 0 到 7")
		}
	}
	return nil
}

func (h *Handler) handleListStates(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	notifType := r.URL.Query().Get("notif_type")

	where := ` WHERE is_notified = TRUE`
	var args []any
	if notifType != "" {
		where += ` AND notif_type = ?`
		args = append(args, notifType)
	}

	var totalItems int
	_ = h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM notification_states`+where, args...).Scan(&totalItems)

	offset := (page - 1) * pageSize
	args = append(args, pageSize, offset)

	rows, err := h.db.QueryContext(
		r.Context(),
		`SELECT id, dedup_key, notif_type, is_notified, notified_at, updated_at
		 FROM notification_states`+where+` ORDER BY notified_at DESC LIMIT ? OFFSET ?`,
		args...,
	)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "查詢通知狀態失敗")
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var dKey, nType string
		var isNotified bool
		var notifiedAt, updatedAt time.Time
		if rows.Scan(&id, &dKey, &nType, &isNotified, &notifiedAt, &updatedAt) == nil {
			items = append(items, map[string]any{
				"id":          id,
				"dedup_key":   dKey,
				"notif_type":  nType,
				"is_notified": isNotified,
				"notified_at": notifiedAt,
				"updated_at":  updatedAt,
			})
		}
	}

	totalPages := 0
	if totalItems > 0 {
		totalPages = (totalItems + pageSize - 1) / pageSize
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"items": items,
		"pagination": map[string]int{
			"page":        page,
			"page_size":   pageSize,
			"total_items": totalItems,
			"total_pages": totalPages,
		},
	})
}

func (h *Handler) handleResetDedupState(w http.ResponseWriter, r *http.Request) {
	dedupKey := chi.URLParam(r, "dedupKey")
	if decoded, err := url.PathUnescape(dedupKey); err == nil {
		dedupKey = decoded
	}
	if dedupKey == "" {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "必須指定去重鍵")
		return
	}
	_, err := h.db.ExecContext(r.Context(), `DELETE FROM notification_states WHERE dedup_key = ?`, dedupKey)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "重設去重狀態失敗")
		return
	}
	u := auth.UserFromContext(r.Context())
	if h.audit != nil && u != nil {
		h.audit.Log(r.Context(), u.Username, u.Role, r.RemoteAddr, "notification.reset_dedup", "notification_state", dedupKey,
			"手動重設未登記通知去重狀態", nil)
	}
	w.WriteHeader(http.StatusNoContent)
}

// CleanupOldLogs 清除超過保留期（預設 1 年）之通知紀錄。
func (h *Handler) CleanupOldLogs(ctx context.Context) (int64, error) {
	if h.db == nil {
		return 0, nil
	}
	days := 365
	if h.settings != nil {
		days = h.settings.GetInt(ctx, "notification_retention_days", 365)
	}
	if days <= 0 {
		return 0, nil
	}
	cutoff := h.clock.Now().AddDate(0, 0, -days)
	res, err := h.db.ExecContext(ctx, `DELETE FROM notification_logs WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
