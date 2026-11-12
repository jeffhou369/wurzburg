package switches

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jeffhou369/wurzburg/backend/internal/audit"
	"github.com/jeffhou369/wurzburg/backend/internal/auth"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/crypto"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
)

// Handler 管理 ARP 來源交換器與 VRF 讀取設定之 CRUD 及測試讀取。
type Handler struct {
	db        *sql.DB
	clock     clock.Clock
	cipher    *crypto.Cipher
	audit     *audit.Logger
	collector interfaces.Collector
}

func NewHandler(
	db *sql.DB,
	clk clock.Clock,
	cipher *crypto.Cipher,
	aud *audit.Logger,
	col interfaces.Collector,
) *Handler {
	if clk == nil {
		clk = clock.NewRealClock()
	}
	return &Handler{
		db:        db,
		clock:     clk,
		cipher:    cipher,
		audit:     aud,
		collector: col,
	}
}

func (h *Handler) RegisterRoutes(api chi.Router) {
	api.Group(func(r chi.Router) {
		r.Use(auth.RequireAdmin)
		r.Get("/switches", h.handleList)
		r.Post("/switches", h.handleCreate)
		r.Get("/switches/{id}", h.handleGet)
		r.Put("/switches/{id}", h.handleUpdate)
		r.Delete("/switches/{id}", h.handleDelete)

		r.Post("/switches/{id}/vrfs", h.handleCreateVRF)
		r.Put("/switches/{id}/vrfs/{vrfId}", h.handleUpdateVRF)
		r.Delete("/switches/{id}/vrfs/{vrfId}", h.handleDeleteVRF)
		r.Post("/switches/{id}/vrfs/{vrfId}/test-read", h.handleTestRead)
	})
}

type vrfConfigResp struct {
	ID               int64      `json:"id"`
	SwitchID         int64      `json:"switch_id"`
	VRFName          string     `json:"vrf_name"`
	SNMPContext      string     `json:"snmp_context"`
	HasCredential    bool       `json:"has_credential"`
	IsDefault        bool       `json:"is_default"`
	Enabled          bool       `json:"enabled"`
	LastPollAt       *time.Time `json:"last_poll_at"`
	LastSuccessAt    *time.Time `json:"last_success_at"`
	LastStatus       string     `json:"last_status"`
	LastErrorClass   string     `json:"last_error_class"`
	LastErrorSummary string     `json:"last_error_summary"`
	LastLearnedCount int        `json:"last_learned_count"`
}

type switchResp struct {
	ID           int64           `json:"id"`
	Name         string          `json:"name"`
	MgmtIP       string          `json:"mgmt_ip"`
	Platform     string          `json:"platform"`
	SNMPVersion  string          `json:"snmp_version"`
	SNMPPort     int             `json:"snmp_port"`
	TimeoutMs    int             `json:"timeout_ms"`
	Enabled      bool            `json:"enabled"`
	RoleLabel    string          `json:"role_label"`
	Notes        string          `json:"notes"`
	V3Username   string          `json:"v3_username,omitempty"`
	HasV3Warning bool            `json:"has_v3_warning"`
	HasV3Creds   bool            `json:"has_v3_creds"`
	VRFConfigs   []vrfConfigResp `json:"vrf_configs"`
}

func (h *Handler) handleList(w http.ResponseWriter, r *http.Request) {
	items, err := h.querySwitches(r.Context(), 0)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "查詢交換器清單失敗")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
}

func (h *Handler) handleGet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的交換器 ID")
		return
	}
	items, err := h.querySwitches(r.Context(), id)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "查詢交換器失敗")
		return
	}
	if len(items) == 0 {
		auth.WriteError(w, http.StatusNotFound, "not_found", "找不到指定交換器")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(items[0])
}

func (h *Handler) querySwitches(ctx context.Context, filterID int64) ([]switchResp, error) {
	query := `SELECT id, name, mgmt_ip, platform, snmp_version, snmp_port, timeout_ms, enabled, role_label, COALESCE(notes, ''), v3_username, COALESCE(v3_auth_pass_encrypted, '')
	          FROM switches`
	var args []any
	if filterID > 0 {
		query += ` WHERE id = ?`
		args = append(args, filterID)
	}
	query += ` ORDER BY id ASC`

	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []switchResp
	for rows.Next() {
		var s switchResp
		var v3PassEnc string
		if err := rows.Scan(&s.ID, &s.Name, &s.MgmtIP, &s.Platform, &s.SNMPVersion, &s.SNMPPort, &s.TimeoutMs, &s.Enabled, &s.RoleLabel, &s.Notes, &s.V3Username, &v3PassEnc); err != nil {
			continue
		}
		s.HasV3Creds = v3PassEnc != ""
		vrfs, _ := h.queryVRFs(ctx, s.ID)
		s.VRFConfigs = vrfs
		s.HasV3Warning = len(vrfs) <= 1
		result = append(result, s)
	}
	if result == nil {
		result = []switchResp{}
	}
	return result, nil
}

func (h *Handler) queryVRFs(ctx context.Context, switchID int64) ([]vrfConfigResp, error) {
	rows, err := h.db.QueryContext(
		ctx,
		`SELECT id, switch_id, vrf_name, snmp_context, COALESCE(snmp_community_encrypted, ''), is_default, enabled,
		        last_poll_at, last_success_at, last_status, last_error_class, COALESCE(last_error_summary, ''), last_learned_count
		 FROM vrf_read_configs WHERE switch_id = ? ORDER BY is_default DESC, id ASC`,
		switchID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []vrfConfigResp
	for rows.Next() {
		var v vrfConfigResp
		var commEnc string
		var lp, ls sql.NullTime
		if err := rows.Scan(&v.ID, &v.SwitchID, &v.VRFName, &v.SNMPContext, &commEnc, &v.IsDefault, &v.Enabled,
			&lp, &ls, &v.LastStatus, &v.LastErrorClass, &v.LastErrorSummary, &v.LastLearnedCount); err != nil {
			continue
		}
		v.HasCredential = commEnc != ""
		if lp.Valid {
			t := lp.Time
			v.LastPollAt = &t
		}
		if ls.Valid {
			t := ls.Time
			v.LastSuccessAt = &t
		}
		out = append(out, v)
	}
	if out == nil {
		out = []vrfConfigResp{}
	}
	return out, nil
}

type switchPayload struct {
	Name           string `json:"name"`
	MgmtIP         string `json:"mgmt_ip"`
	Platform       string `json:"platform"`
	SNMPVersion    string `json:"snmp_version"`
	SNMPPort       int    `json:"snmp_port"`
	TimeoutMs      int    `json:"timeout_ms"`
	Enabled        *bool  `json:"enabled"`
	RoleLabel      string `json:"role_label"`
	Notes          string `json:"notes"`
	SNMPCommunity  string `json:"snmp_community"`
	Community      string `json:"community"`
	V3Username     string `json:"v3_username"`
	V3User         string `json:"v3_user"`
	V3AuthProtocol string `json:"v3_auth_protocol"`
	V3AuthProto    string `json:"v3_auth_proto"`
	V3AuthPass     string `json:"v3_auth_pass"`
	V3PrivProtocol string `json:"v3_priv_protocol"`
	V3PrivProto    string `json:"v3_priv_proto"`
	V3PrivPass     string `json:"v3_priv_pass"`
}

func validateSwitchPayload(p *switchPayload, isCreate bool) error {
	if p.SNMPCommunity == "" && p.Community != "" {
		p.SNMPCommunity = p.Community
	}
	if p.V3Username == "" && p.V3User != "" {
		p.V3Username = p.V3User
	}
	if p.V3AuthProtocol == "" && p.V3AuthProto != "" {
		p.V3AuthProtocol = p.V3AuthProto
	}
	if p.V3PrivProtocol == "" && p.V3PrivProto != "" {
		p.V3PrivProtocol = p.V3PrivProto
	}
	if p.Name == "" {
		return errors.New("交換器名稱不可為空")
	}
	if net.ParseIP(p.MgmtIP).To4() == nil {
		return errors.New("管理 IP 必須為有效的 IPv4 位址")
	}
	if p.Platform != "ios-xe" && p.Platform != "nx-os" {
		return errors.New("平台目前僅支援 ios-xe 或 nx-os")
	}
	if p.SNMPVersion != "v2c" && p.SNMPVersion != "v3" {
		return errors.New("SNMP 版本必須為 v2c 或 v3")
	}
	if p.SNMPPort == 0 {
		p.SNMPPort = 161
	}
	if p.SNMPPort < 1 || p.SNMPPort > 65535 {
		return errors.New("SNMP 埠號必須介於 1 到 65535")
	}
	if p.TimeoutMs == 0 {
		p.TimeoutMs = 3000
	}
	if p.TimeoutMs < 500 || p.TimeoutMs > 60000 {
		return errors.New("逾時時間必須介於 500 到 60000 毫秒")
	}
	if isCreate {
		if p.SNMPVersion == "v2c" && p.SNMPCommunity == "" {
			return errors.New("使用 SNMPv2c 時必須提供 Community")
		}
		if p.SNMPVersion == "v3" && p.V3Username == "" {
			return errors.New("使用 SNMPv3 時必須提供使用者名稱")
		}
	}
	return nil
}

func (h *Handler) encryptSecret(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	if h.cipher == nil {
		return "", errors.New("加密處理器未初始化 (請檢查 WURZBURG_SECRET_KEY)")
	}
	return h.cipher.Encrypt(plain)
}

func (h *Handler) handleCreate(w http.ResponseWriter, r *http.Request) {
	var p switchPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求格式")
		return
	}
	if err := validateSwitchPayload(&p, true); err != nil {
		auth.WriteError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}

	enabled := true
	if p.Enabled != nil {
		enabled = *p.Enabled
	}
	if p.RoleLabel == "" {
		p.RoleLabel = "核心"
	}
	if p.V3AuthProtocol == "" {
		p.V3AuthProtocol = "SHA256"
	}
	if p.V3PrivProtocol == "" {
		p.V3PrivProtocol = "AES"
	}

	commEnc, err := h.encryptSecret(p.SNMPCommunity)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "secret_error", err.Error())
		return
	}
	v3AuthEnc, err := h.encryptSecret(p.V3AuthPass)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "secret_error", err.Error())
		return
	}
	v3PrivEnc, err := h.encryptSecret(p.V3PrivPass)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "secret_error", err.Error())
		return
	}

	now := h.clock.Now()
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "交易啟動失敗")
		return
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(
		r.Context(),
		`INSERT INTO switches (name, mgmt_ip, platform, snmp_version, v3_username, v3_auth_protocol, v3_auth_pass_encrypted, v3_priv_protocol, v3_priv_pass_encrypted, snmp_port, timeout_ms, enabled, role_label, notes, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.MgmtIP, p.Platform, p.SNMPVersion, p.V3Username, p.V3AuthProtocol, v3AuthEnc, p.V3PrivProtocol, v3PrivEnc,
		p.SNMPPort, p.TimeoutMs, enabled, p.RoleLabel, p.Notes, now, now,
	)
	if err != nil {
		auth.WriteError(w, http.StatusConflict, "duplicate_switch", "此管理 IP 交換器已存在")
		return
	}
	swID, _ := res.LastInsertId()

	// 自動建立預設全域 VRF 讀取設定
	if _, err := tx.ExecContext(
		r.Context(),
		`INSERT INTO vrf_read_configs (switch_id, vrf_name, snmp_context, snmp_community_encrypted, is_default, enabled, created_at, updated_at)
		 VALUES (?, 'default', '', ?, TRUE, TRUE, ?, ?)`,
		swID, commEnc, now, now,
	); err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "建立預設 VRF 讀取設定失敗")
		return
	}

	if err := tx.Commit(); err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "寫入資料庫失敗")
		return
	}

	u := auth.UserFromContext(r.Context())
	if h.audit != nil && u != nil {
		h.audit.Log(r.Context(), u.Username, u.Role, r.RemoteAddr, "switch.create", "switch", fmt.Sprintf("%d", swID),
			fmt.Sprintf("新增交換器 %s (%s)", p.Name, p.MgmtIP), map[string]any{"platform": p.Platform, "snmp_version": p.SNMPVersion})
	}

	items, _ := h.querySwitches(r.Context(), swID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	if len(items) > 0 {
		_ = json.NewEncoder(w).Encode(items[0])
	}
}

func (h *Handler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的交換器 ID")
		return
	}

	var p switchPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求格式")
		return
	}
	if err := validateSwitchPayload(&p, false); err != nil {
		auth.WriteError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}

	enabled := true
	if p.Enabled != nil {
		enabled = *p.Enabled
	}

	now := h.clock.Now()
	_, err = h.db.ExecContext(
		r.Context(),
		`UPDATE switches SET name = ?, mgmt_ip = ?, platform = ?, snmp_version = ?, snmp_port = ?, timeout_ms = ?, enabled = ?, role_label = ?, notes = ?, updated_at = ?
		 WHERE id = ?`,
		p.Name, p.MgmtIP, p.Platform, p.SNMPVersion, p.SNMPPort, p.TimeoutMs, enabled, p.RoleLabel, p.Notes, now, id,
	)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "更新交換器失敗")
		return
	}

	// 若未留空則更新憑證
	if p.V3Username != "" {
		_, _ = h.db.ExecContext(r.Context(), `UPDATE switches SET v3_username = ? WHERE id = ?`, p.V3Username, id)
	}
	if p.V3AuthPass != "" {
		if enc, err := h.encryptSecret(p.V3AuthPass); err == nil {
			_, _ = h.db.ExecContext(r.Context(), `UPDATE switches SET v3_auth_pass_encrypted = ? WHERE id = ?`, enc, id)
		}
	}
	if p.V3PrivPass != "" {
		if enc, err := h.encryptSecret(p.V3PrivPass); err == nil {
			_, _ = h.db.ExecContext(r.Context(), `UPDATE switches SET v3_priv_pass_encrypted = ? WHERE id = ?`, enc, id)
		}
	}
	if p.SNMPCommunity != "" {
		if enc, err := h.encryptSecret(p.SNMPCommunity); err == nil {
			_, _ = h.db.ExecContext(r.Context(), `UPDATE vrf_read_configs SET snmp_community_encrypted = ? WHERE switch_id = ? AND is_default = TRUE`, enc, id)
		}
	}

	// 停用整台交換器時，立即移除其名下所有 ARP 觀測並重算受影響 IP 的離線狀態
	if !enabled {
		h.cleanupSightingsForSwitch(r.Context(), id)
	}

	u := auth.UserFromContext(r.Context())
	if h.audit != nil && u != nil {
		h.audit.Log(r.Context(), u.Username, u.Role, r.RemoteAddr, "switch.update", "switch", fmt.Sprintf("%d", id),
			fmt.Sprintf("更新交換器 %s 設定 (enabled=%v)", p.Name, enabled), nil)
	}

	items, _ := h.querySwitches(r.Context(), id)
	if len(items) == 0 {
		auth.WriteError(w, http.StatusNotFound, "not_found", "找不到交換器")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(items[0])
}

func (h *Handler) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的交換器 ID")
		return
	}

	// 先記錄受影響的 IP 並刪除觀測
	h.cleanupSightingsForSwitch(r.Context(), id)

	res, err := h.db.ExecContext(r.Context(), `DELETE FROM switches WHERE id = ?`, id)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "刪除交換器失敗")
		return
	}
	if aff, _ := res.RowsAffected(); aff == 0 {
		auth.WriteError(w, http.StatusNotFound, "not_found", "找不到交換器")
		return
	}

	u := auth.UserFromContext(r.Context())
	if h.audit != nil && u != nil {
		h.audit.Log(r.Context(), u.Username, u.Role, r.RemoteAddr, "switch.delete", "switch", fmt.Sprintf("%d", id), "刪除交換器與其名下讀取設定", nil)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleCreateVRF(w http.ResponseWriter, r *http.Request) {
	swID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的交換器 ID")
		return
	}

	var req struct {
		VRFName       string `json:"vrf_name"`
		SNMPContext   string `json:"snmp_context"`
		SNMPCommunity string `json:"snmp_community"`
		Enabled       *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.VRFName == "" {
		auth.WriteError(w, http.StatusUnprocessableEntity, "validation_error", "VRF 名稱不可為空")
		return
	}

	var snmpVer string
	if err := h.db.QueryRowContext(r.Context(), `SELECT snmp_version FROM switches WHERE id = ?`, swID).Scan(&snmpVer); err != nil {
		auth.WriteError(w, http.StatusNotFound, "not_found", "找不到交換器")
		return
	}

	if snmpVer == "v3" && req.SNMPContext == "" {
		auth.WriteError(w, http.StatusUnprocessableEntity, "validation_error", "SNMPv3 必須指定 Context 名稱")
		return
	}
	if snmpVer == "v2c" && req.SNMPCommunity == "" {
		auth.WriteError(w, http.StatusUnprocessableEntity, "validation_error", "SNMPv2c 必須為此 VRF 指定專屬 Community")
		return
	}

	commEnc, err := h.encryptSecret(req.SNMPCommunity)
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "secret_error", err.Error())
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	now := h.clock.Now()
	_, err = h.db.ExecContext(
		r.Context(),
		`INSERT INTO vrf_read_configs (switch_id, vrf_name, snmp_context, snmp_community_encrypted, is_default, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, FALSE, ?, ?, ?)`,
		swID, req.VRFName, req.SNMPContext, commEnc, enabled, now, now,
	)
	if err != nil {
		auth.WriteError(w, http.StatusConflict, "duplicate_vrf", "此交換器已存在同名之 VRF 讀取設定")
		return
	}

	vrfs, _ := h.queryVRFs(r.Context(), swID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"items": vrfs})
}

func (h *Handler) handleUpdateVRF(w http.ResponseWriter, r *http.Request) {
	swID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	vrfID, err := strconv.ParseInt(chi.URLParam(r, "vrfId"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的 VRF 設定 ID")
		return
	}

	var req struct {
		VRFName       string `json:"vrf_name"`
		SNMPContext   string `json:"snmp_context"`
		SNMPCommunity string `json:"snmp_community"`
		Enabled       *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求格式")
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	now := h.clock.Now()

	if req.VRFName != "" {
		_, _ = h.db.ExecContext(r.Context(), `UPDATE vrf_read_configs SET vrf_name = ?, snmp_context = ?, enabled = ?, updated_at = ? WHERE id = ? AND switch_id = ?`,
			req.VRFName, req.SNMPContext, enabled, now, vrfID, swID)
	} else {
		_, _ = h.db.ExecContext(r.Context(), `UPDATE vrf_read_configs SET snmp_context = ?, enabled = ?, updated_at = ? WHERE id = ? AND switch_id = ?`,
			req.SNMPContext, enabled, now, vrfID, swID)
	}

	if req.SNMPCommunity != "" {
		if enc, err := h.encryptSecret(req.SNMPCommunity); err == nil {
			_, _ = h.db.ExecContext(r.Context(), `UPDATE vrf_read_configs SET snmp_community_encrypted = ? WHERE id = ? AND switch_id = ?`, enc, vrfID, swID)
		}
	}

	// 停用此 VRF 讀取設定時，立即移除其觀測並重算受影響 IP 離線狀態
	if !enabled {
		h.cleanupSightingsForVRF(r.Context(), vrfID)
	}

	vrfs, _ := h.queryVRFs(r.Context(), swID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": vrfs})
}

func (h *Handler) handleDeleteVRF(w http.ResponseWriter, r *http.Request) {
	swID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	vrfID, err := strconv.ParseInt(chi.URLParam(r, "vrfId"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的 VRF ID")
		return
	}

	var isDefault bool
	err = h.db.QueryRowContext(r.Context(), `SELECT is_default FROM vrf_read_configs WHERE id = ? AND switch_id = ?`, vrfID, swID).Scan(&isDefault)
	if errors.Is(err, sql.ErrNoRows) {
		auth.WriteError(w, http.StatusNotFound, "not_found", "找不到 VRF 讀取設定")
		return
	}
	if isDefault {
		auth.WriteError(w, http.StatusConflict, "cannot_delete_default_vrf", "全域預設 VRF 讀取設定不可刪除")
		return
	}

	h.cleanupSightingsForVRF(r.Context(), vrfID)
	_, _ = h.db.ExecContext(r.Context(), `DELETE FROM vrf_read_configs WHERE id = ? AND switch_id = ?`, vrfID, swID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleTestRead(w http.ResponseWriter, r *http.Request) {
	swID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	vrfID, err := strconv.ParseInt(chi.URLParam(r, "vrfId"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的 ID")
		return
	}

	if h.collector == nil {
		auth.WriteError(w, http.StatusServiceUnavailable, "collector_unavailable", "採集模組未就緒")
		return
	}

	input, err := h.buildVRFReadInput(r.Context(), swID, vrfID)
	if err != nil {
		auth.WriteError(w, http.StatusNotFound, "not_found", "找不到交換器或 VRF 設定")
		return
	}

	out, collectErr := h.collector.Collect(r.Context(), input)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if collectErr != nil {
		errClass := "unknown"
		var ce *interfaces.CollectError
		if errors.As(collectErr, &ce) {
			errClass = string(ce.Class)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success":       false,
			"arp_count":     0,
			"subnet_count":  0,
			"error_class":   errClass,
			"error_summary": collectErr.Error(),
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":      true,
		"arp_count":    len(out.ARPObservations),
		"subnet_count": len(out.InterfaceAddresses),
	})
}

func (h *Handler) buildVRFReadInput(ctx context.Context, swID, vrfID int64) (interfaces.VRFReadInput, error) {
	var in interfaces.VRFReadInput
	var commEnc, authPassEnc, privPassEnc string
	err := h.db.QueryRowContext(
		ctx,
		`SELECT s.id, v.id, s.mgmt_ip, s.platform, s.snmp_version, s.snmp_port, s.timeout_ms,
		        v.vrf_name, v.snmp_context, COALESCE(v.snmp_community_encrypted, ''),
		        s.v3_username, s.v3_auth_protocol, COALESCE(s.v3_auth_pass_encrypted, ''),
		        s.v3_priv_protocol, COALESCE(s.v3_priv_pass_encrypted, '')
		 FROM switches s
		 JOIN vrf_read_configs v ON s.id = v.switch_id
		 WHERE s.id = ? AND v.id = ?`,
		swID, vrfID,
	).Scan(
		&in.SwitchID, &in.VRFConfigID, &in.MgmtIP, &in.Platform, &in.SNMPVersion, &in.SNMPPort, &in.TimeoutMs,
		&in.VRFName, &in.SNMPContext, &commEnc,
		&in.V3Username, &in.V3AuthProto, &authPassEnc, &in.V3PrivProto, &privPassEnc,
	)
	if err != nil {
		return in, err
	}

	if h.cipher != nil {
		if commEnc != "" {
			in.SNMPCommunity, _ = h.cipher.Decrypt(commEnc)
		}
		if authPassEnc != "" {
			in.V3AuthPass, _ = h.cipher.Decrypt(authPassEnc)
		}
		if privPassEnc != "" {
			in.V3PrivPass, _ = h.cipher.Decrypt(privPassEnc)
		}
	}
	return in, nil
}

func (h *Handler) cleanupSightingsForSwitch(ctx context.Context, switchID int64) {
	rows, err := h.db.QueryContext(ctx, `SELECT DISTINCT ip_record_id FROM arp_sightings WHERE switch_id = ?`, switchID)
	if err != nil {
		return
	}
	var affectedIPs []int64
	for rows.Next() {
		var ipID int64
		if rows.Scan(&ipID) == nil {
			affectedIPs = append(affectedIPs, ipID)
		}
	}
	_ = rows.Close()

	_, _ = h.db.ExecContext(ctx, `DELETE FROM arp_sightings WHERE switch_id = ?`, switchID)
	h.recalculatePresenceForIPs(ctx, affectedIPs)
}

func (h *Handler) cleanupSightingsForVRF(ctx context.Context, vrfConfigID int64) {
	rows, err := h.db.QueryContext(ctx, `SELECT DISTINCT ip_record_id FROM arp_sightings WHERE vrf_config_id = ?`, vrfConfigID)
	if err != nil {
		return
	}
	var affectedIPs []int64
	for rows.Next() {
		var ipID int64
		if rows.Scan(&ipID) == nil {
			affectedIPs = append(affectedIPs, ipID)
		}
	}
	_ = rows.Close()

	_, _ = h.db.ExecContext(ctx, `DELETE FROM arp_sightings WHERE vrf_config_id = ?`, vrfConfigID)
	h.recalculatePresenceForIPs(ctx, affectedIPs)
}

func (h *Handler) recalculatePresenceForIPs(ctx context.Context, ipIDs []int64) {
	for _, ipID := range ipIDs {
		var remaining int
		_ = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM arp_sightings WHERE ip_record_id = ?`, ipID).Scan(&remaining)
		if remaining == 0 {
			_, _ = h.db.ExecContext(ctx, `UPDATE ip_records SET presence = 'offline', updated_at = ? WHERE id = ?`, h.clock.Now(), ipID)
		}
	}
}
