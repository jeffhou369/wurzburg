package subnets

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-sql-driver/mysql"

	"github.com/jeffhou369/wurzburg/backend/internal/audit"
	"github.com/jeffhou369/wurzburg/backend/internal/auth"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
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

// 網段狀態（subnets.state）。SQL 字串內仍直接使用同樣的值。
const (
	StatePending   = "pending"   // 待檢視
	StateManaged   = "managed"   // 納管
	StateUnmanaged = "unmanaged" // 不納管（必須附原因）
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
