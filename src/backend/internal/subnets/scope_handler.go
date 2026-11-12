package subnets

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"
	"github.com/jeffhou369/wurzburg/backend/internal/audit"
	"github.com/jeffhou369/wurzburg/backend/internal/auth"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
)

const (
	// 預覽金鑰與 token 的領域分隔字串，避免同一把金鑰在不同用途間互相通用。
	previewKeyDomain   = "wurzburg/scope-preview-key/v1"
	previewTokenDomain = "wurzburg/scope-preview-token/v1"

	// 一次 DELETE／SELECT 處理的列數上限，避免產生過長的 SQL 與過多的佔位符。
	scopeDeleteChunk = 200

	// MariaDB／MySQL 錯誤編號。
	mysqlErrDuplicateEntry = 1062
	mysqlErrLockWait       = 1205
	mysqlErrDeadlock       = 1213
)

// ScopeService 實作網段發現、狀態管理、排除範圍與刪除預覽確認流程（Ticket 12 & 24）。
//
// 所有會刪除 IP 紀錄的範圍變更（修正 CIDR、改為不納管、新增排除範圍）都在「同一個資料庫交易」內完成：
// 先取得範圍鎖，再於鎖內重新計算受影響的紀錄並核對預覽 token，接著依序刪除 IP 紀錄與通知去重鍵、
// 寫入刪除明細稽核、變更範圍本體、寫入範圍摘要稽核，最後才提交。任何一步失敗都整筆回滾，
// 不會留下「IP 紀錄已刪、範圍沒改」或「範圍已改、沒有稽核」的半完成狀態。
type ScopeService struct {
	db         *sql.DB
	clock      clock.Clock
	audit      *audit.Logger
	previewKey []byte
}

// NewScopeService 建立範圍服務。
//
// previewSecret 為空字串時，每個實例以 crypto/rand 產生自己的隨機預覽金鑰：
// 沒有任何公開或可預測的預設值，其他實例（或舊版本）簽發的 token 一律無效，重啟後既有預覽也會失效。
// 明確設定 previewSecret 時，金鑰由它導出，相同設定的多個實例之間 token 互通。
func NewScopeService(db *sql.DB, clk clock.Clock, aud *audit.Logger, previewSecret string) *ScopeService {
	if clk == nil {
		clk = clock.NewRealClock()
	}
	return &ScopeService{
		db:         db,
		clock:      clk,
		audit:      aud,
		previewKey: derivePreviewKey(previewSecret),
	}
}

func derivePreviewKey(secret string) []byte {
	if secret == "" {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			// 取不到系統亂數時不得退回可預測的金鑰；直接中止啟動。
			panic("無法取得系統亂數，預覽金鑰無法安全產生: " + err.Error())
		}
		return key
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(previewKeyDomain))
	return mac.Sum(nil)
}

func (s *ScopeService) RegisterRoutes(api chi.Router) {
	api.With(auth.RequireAuth).Get("/subnets", s.handleListSubnets)
	api.With(auth.RequireAuth).Get("/subnets/undetermined", s.handleListUndetermined)
	api.With(auth.RequireAuth).Get("/subnets/{id}", s.handleGetSubnet)

	api.Group(func(r chi.Router) {
		r.Use(auth.RequireAdmin)
		r.Post("/subnets", s.handleCreateSubnet)
		r.Put("/subnets/{id}", s.handleUpdateSubnet)
		r.Post("/subnets/{id}/cidr/preview", s.HandlePreviewCIDRChange)
		r.Post("/subnets/{id}/state/preview", s.handlePreviewStateChange)
		r.Put("/subnets/{id}/state", s.handleChangeState)
		r.Post("/subnets/{id}/exclusions/preview", s.handlePreviewExclusion)
		r.Post("/subnets/{id}/exclusions", s.handleCreateExclusion)
		r.Delete("/subnets/{id}/exclusions/{exclusionId}", s.handleDeleteExclusion)
	})
}

// ApplyInterfaceAddresses 實作 pipeline.SubnetScopeProcessor，從採集到的介面位址表發現網段與閘道位址。
func (s *ScopeService) ApplyInterfaceAddresses(ctx context.Context, outputs []*interfaces.PollOutput) error {
	if s.db == nil || len(outputs) == 0 {
		return nil
	}

	now := s.clock.Now()
	seenCIDRs := make(map[string]bool)

	for _, out := range outputs {
		if out == nil {
			continue
		}
		for _, addr := range out.InterfaceAddresses {
			cidr, err := toNetworkCIDR(addr.IPAddress, addr.NetMask)
			if err != nil {
				continue
			}
			seenCIDRs[cidr] = true

			// 尋找或建立網段（新發現者預設為 pending 待檢視）
			var subnetID int64
			err = s.db.QueryRowContext(ctx, `SELECT id FROM subnets WHERE cidr = ?`, cidr).Scan(&subnetID)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				res, insErr := s.db.ExecContext(
					ctx,
					`INSERT INTO subnets (cidr, state, is_discovered, source_missing, created_at, updated_at)
					 VALUES (?, 'pending', TRUE, FALSE, ?, ?)`,
					cidr, now, now,
				)
				if insErr != nil {
					return fmt.Errorf("新增發現網段 %s 失敗: %w", cidr, insErr)
				}
				if subnetID, err = res.LastInsertId(); err != nil {
					return fmt.Errorf("取得發現網段 %s 編號失敗: %w", cidr, err)
				}
			case err != nil:
				return fmt.Errorf("查詢網段 %s 失敗: %w", cidr, err)
			default:
				if _, err := s.db.ExecContext(ctx, `UPDATE subnets SET source_missing = FALSE, updated_at = ? WHERE id = ?`, now, subnetID); err != nil {
					return fmt.Errorf("更新網段 %s 來源狀態失敗: %w", cidr, err)
				}
			}

			if subnetID > 0 {
				if _, err := s.db.ExecContext(
					ctx,
					`INSERT INTO subnet_gateways (subnet_id, ip_address, if_index, if_name, switch_id, vrf_name, created_at)
					 VALUES (?, ?, ?, ?, ?, ?, ?)
					 ON DUPLICATE KEY UPDATE if_index = VALUES(if_index), if_name = VALUES(if_name)`,
					subnetID, addr.IPAddress, addr.IfIndex, addr.IfName, out.SwitchID, out.VRFName, now,
				); err != nil {
					return fmt.Errorf("寫入閘道位址 %s 失敗: %w", addr.IPAddress, err)
				}
			}
		}
	}

	// 標記來源已消失的發現網段（僅在本次確實有成功的讀取輸出時比對）
	if len(seenCIDRs) > 0 {
		rows, err := s.db.QueryContext(ctx, `SELECT id, cidr FROM subnets WHERE is_discovered = TRUE AND source_missing = FALSE`)
		if err != nil {
			return fmt.Errorf("查詢發現網段失敗: %w", err)
		}
		var missingIDs []int64
		for rows.Next() {
			var id int64
			var cidr string
			if err := rows.Scan(&id, &cidr); err != nil {
				_ = rows.Close()
				return fmt.Errorf("讀取發現網段失敗: %w", err)
			}
			if !seenCIDRs[cidr] {
				missingIDs = append(missingIDs, id)
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("讀取發現網段失敗: %w", err)
		}
		_ = rows.Close()
		for _, id := range missingIDs {
			if _, err := s.db.ExecContext(ctx, `UPDATE subnets SET source_missing = TRUE, updated_at = ? WHERE id = ?`, now, id); err != nil {
				return fmt.Errorf("標記網段來源消失失敗: %w", err)
			}
		}
	}

	return nil
}

func toNetworkCIDR(ipStr, maskStr string) (string, error) {
	ip := net.ParseIP(ipStr).To4()
	if ip == nil {
		return "", errors.New("invalid IPv4")
	}
	maskIP := net.ParseIP(maskStr).To4()
	if maskIP == nil {
		return "", errors.New("invalid NetMask")
	}
	mask := net.IPv4Mask(maskIP[0], maskIP[1], maskIP[2], maskIP[3])
	ones, bits := mask.Size()
	if bits != 32 || (ones == 0 && maskStr != "0.0.0.0") {
		return "", errors.New("non-contiguous mask")
	}
	if ones == 32 {
		return "", errors.New("ignore /32 host loopback")
	}
	ipNet := &net.IPNet{IP: ip.Mask(mask), Mask: mask}
	return ipNet.String(), nil
}

// ---------------------------------------------------------------------------
// 錯誤表達
// ---------------------------------------------------------------------------

// scopeError 是可直接對應成 API 回應的業務錯誤。其他錯誤一律視為資料庫／系統錯誤。
type scopeError struct {
	status  int
	code    string
	message string
}

func (e *scopeError) Error() string { return e.code + ": " + e.message }

func newScopeError(status int, code, message string) *scopeError {
	return &scopeError{status: status, code: code, message: message}
}

func errSubnetNotFound() *scopeError {
	return newScopeError(http.StatusNotFound, "not_found", "找不到該網段")
}

// writeScopeError 把交易內回傳的錯誤轉成 API 回應：
// 業務錯誤照實回應；死結與鎖等待逾時回 503（呼叫端可安全重試，交易已整筆回滾）；
// 其餘一律記錄後回 500，絕不把錯誤當成「沒有資料」而繼續。
func writeScopeError(w http.ResponseWriter, r *http.Request, err error, dbMessage string) {
	var se *scopeError
	if errors.As(err, &se) {
		auth.WriteError(w, se.status, se.code, se.message)
		return
	}
	var me *mysql.MySQLError
	if errors.As(err, &me) && (me.Number == mysqlErrDeadlock || me.Number == mysqlErrLockWait) {
		slog.WarnContext(r.Context(), "網段範圍操作遇到鎖衝突，已回滾", "error", err, "path", r.URL.Path)
		auth.WriteError(w, http.StatusServiceUnavailable, "scope_busy", "範圍正被其他作業使用，請稍後重試")
		return
	}
	slog.ErrorContext(r.Context(), "網段範圍操作失敗", "error", err, "path", r.URL.Path)
	auth.WriteError(w, http.StatusInternalServerError, "db_error", dbMessage)
}

func isDuplicateEntry(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == mysqlErrDuplicateEntry
}

// ---------------------------------------------------------------------------
// 交易骨架
// ---------------------------------------------------------------------------

// runScopeTx 開啟交易並先取得範圍鎖（subnets 全部列依 id FOR UPDATE，與 Resolver.LockSnapshot 同一入口），
// 再執行 fn；fn 回傳錯誤或提交失敗時整筆回滾。
//
// 鎖次序固定為：subnets → 閘道／排除範圍 → ip_records（依 id）→ notification_states → audit_logs。
func (s *ScopeService) runScopeTx(ctx context.Context, fn func(tx *sql.Tx, scope []scopeRow) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("開啟交易失敗: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	scope, err := lockScopeRows(ctx, tx)
	if err != nil {
		return fmt.Errorf("鎖定網段範圍失敗: %w", err)
	}
	if err := fn(tx, scope); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交交易失敗: %w", err)
	}
	return nil
}

// runPreviewTx 以唯讀交易提供預覽一個一致的讀取視圖，不加任何鎖（預覽不阻擋採集與其他操作）。
// 預覽與確認會在不同的視圖上計算；任何差異都會在確認時以 token 不符（409）擋下。
func (s *ScopeService) runPreviewTx(ctx context.Context, subnetID int64, fn func(tx *sql.Tx, row scopeRow) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("開啟交易失敗: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var row scopeRow
	err = tx.QueryRowContext(ctx, `SELECT id, cidr, state FROM subnets WHERE id = ?`, subnetID).Scan(&row.id, &row.cidr, &row.state)
	if errors.Is(err, sql.ErrNoRows) {
		return errSubnetNotFound()
	}
	if err != nil {
		return fmt.Errorf("查詢網段失敗: %w", err)
	}
	return fn(tx, row)
}

func findScopeRow(scope []scopeRow, id int64) (scopeRow, bool) {
	for _, row := range scope {
		if row.id == id {
			return row, true
		}
	}
	return scopeRow{}, false
}

// ---------------------------------------------------------------------------
// 受影響的 IP 紀錄、指紋與預覽 token
// ---------------------------------------------------------------------------

// affectedRecord 是範圍變更會刪除的一筆 IP 紀錄；欄位就是 token 綁定與稽核明細所需的全部內容。
type affectedRecord struct {
	id            int64
	ip            string
	mac           sql.NullString
	ownerID       sql.NullInt64
	ownerUsername string
	ownerMail     sql.NullString
	expiresAt     sql.NullTime
}

// loadAffectedRecords 讀取網段內符合 match 的 IP 紀錄（依 id 排序）。lock 為 true 時以 FOR UPDATE 鎖住
// 該網段全部紀錄（讀取最新已提交內容，且與 purge、對帳以相同的 id 遞增次序加鎖）。
// 任何查詢、Scan、迭代錯誤，以及無法解析的 IP 都會回傳錯誤，不會被當成「沒有受影響的紀錄」。
func loadAffectedRecords(ctx context.Context, tx *sql.Tx, subnetID int64, lock bool, match func(ip4 net.IP) bool) ([]affectedRecord, error) {
	query := `SELECT id, ip_address, mac_address, owner_user_id, owner_mail, expires_at
	          FROM ip_records WHERE subnet_id = ? ORDER BY id`
	if lock {
		query += ` FOR UPDATE`
	}
	rows, err := tx.QueryContext(ctx, query, subnetID)
	if err != nil {
		return nil, fmt.Errorf("查詢 IP 紀錄失敗: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []affectedRecord
	for rows.Next() {
		var rec affectedRecord
		if err := rows.Scan(&rec.id, &rec.ip, &rec.mac, &rec.ownerID, &rec.ownerMail, &rec.expiresAt); err != nil {
			return nil, fmt.Errorf("讀取 IP 紀錄失敗: %w", err)
		}
		ip4 := net.ParseIP(rec.ip).To4()
		if ip4 == nil {
			return nil, fmt.Errorf("IP 紀錄 %d 的位址 %q 不是有效的 IPv4", rec.id, rec.ip)
		}
		if match(ip4) {
			out = append(out, rec)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("迭代 IP 紀錄失敗: %w", err)
	}
	// 同一交易的連線必須先把結果讀完並關閉，才能再下查詢。
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("關閉 IP 紀錄結果集失敗: %w", err)
	}

	if err := fillOwnerUsernames(ctx, tx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// fillOwnerUsernames 以獨立查詢補上擁有者帳號（不 JOIN、不鎖 users）。
func fillOwnerUsernames(ctx context.Context, tx *sql.Tx, recs []affectedRecord) error {
	ids := make([]int64, 0)
	seen := make(map[int64]bool)
	for _, rec := range recs {
		if rec.ownerID.Valid && !seen[rec.ownerID.Int64] {
			seen[rec.ownerID.Int64] = true
			ids = append(ids, rec.ownerID.Int64)
		}
	}
	if len(ids) == 0 {
		return nil
	}

	names := make(map[int64]string, len(ids))
	for start := 0; start < len(ids); start += scopeDeleteChunk {
		end := min(start+scopeDeleteChunk, len(ids))
		chunk := ids[start:end]
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		rows, err := tx.QueryContext(ctx,
			`SELECT id, username FROM users WHERE id IN (`+placeholders(len(chunk))+`)`, args...)
		if err != nil {
			return fmt.Errorf("查詢擁有者帳號失敗: %w", err)
		}
		for rows.Next() {
			var id int64
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				_ = rows.Close()
				return fmt.Errorf("讀取擁有者帳號失敗: %w", err)
			}
			names[id] = name
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("迭代擁有者帳號失敗: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("關閉擁有者帳號結果集失敗: %w", err)
		}
	}
	for i := range recs {
		if recs[i].ownerID.Valid {
			recs[i].ownerUsername = names[recs[i].ownerID.Int64]
		}
	}
	return nil
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func quotedNullString(ns sql.NullString) string {
	if !ns.Valid {
		return "-"
	}
	return strconv.Quote(ns.String)
}

// fingerprintRecords 對「逐筆內容」計算指紋：依 id 排序，涵蓋 ID、IP、MAC、擁有者（帳號 ID 與名稱）、
// 擁有者信箱與到期日。任何一筆被換掉、改擁有者、改信箱、改 MAC、改到期日，指紋都會不同；
// 因此 token 綁的是內容而不只是筆數。
func fingerprintRecords(recs []affectedRecord) string {
	sorted := make([]affectedRecord, len(recs))
	copy(sorted, recs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].id < sorted[j].id })

	h := sha256.New()
	for _, r := range sorted {
		owner := "-"
		if r.ownerID.Valid {
			owner = strconv.FormatInt(r.ownerID.Int64, 10)
		}
		expires := "-"
		if r.expiresAt.Valid {
			expires = r.expiresAt.Time.UTC().Format(time.RFC3339Nano)
		}
		_, _ = fmt.Fprintf(h, "%d|%s|%s|%s|%s|%s|%s\n",
			r.id, strconv.Quote(r.ip), quotedNullString(r.mac), owner,
			strconv.Quote(r.ownerUsername), quotedNullString(r.ownerMail), expires)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// 操作描述：token 同時綁定「做什麼」與「刪哪些」。操作描述含目標網段目前的 CIDR（與狀態），
// 因此預覽後網段本身被改動，token 也會失效。
func exclusionOpDesc(subnetID int64, cidr, startIP, endIP, reason string) string {
	return fmt.Sprintf("excl|%d|%s|%s|%s|%s", subnetID, cidr, startIP, endIP, reason)
}

func stateOpDesc(subnetID int64, cidr, fromState, toState string) string {
	return fmt.Sprintf("state|%d|%s|%s|%s", subnetID, cidr, fromState, toState)
}

func cidrOpDesc(subnetID int64, fromCIDR, toCIDR string) string {
	return fmt.Sprintf("cidr|%d|%s|%s", subnetID, fromCIDR, toCIDR)
}

func (s *ScopeService) signPreviewToken(opDesc, fingerprint string) string {
	mac := hmac.New(sha256.New, s.previewKey)
	mac.Write([]byte(previewTokenDomain))
	mac.Write([]byte{0})
	mac.Write([]byte(opDesc))
	mac.Write([]byte{0})
	mac.Write([]byte(fingerprint))
	return hex.EncodeToString(mac.Sum(nil))
}

// previewTokenFor 回傳該操作與受影響紀錄對應的 token；沒有受影響紀錄時不需要確認，token 為空字串。
func (s *ScopeService) previewTokenFor(opDesc string, recs []affectedRecord) string {
	if len(recs) == 0 {
		return ""
	}
	return s.signPreviewToken(opDesc, fingerprintRecords(recs))
}

// previewTokenMatches 以常數時間比對 token。受影響集合為空時只接受空 token。
func (s *ScopeService) previewTokenMatches(opDesc string, recs []affectedRecord, supplied string) bool {
	expected := s.previewTokenFor(opDesc, recs)
	if expected == "" {
		return supplied == ""
	}
	return hmac.Equal([]byte(expected), []byte(supplied))
}

func countOwned(recs []affectedRecord) int {
	n := 0
	for _, r := range recs {
		if r.ownerID.Valid {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// 刪除與稽核（皆在呼叫端的交易內）
// ---------------------------------------------------------------------------

type deletedOwnedDetail struct {
	IPAddress  string `json:"ip_address"`
	MACAddress string `json:"mac_address"`
	OwnerUser  string `json:"owner_username"`
	OwnerMail  string `json:"owner_mail"`
	ExpiresAt  any    `json:"expires_at"`
}

func ownedDetails(recs []affectedRecord) []deletedOwnedDetail {
	out := make([]deletedOwnedDetail, 0)
	for _, r := range recs {
		if !r.ownerID.Valid {
			continue
		}
		var expires any
		if r.expiresAt.Valid {
			expires = r.expiresAt.Time
		}
		out = append(out, deletedOwnedDetail{
			IPAddress:  r.ip,
			MACAddress: r.mac.String,
			OwnerUser:  r.ownerUsername,
			OwnerMail:  r.ownerMail.String,
			ExpiresAt:  expires,
		})
	}
	return out
}

// likeEscape 跳脫 LIKE 的萬用字元，確保以 IP 組成的前綴只會比對到該 IP。
func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// deleteAffectedRecords 在交易內刪除這些 IP 紀錄，以及它們的「未登記」與「到期」通知去重鍵
// （unreg|<ip>|…、expiry|<ip>|…；做法與清除排程相同）。分批執行，任何一批失敗都回傳錯誤。
func deleteAffectedRecords(ctx context.Context, tx *sql.Tx, recs []affectedRecord) error {
	for start := 0; start < len(recs); start += scopeDeleteChunk {
		end := min(start+scopeDeleteChunk, len(recs))
		chunk := recs[start:end]

		conds := make([]string, 0, len(chunk)*2)
		likeArgs := make([]any, 0, len(chunk)*2)
		idArgs := make([]any, 0, len(chunk))
		for _, r := range chunk {
			esc := likeEscape(r.ip)
			conds = append(conds, "dedup_key LIKE ?", "dedup_key LIKE ?")
			likeArgs = append(likeArgs, "unreg|"+esc+"|%", "expiry|"+esc+"|%")
			idArgs = append(idArgs, r.id)
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM notification_states WHERE `+strings.Join(conds, " OR "), likeArgs...); err != nil {
			return fmt.Errorf("刪除通知去重鍵失敗: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM ip_records WHERE id IN (`+placeholders(len(idArgs))+`)`, idArgs...); err != nil {
			return fmt.Errorf("刪除 IP 紀錄失敗: %w", err)
		}
	}
	return nil
}

// auditTx 在交易內寫入一筆稽核。稽核寫入失敗會回傳錯誤，使整筆範圍變更回滾；
// 沒有設定稽核器（單元測試）時略過。
func (s *ScopeService) auditTx(ctx context.Context, tx *sql.Tx, r *http.Request, action, targetType, targetID, summary string, details map[string]any) error {
	if s.audit == nil {
		return nil
	}
	username, role := "system", "system"
	if u := auth.UserFromContext(ctx); u != nil {
		username, role = u.Username, u.Role
	}
	if err := s.audit.LogTx(ctx, tx, username, role, r.RemoteAddr, action, targetType, targetID, summary, details); err != nil {
		return fmt.Errorf("寫入稽核失敗 (%s): %w", action, err)
	}
	return nil
}

// deleteWithAudit 刪除受影響的紀錄並寫入含已登記者明細的刪除稽核；沒有受影響紀錄時什麼都不做。
func (s *ScopeService) deleteWithAudit(
	ctx context.Context, tx *sql.Tx, r *http.Request,
	subnetID int64, recs []affectedRecord, action, scopeOperation, reasonDesc string,
) error {
	if len(recs) == 0 {
		return nil
	}
	if err := deleteAffectedRecords(ctx, tx, recs); err != nil {
		return err
	}
	owned := ownedDetails(recs)
	return s.auditTx(ctx, tx, r, action, "ip_record", "batch_delete",
		fmt.Sprintf("%s：刪除 %d 筆 IP 紀錄（其中 %d 筆已登記）", reasonDesc, len(recs), len(owned)),
		map[string]any{
			"subnet_id":             subnetID,
			"scope_operation":       scopeOperation,
			"deleted_count":         len(recs),
			"owned_deleted_count":   len(owned),
			"owned_deleted_details": owned,
		})
}

// ---------------------------------------------------------------------------
// 讀取類 API
// ---------------------------------------------------------------------------

func (s *ScopeService) handleListSubnets(w http.ResponseWriter, r *http.Request) {
	stateFilter := r.URL.Query().Get("state")

	var pendingCount int
	if err := s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM subnets WHERE state = 'pending'`).Scan(&pendingCount); err != nil {
		writeScopeError(w, r, err, "查詢網段失敗")
		return
	}

	query := `SELECT s.id, s.cidr, s.state, s.exclude_reason, COALESCE(s.exclude_comment, ''), s.is_discovered, s.source_missing, s.description,
	                 (SELECT COUNT(*) FROM ip_records r WHERE r.subnet_id = s.id) AS ip_count,
	                 (SELECT COUNT(*) FROM ip_records r WHERE r.subnet_id = s.id AND r.presence = 'online') AS online_count
	          FROM subnets s`
	var args []any
	if stateFilter != "" {
		query += ` WHERE s.state = ?`
		args = append(args, stateFilter)
	}
	query += ` ORDER BY s.id ASC`

	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeScopeError(w, r, err, "查詢網段失敗")
		return
	}
	defer func() { _ = rows.Close() }()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var cidr, state, exComment, desc string
		var exReason sql.NullString
		var isDisc, srcMissing bool
		var ipCount, onlineCount int
		if err := rows.Scan(&id, &cidr, &state, &exReason, &exComment, &isDisc, &srcMissing, &desc, &ipCount, &onlineCount); err != nil {
			writeScopeError(w, r, err, "查詢網段失敗")
			return
		}
		var er any
		if exReason.Valid {
			er = exReason.String
		}
		items = append(items, map[string]any{
			"id":              id,
			"cidr":            cidr,
			"state":           state,
			"exclude_reason":  er,
			"exclude_comment": exComment,
			"is_discovered":   isDisc,
			"source_missing":  srcMissing,
			"description":     desc,
			"ip_count":        ipCount,
			"online_count":    onlineCount,
		})
	}
	if err := rows.Err(); err != nil {
		writeScopeError(w, r, err, "查詢網段失敗")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"pending_count": pendingCount,
		"items":         items,
	})
}

// loadSubnetView 讀取單一網段的完整檢視（含閘道與排除範圍）。找不到網段回傳 errSubnetNotFound。
func (s *ScopeService) loadSubnetView(ctx context.Context, id int64) (map[string]any, error) {
	var cidr, state, exComment, desc string
	var exReason sql.NullString
	var isDisc, srcMissing bool
	err := s.db.QueryRowContext(
		ctx,
		`SELECT cidr, state, exclude_reason, COALESCE(exclude_comment, ''), is_discovered, source_missing, description FROM subnets WHERE id = ?`,
		id,
	).Scan(&cidr, &state, &exReason, &exComment, &isDisc, &srcMissing, &desc)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errSubnetNotFound()
	}
	if err != nil {
		return nil, fmt.Errorf("查詢網段失敗: %w", err)
	}

	var ipCount, onlineCount int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(CASE WHEN presence='online' THEN 1 ELSE 0 END), 0) FROM ip_records WHERE subnet_id = ?`, id,
	).Scan(&ipCount, &onlineCount); err != nil {
		return nil, fmt.Errorf("統計 IP 紀錄失敗: %w", err)
	}

	gateways, err := s.loadGatewayView(ctx, id)
	if err != nil {
		return nil, err
	}
	exclusions, err := s.loadExclusionView(ctx, id)
	if err != nil {
		return nil, err
	}

	var er any
	if exReason.Valid {
		er = exReason.String
	}
	return map[string]any{
		"id":              id,
		"cidr":            cidr,
		"state":           state,
		"exclude_reason":  er,
		"exclude_comment": exComment,
		"is_discovered":   isDisc,
		"source_missing":  srcMissing,
		"description":     desc,
		"ip_count":        ipCount,
		"online_count":    onlineCount,
		"gateways":        gateways,
		"exclusions":      exclusions,
	}, nil
}

func (s *ScopeService) loadGatewayView(ctx context.Context, id int64) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT g.ip_address, g.if_name, COALESCE(sw.name, ''), g.vrf_name
		 FROM subnet_gateways g LEFT JOIN switches sw ON g.switch_id = sw.id
		 WHERE g.subnet_id = ? ORDER BY g.ip_address ASC`,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("查詢閘道位址失敗: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]map[string]any, 0)
	for rows.Next() {
		var ip, ifn, swn, vrf string
		if err := rows.Scan(&ip, &ifn, &swn, &vrf); err != nil {
			return nil, fmt.Errorf("讀取閘道位址失敗: %w", err)
		}
		out = append(out, map[string]any{
			"ip_address":  ip,
			"if_name":     ifn,
			"switch_name": swn,
			"vrf_name":    vrf,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("迭代閘道位址失敗: %w", err)
	}
	return out, nil
}

func (s *ScopeService) loadExclusionView(ctx context.Context, id int64) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, start_ip, end_ip, reason, COALESCE(comment, '') FROM exclusion_ranges WHERE subnet_id = ? ORDER BY id ASC`,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("查詢排除範圍失敗: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]map[string]any, 0)
	for rows.Next() {
		var eid int64
		var sip, eip, reason, cmt string
		if err := rows.Scan(&eid, &sip, &eip, &reason, &cmt); err != nil {
			return nil, fmt.Errorf("讀取排除範圍失敗: %w", err)
		}
		out = append(out, map[string]any{
			"id":       eid,
			"start_ip": sip,
			"end_ip":   eip,
			"reason":   reason,
			"comment":  cmt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("迭代排除範圍失敗: %w", err)
	}
	return out, nil
}

func (s *ScopeService) handleGetSubnet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的網段 ID")
		return
	}
	s.writeSubnetView(w, r, id, http.StatusOK)
}

func (s *ScopeService) writeSubnetView(w http.ResponseWriter, r *http.Request, id int64, status int) {
	view, err := s.loadSubnetView(r.Context(), id)
	if err != nil {
		writeScopeError(w, r, err, "查詢網段失敗")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(view)
}

func (s *ScopeService) handleListUndetermined(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(
		r.Context(),
		`SELECT u.ip_address, u.mac_address, COALESCE(sw.name, ''), u.vrf_name, u.if_name, u.last_seen_at
		 FROM undetermined_prefixes u LEFT JOIN switches sw ON u.switch_id = sw.id
		 ORDER BY u.last_seen_at DESC`,
	)
	if err != nil {
		writeScopeError(w, r, err, "查詢未定前綴失敗")
		return
	}
	defer func() { _ = rows.Close() }()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var ip, mac, swName, vrf, ifn string
		var lastSeen any
		if err := rows.Scan(&ip, &mac, &swName, &vrf, &ifn, &lastSeen); err != nil {
			writeScopeError(w, r, err, "查詢未定前綴失敗")
			return
		}
		items = append(items, map[string]any{
			"ip_address":   ip,
			"mac_address":  mac,
			"switch_name":  swName,
			"vrf_name":     vrf,
			"if_name":      ifn,
			"last_seen_at": lastSeen,
		})
	}
	if err := rows.Err(); err != nil {
		writeScopeError(w, r, err, "查詢未定前綴失敗")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
}

// ---------------------------------------------------------------------------
// 新增網段
// ---------------------------------------------------------------------------

func (s *ScopeService) handleCreateSubnet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CIDR        string `json:"cidr"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CIDR == "" {
		auth.WriteError(w, http.StatusUnprocessableEntity, "validation_error", "請提供 CIDR")
		return
	}

	ip, ipNet, err := net.ParseCIDR(strings.TrimSpace(req.CIDR))
	if err != nil || ip.To4() == nil {
		auth.WriteError(w, http.StatusUnprocessableEntity, "invalid_cidr", "CIDR 格式無效，必須為 IPv4 網段")
		return
	}
	canonicalCIDR := ipNet.String()

	var id int64
	err = s.runScopeTx(r.Context(), func(tx *sql.Tx, _ []scopeRow) error {
		now := s.clock.Now()
		res, err := tx.ExecContext(
			r.Context(),
			`INSERT INTO subnets (cidr, state, is_discovered, source_missing, description, created_at, updated_at)
			 VALUES (?, 'managed', FALSE, FALSE, ?, ?, ?)`,
			canonicalCIDR, req.Description, now, now,
		)
		if isDuplicateEntry(err) {
			return newScopeError(http.StatusUnprocessableEntity, "duplicate_cidr", "該 CIDR 網段已存在")
		}
		if err != nil {
			return fmt.Errorf("新增網段失敗: %w", err)
		}
		id, err = res.LastInsertId()
		if err != nil {
			return fmt.Errorf("取得新網段 ID 失敗: %w", err)
		}
		return s.auditTx(r.Context(), tx, r, "subnet.create_manual", "subnet", strconv.FormatInt(id, 10),
			fmt.Sprintf("手動新增網段 %s", canonicalCIDR),
			map[string]any{"subnet_id": id, "cidr": canonicalCIDR})
	})
	if err != nil {
		writeScopeError(w, r, err, "新增網段失敗")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "cidr": canonicalCIDR, "state": "managed"})
}

// ---------------------------------------------------------------------------
// 修正 CIDR
// ---------------------------------------------------------------------------

func parseSubnetID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的網段 ID")
		return 0, false
	}
	return id, true
}

// parseTargetCIDR 解析並正規化目標 CIDR，回傳（網段、正規化字串）。
func parseTargetCIDR(raw string) (*net.IPNet, string, error) {
	_, ipNet, err := net.ParseCIDR(strings.TrimSpace(raw))
	if err != nil || ipNet.IP.To4() == nil {
		return nil, "", errors.New("CIDR 格式無效")
	}
	return ipNet, ipNet.String(), nil
}

func outsideCIDR(newNet *net.IPNet) func(net.IP) bool {
	return func(ip4 net.IP) bool { return !newNet.Contains(ip4) }
}

// recordMove 是修正 CIDR 後改歸屬其他納管網段、因此保留不刪的 IP 紀錄。
type recordMove struct {
	recordID int64
	subnetID int64
}

// splitCIDROrphans 把「修正 CIDR 後不在新範圍內」的紀錄分成兩類（開發規格 3.3）：
// 仍落在其他納管網段、且不在排除範圍、不是閘道位址者改歸屬該網段；其餘才刪除。
// 判定使用「套用新 CIDR 後」的範圍快照；shareLock 為 true 時以共享鎖讀取閘道與排除範圍。
func splitCIDROrphans(ctx context.Context, q scopeQuerier, scope []scopeRow, subnetID int64, canonical string, shareLock bool, orphans []affectedRecord) ([]affectedRecord, []recordMove, error) {
	if len(orphans) == 0 {
		return nil, nil, nil
	}
	rows := make([]scopeRow, len(scope))
	copy(rows, scope)
	for i := range rows {
		if rows[i].id == subnetID {
			rows[i].cidr = canonical
		}
	}
	snap, err := loadSnapshot(ctx, q, rows, shareLock)
	if err != nil {
		return nil, nil, err
	}
	var deletes []affectedRecord
	var moves []recordMove
	for _, rec := range orphans {
		res, err := snap.ResolveIP(rec.ip)
		if err != nil {
			return nil, nil, err
		}
		if res.ShouldHaveRecord && res.SubnetID != subnetID {
			moves = append(moves, recordMove{recordID: rec.id, subnetID: res.SubnetID})
			continue
		}
		deletes = append(deletes, rec)
	}
	return deletes, moves, nil
}

// HandlePreviewCIDRChange 預覽「把網段 CIDR 修正為 X」會刪除哪些 IP 紀錄，回傳與其他預覽相同形狀的結果。
// 路徑：POST /subnets/{id}/cidr/preview，與確認用 PUT /subnets/{id} 共用相同刪除指紋。
func (s *ScopeService) HandlePreviewCIDRChange(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSubnetID(w, r)
	if !ok {
		return
	}
	var req struct {
		CIDR string `json:"cidr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求格式")
		return
	}
	newNet, canonical, err := parseTargetCIDR(req.CIDR)
	if err != nil {
		auth.WriteError(w, http.StatusUnprocessableEntity, "invalid_cidr", err.Error())
		return
	}

	var recs []affectedRecord
	var token string
	err = s.runPreviewTx(r.Context(), id, func(tx *sql.Tx, row scopeRow) error {
		orphans, err := loadAffectedRecords(r.Context(), tx, id, false, outsideCIDR(newNet))
		if err != nil {
			return err
		}
		scope, err := queryScopeRows(r.Context(), tx, plainScopeRowsSQL)
		if err != nil {
			return fmt.Errorf("查詢網段範圍失敗: %w", err)
		}
		recs, _, err = splitCIDROrphans(r.Context(), tx, scope, id, canonical, false, orphans)
		if err != nil {
			return err
		}
		token = s.previewTokenFor(cidrOpDesc(id, row.cidr, canonical), recs)
		return nil
	})
	if err != nil {
		writeScopeError(w, r, err, "預覽修正 CIDR 失敗")
		return
	}
	writePreview(w, token, recs)
}

func writePreview(w http.ResponseWriter, token string, recs []affectedRecord) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"preview_token":    token,
		"total_ip_records": len(recs),
		"owned_ip_records": countOwned(recs),
		"requires_confirm": len(recs) > 0,
	})
}

func (s *ScopeService) handleUpdateSubnet(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSubnetID(w, r)
	if !ok {
		return
	}

	var req struct {
		CIDR         string `json:"cidr"`
		Description  string `json:"description"`
		PreviewToken string `json:"preview_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求格式")
		return
	}

	newNet, canonical, err := parseTargetCIDR(req.CIDR)
	if err != nil {
		auth.WriteError(w, http.StatusUnprocessableEntity, "invalid_cidr", err.Error())
		return
	}

	err = s.runScopeTx(r.Context(), func(tx *sql.Tx, scope []scopeRow) error {
		row, ok := findScopeRow(scope, id)
		if !ok {
			return errSubnetNotFound()
		}

		// 在鎖內重新計算：修正後不再屬於此網段的既有 IP 紀錄；仍屬其他納管網段者改歸屬，其餘才刪除。
		orphans, err := loadAffectedRecords(r.Context(), tx, id, true, outsideCIDR(newNet))
		if err != nil {
			return err
		}
		recs, moves, err := splitCIDROrphans(r.Context(), tx, scope, id, canonical, true, orphans)
		if err != nil {
			return err
		}
		if len(recs) > 0 {
			if !s.previewTokenMatches(cidrOpDesc(id, row.cidr, canonical), recs, req.PreviewToken) {
				return newScopeError(http.StatusConflict, "confirmation_required",
					fmt.Sprintf("修正 CIDR 將刪除 %d 筆 IP 紀錄（其中 %d 筆已登記），請先預覽並確認，或資料已異動請重新預覽", len(recs), countOwned(recs)))
			}
			if err := s.deleteWithAudit(r.Context(), tx, r, id, recs,
				"subnet.cidr_change_delete", "cidr_change", fmt.Sprintf("修正網段 %d CIDR 為 %s", id, canonical)); err != nil {
				return err
			}
		}

		for _, mv := range moves {
			if _, err := tx.ExecContext(r.Context(), `UPDATE ip_records SET subnet_id = ?, updated_at = ? WHERE id = ?`,
				mv.subnetID, s.clock.Now(), mv.recordID); err != nil {
				return fmt.Errorf("調整 IP 紀錄歸屬失敗: %w", err)
			}
		}

		if _, err := tx.ExecContext(r.Context(), `UPDATE subnets SET cidr = ?, description = ?, updated_at = ? WHERE id = ?`,
			canonical, req.Description, s.clock.Now(), id); err != nil {
			if isDuplicateEntry(err) {
				return newScopeError(http.StatusUnprocessableEntity, "duplicate_cidr", "CIDR 更新失敗或與現有網段衝突")
			}
			return fmt.Errorf("更新網段失敗: %w", err)
		}

		return s.auditTx(r.Context(), tx, r, "subnet.update", "subnet", strconv.FormatInt(id, 10),
			fmt.Sprintf("更新網段 %d：CIDR %s → %s", id, row.cidr, canonical),
			map[string]any{"subnet_id": id, "old_cidr": row.cidr, "cidr": canonical, "deleted_count": len(recs), "moved_count": len(moves)})
	})
	if err != nil {
		writeScopeError(w, r, err, "更新網段失敗")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "cidr": canonical, "description": req.Description})
}

// ---------------------------------------------------------------------------
// 網段狀態
// ---------------------------------------------------------------------------

func allRecords(net.IP) bool { return true }

func (s *ScopeService) handlePreviewStateChange(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSubnetID(w, r)
	if !ok {
		return
	}

	var req struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求格式")
		return
	}

	var recs []affectedRecord
	var token string
	err := s.runPreviewTx(r.Context(), id, func(tx *sql.Tx, row scopeRow) error {
		if req.State != "unmanaged" {
			return nil // 只有改為不納管才會刪除 IP 紀錄
		}
		var err error
		recs, err = loadAffectedRecords(r.Context(), tx, id, false, allRecords)
		if err != nil {
			return err
		}
		token = s.previewTokenFor(stateOpDesc(id, row.cidr, row.state, req.State), recs)
		return nil
	})
	if err != nil {
		writeScopeError(w, r, err, "預覽網段狀態變更失敗")
		return
	}
	writePreview(w, token, recs)
}

func (s *ScopeService) handleChangeState(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSubnetID(w, r)
	if !ok {
		return
	}

	var req struct {
		State          string `json:"state"`
		ExcludeReason  string `json:"exclude_reason"`
		ExcludeComment string `json:"exclude_comment"`
		PreviewToken   string `json:"preview_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求格式")
		return
	}

	if req.State != "managed" && req.State != "unmanaged" {
		auth.WriteError(w, http.StatusUnprocessableEntity, "invalid_state", "網段僅可設為納管 (managed) 或不納管 (unmanaged)，不可退回待檢視")
		return
	}
	if req.State == "unmanaged" && req.ExcludeReason != "dhcp" && req.ExcludeReason != "wireless" && req.ExcludeReason != "other" {
		auth.WriteError(w, http.StatusUnprocessableEntity, "reason_required", "設為不納管時必須選擇原因（dhcp、wireless 或 other）")
		return
	}

	err := s.runScopeTx(r.Context(), func(tx *sql.Tx, scope []scopeRow) error {
		row, ok := findScopeRow(scope, id)
		if !ok {
			return errSubnetNotFound()
		}

		if req.State == "unmanaged" {
			recs, err := loadAffectedRecords(r.Context(), tx, id, true, allRecords)
			if err != nil {
				return err
			}
			if len(recs) > 0 {
				if !s.previewTokenMatches(stateOpDesc(id, row.cidr, row.state, req.State), recs, req.PreviewToken) {
					return newScopeError(http.StatusConflict, "preview_mismatch", "資料已異動或尚未通過刪除預覽確認，請重新預覽")
				}
				if err := s.deleteWithAudit(r.Context(), tx, r, id, recs,
					"subnet.unmanage_delete", "unmanage", fmt.Sprintf("網段 %d 改為不納管 (%s)", id, req.ExcludeReason)); err != nil {
					return err
				}
			}
			if _, err := tx.ExecContext(
				r.Context(),
				`UPDATE subnets SET state = 'unmanaged', exclude_reason = ?, exclude_comment = ?, updated_at = ? WHERE id = ?`,
				req.ExcludeReason, req.ExcludeComment, s.clock.Now(), id,
			); err != nil {
				return fmt.Errorf("更新網段狀態失敗: %w", err)
			}
		} else {
			if _, err := tx.ExecContext(
				r.Context(),
				`UPDATE subnets SET state = 'managed', exclude_reason = NULL, exclude_comment = '', updated_at = ? WHERE id = ?`,
				s.clock.Now(), id,
			); err != nil {
				return fmt.Errorf("更新網段狀態失敗: %w", err)
			}
		}

		return s.auditTx(r.Context(), tx, r, "subnet.state_change", "subnet", strconv.FormatInt(id, 10),
			fmt.Sprintf("變更網段狀態為 %s", req.State),
			map[string]any{"subnet_id": id, "state": req.State, "exclude_reason": req.ExcludeReason})
	})
	if err != nil {
		writeScopeError(w, r, err, "更新網段狀態失敗")
		return
	}

	s.writeSubnetView(w, r, id, http.StatusOK)
}

// ---------------------------------------------------------------------------
// 排除範圍
// ---------------------------------------------------------------------------

// exclusionRange 是通過語法檢查、已正規化的排除範圍。
type exclusionRange struct {
	startIP, endIP     string
	startUint, endUint uint32
	reason             string
}

// parseExclusionRange 檢查不需要資料庫的部分：原因、IPv4 格式與起訖順序。位址以點分十進位正規化。
func parseExclusionRange(startStr, endStr, reason string) (exclusionRange, error) {
	if reason != "dhcp" && reason != "wireless" && reason != "other" {
		return exclusionRange{}, errors.New("必須選擇排除原因（dhcp、wireless 或 other）")
	}
	sIP := net.ParseIP(strings.TrimSpace(startStr)).To4()
	eIP := net.ParseIP(strings.TrimSpace(endStr)).To4()
	if sIP == nil || eIP == nil {
		return exclusionRange{}, errors.New("起始或結束 IP 必須為有效的 IPv4 位址")
	}
	rng := exclusionRange{
		startIP: sIP.String(), endIP: eIP.String(),
		startUint: binary.BigEndian.Uint32(sIP), endUint: binary.BigEndian.Uint32(eIP),
		reason: reason,
	}
	if rng.startUint > rng.endUint {
		return exclusionRange{}, errors.New("起始 IP 不可大於結束 IP")
	}
	return rng, nil
}

// checkRangeInCIDR 檢查起訖位址完全落在網段 CIDR 內；網段 CIDR 來自資料庫，無法解析時視為系統錯誤。
func checkRangeInCIDR(cidr string, rng exclusionRange) error {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("網段 CIDR %q 無法解析: %w", cidr, err)
	}
	if !ipNet.Contains(net.ParseIP(rng.startIP)) || !ipNet.Contains(net.ParseIP(rng.endIP)) {
		return newScopeError(http.StatusUnprocessableEntity, "validation_error", "排除範圍的起訖位址必須完全落在網段 CIDR 內")
	}
	return nil
}

func (rng exclusionRange) matcher() func(net.IP) bool {
	return func(ip4 net.IP) bool {
		v := binary.BigEndian.Uint32(ip4)
		return v >= rng.startUint && v <= rng.endUint
	}
}

func (s *ScopeService) handlePreviewExclusion(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSubnetID(w, r)
	if !ok {
		return
	}

	var req struct {
		StartIP string `json:"start_ip"`
		EndIP   string `json:"end_ip"`
		Reason  string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求格式")
		return
	}
	rng, err := parseExclusionRange(req.StartIP, req.EndIP, req.Reason)
	if err != nil {
		auth.WriteError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}

	var recs []affectedRecord
	var token string
	err = s.runPreviewTx(r.Context(), id, func(tx *sql.Tx, row scopeRow) error {
		if err := checkRangeInCIDR(row.cidr, rng); err != nil {
			return err
		}
		var err error
		recs, err = loadAffectedRecords(r.Context(), tx, id, false, rng.matcher())
		if err != nil {
			return err
		}
		token = s.previewTokenFor(exclusionOpDesc(id, row.cidr, rng.startIP, rng.endIP, rng.reason), recs)
		return nil
	})
	if err != nil {
		writeScopeError(w, r, err, "預覽排除範圍失敗")
		return
	}
	writePreview(w, token, recs)
}

func (s *ScopeService) handleCreateExclusion(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSubnetID(w, r)
	if !ok {
		return
	}

	var req struct {
		StartIP      string `json:"start_ip"`
		EndIP        string `json:"end_ip"`
		Reason       string `json:"reason"`
		Comment      string `json:"comment"`
		PreviewToken string `json:"preview_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的請求格式")
		return
	}
	rng, err := parseExclusionRange(req.StartIP, req.EndIP, req.Reason)
	if err != nil {
		auth.WriteError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}

	var exID int64
	err = s.runScopeTx(r.Context(), func(tx *sql.Tx, scope []scopeRow) error {
		row, ok := findScopeRow(scope, id)
		if !ok {
			return errSubnetNotFound()
		}
		if err := checkRangeInCIDR(row.cidr, rng); err != nil {
			return err
		}

		recs, err := loadAffectedRecords(r.Context(), tx, id, true, rng.matcher())
		if err != nil {
			return err
		}
		if len(recs) > 0 {
			if !s.previewTokenMatches(exclusionOpDesc(id, row.cidr, rng.startIP, rng.endIP, rng.reason), recs, req.PreviewToken) {
				return newScopeError(http.StatusConflict, "preview_mismatch", "資料已異動或尚未通過刪除預覽確認，請重新預覽")
			}
			if err := s.deleteWithAudit(r.Context(), tx, r, id, recs,
				"exclusion.create_delete", "exclusion_create",
				fmt.Sprintf("新增排除範圍 %s-%s (%s)", rng.startIP, rng.endIP, rng.reason)); err != nil {
				return err
			}
		}

		now := s.clock.Now()
		res, err := tx.ExecContext(
			r.Context(),
			`INSERT INTO exclusion_ranges (subnet_id, start_ip, end_ip, reason, comment, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, rng.startIP, rng.endIP, rng.reason, req.Comment, now, now,
		)
		if err != nil {
			return fmt.Errorf("建立排除範圍失敗: %w", err)
		}
		exID, err = res.LastInsertId()
		if err != nil {
			return fmt.Errorf("取得排除範圍 ID 失敗: %w", err)
		}

		return s.auditTx(r.Context(), tx, r, "exclusion.create", "subnet", strconv.FormatInt(id, 10),
			fmt.Sprintf("新增排除範圍 %s - %s (%s)", rng.startIP, rng.endIP, rng.reason),
			map[string]any{"subnet_id": id, "exclusion_id": exID, "start_ip": rng.startIP, "end_ip": rng.endIP, "reason": rng.reason, "deleted_count": len(recs)})
	})
	if err != nil {
		writeScopeError(w, r, err, "建立排除範圍失敗")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"id": exID, "start_ip": rng.startIP, "end_ip": rng.endIP, "reason": rng.reason})
}

func (s *ScopeService) handleDeleteExclusion(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSubnetID(w, r)
	if !ok {
		return
	}
	exID, err := strconv.ParseInt(chi.URLParam(r, "exclusionId"), 10, 64)
	if err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無效的 ID")
		return
	}

	err = s.runScopeTx(r.Context(), func(tx *sql.Tx, scope []scopeRow) error {
		if _, ok := findScopeRow(scope, id); !ok {
			return errSubnetNotFound()
		}
		res, err := tx.ExecContext(r.Context(), `DELETE FROM exclusion_ranges WHERE id = ? AND subnet_id = ?`, exID, id)
		if err != nil {
			return fmt.Errorf("刪除排除範圍失敗: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("確認刪除筆數失敗: %w", err)
		}
		if affected == 0 {
			return newScopeError(http.StatusNotFound, "not_found", "找不到該排除範圍")
		}
		return s.auditTx(r.Context(), tx, r, "exclusion.delete", "subnet", strconv.FormatInt(id, 10),
			fmt.Sprintf("移除排除範圍 #%d", exID),
			map[string]any{"subnet_id": id, "exclusion_id": exID})
	})
	if err != nil {
		writeScopeError(w, r, err, "刪除排除範圍失敗")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
