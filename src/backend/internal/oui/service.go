package oui

import (
	"bufio"
	"context"
	"database/sql"
	_ "embed"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jeffhou369/wurzburg/backend/internal/audit"
	"github.com/jeffhou369/wurzburg/backend/internal/auth"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
)

//go:embed oui-snapshot.csv
var embeddedSnapshot string

type Entry struct {
	Prefix       string
	PrefixLength int
	VendorName   string
}

// Service 實作本地 OUI 廠商查詢與匯入更新，嚴格保持離線零對外連線。
type Service struct {
	db    *sql.DB
	clock clock.Clock
	audit *audit.Logger

	mu    sync.RWMutex
	byLen map[int]map[string]string // 36 -> prefix9 -> vendor, 28 -> prefix7 -> vendor, 24 -> prefix6 -> vendor
}

func NewService(db *sql.DB, clk clock.Clock, aud *audit.Logger) *Service {
	if clk == nil {
		clk = clock.NewRealClock()
	}
	return &Service{
		db:    db,
		clock: clk,
		audit: aud,
		byLen: map[int]map[string]string{
			36: make(map[string]string),
			28: make(map[string]string),
			24: make(map[string]string),
		},
	}
}

// EnsureLoaded 若資料庫尚未匯入任何 OUI 資料，自動載入隨產品出貨的快照並建立記憶體索引。
func (s *Service) EnsureLoaded(ctx context.Context) error {
	if s.db == nil {
		entries, _ := ParseOUI(strings.NewReader(embeddedSnapshot))
		s.replaceCache(entries)
		return nil
	}

	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM oui_imports`).Scan(&count); err != nil {
		return fmt.Errorf("檢查 OUI 匯入紀錄失敗: %w", err)
	}

	if count == 0 {
		entries, err := ParseOUI(strings.NewReader(embeddedSnapshot))
		if err != nil {
			return fmt.Errorf("解析內建 OUI 快照失敗: %w", err)
		}
		if err := s.saveEntries(ctx, "2026-11-11-builtin", "oui-snapshot.csv", "system", entries); err != nil {
			return err
		}
		s.replaceCache(entries)
		return nil
	}

	return s.reloadFromDB(ctx)
}

func (s *Service) reloadFromDB(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT prefix, prefix_length, vendor_name FROM oui_entries`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.Prefix, &e.PrefixLength, &e.VendorName); err == nil {
			entries = append(entries, e)
		}
	}
	s.replaceCache(entries)
	return nil
}

func (s *Service) replaceCache(entries []Entry) {
	newMap := map[int]map[string]string{
		36: make(map[string]string),
		28: make(map[string]string),
		24: make(map[string]string),
	}
	for _, e := range entries {
		if m, ok := newMap[e.PrefixLength]; ok {
			m[strings.ToUpper(e.Prefix)] = e.VendorName
		}
	}
	s.mu.Lock()
	s.byLen = newMap
	s.mu.Unlock()
}

// Lookup 查詢 MAC 位址對應的廠商名稱，依 36 -> 28 -> 24 位元長前綴優先比對，查不到一律回傳「未知」。
func (s *Service) Lookup(mac string) string {
	clean := NormalizeHex(mac)
	if len(clean) < 6 {
		return "未知"
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(clean) >= 9 {
		if v, ok := s.byLen[36][clean[:9]]; ok {
			return v
		}
	}
	if len(clean) >= 7 {
		if v, ok := s.byLen[28][clean[:7]]; ok {
			return v
		}
	}
	if v, ok := s.byLen[24][clean[:6]]; ok {
		return v
	}
	return "未知"
}

// NormalizeHex 移除冒號、連字號與點，轉為大寫十六進位。
func NormalizeHex(input string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(input) {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'F') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ParseOUI 解析專案 CSV 格式 (prefix,bitlen,vendor) 或簡化雙欄格式 (prefix,vendor)。
func ParseOUI(r io.Reader) ([]Entry, error) {
	scanner := bufio.NewScanner(r)
	var entries []Entry
	lineNo := 0

	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		cr := csv.NewReader(strings.NewReader(line))
		cr.FieldsPerRecord = -1
		cols, err := cr.Read()
		if err != nil {
			return nil, fmt.Errorf("第 %d 行 CSV 格式錯誤: %w", lineNo, err)
		}

		if len(cols) < 2 {
			return nil, fmt.Errorf("第 %d 行欄位數不足", lineNo)
		}

		// 跳過標題列
		if lineNo <= 5 && strings.EqualFold(strings.TrimSpace(cols[0]), "prefix") {
			continue
		}

		prefixHex := NormalizeHex(cols[0])
		if len(prefixHex) != 6 && len(prefixHex) != 7 && len(prefixHex) != 9 {
			return nil, fmt.Errorf("第 %d 行前綴長度無效 (%s)", lineNo, cols[0])
		}

		var bitLen int
		var vendor string
		if len(cols) >= 3 {
			bitLen, err = strconv.Atoi(strings.TrimSpace(cols[1]))
			if err != nil || (bitLen != 24 && bitLen != 28 && bitLen != 36) {
				return nil, fmt.Errorf("第 %d 行位元長度必須為 24、28 或 36", lineNo)
			}
			vendor = strings.TrimSpace(strings.Join(cols[2:], ","))
		} else {
			bitLen = len(prefixHex) * 4
			vendor = strings.TrimSpace(cols[1])
		}

		if vendor == "" {
			return nil, fmt.Errorf("第 %d 行廠商名稱不可為空", lineNo)
		}

		entries = append(entries, Entry{
			Prefix:       prefixHex,
			PrefixLength: bitLen,
			VendorName:   vendor,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, errors.New("OUI 資料檔不含任何有效項目")
	}

	return entries, nil
}

func (s *Service) saveEntries(ctx context.Context, version, filename, importedBy string, entries []Entry) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM oui_entries`); err != nil {
		return err
	}

	now := s.clock.Now()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO oui_entries (prefix, prefix_length, vendor_name, created_at) VALUES (?, ?, ?, ?) ON DUPLICATE KEY UPDATE vendor_name = VALUES(vendor_name)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range entries {
		if _, err := stmt.ExecContext(ctx, e.Prefix, e.PrefixLength, e.VendorName, now); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO oui_imports (version, filename, imported_by, entry_count, imported_at) VALUES (?, ?, ?, ?, ?)`,
		version, filename, importedBy, len(entries), now,
	); err != nil {
		return err
	}

	return tx.Commit()
}

// RecomputeExistingIPVendors 重新計算既有 IP 紀錄的廠商名稱快照。
func (s *Service) RecomputeExistingIPVendors(ctx context.Context) error {
	if s.db == nil {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, mac_address FROM ip_records WHERE mac_address != ''`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type rowUpdate struct {
		id     int64
		vendor string
	}
	var updates []rowUpdate
	for rows.Next() {
		var id int64
		var mac string
		if err := rows.Scan(&id, &mac); err == nil {
			updates = append(updates, rowUpdate{id: id, vendor: s.Lookup(mac)})
		}
	}

	for _, u := range updates {
		_, _ = s.db.ExecContext(ctx, `UPDATE ip_records SET vendor_name = ? WHERE id = ?`, u.vendor, u.id)
	}
	return nil
}

func (s *Service) RegisterRoutes(api chi.Router) {
	api.With(auth.RequireAdmin).Get("/settings/oui", s.handleGetStatus)
	api.With(auth.RequireAdmin).Post("/settings/oui", s.handleUpload)
}

func (s *Service) handleGetStatus(w http.ResponseWriter, r *http.Request) {
	var version, filename, importedBy string
	var count int
	var importedAt time.Time

	err := s.db.QueryRowContext(
		r.Context(),
		`SELECT version, filename, imported_by, entry_count, imported_at FROM oui_imports ORDER BY id DESC LIMIT 1`,
	).Scan(&version, &filename, &importedBy, &count, &importedAt)

	if errors.Is(err, sql.ErrNoRows) {
		auth.WriteError(w, http.StatusNotFound, "not_found", "尚未載入 OUI 資料")
		return
	}
	if err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "查詢 OUI 狀態失敗")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"version":     version,
		"filename":    filename,
		"imported_by": importedBy,
		"entry_count": count,
		"imported_at": importedAt,
	})
}

func (s *Service) handleUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		auth.WriteError(w, http.StatusBadRequest, "bad_request", "無法讀取上傳表單")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		auth.WriteError(w, http.StatusUnprocessableEntity, "file_missing", "請選擇要上傳的 OUI 資料檔")
		return
	}
	defer file.Close()

	entries, err := ParseOUI(file)
	if err != nil {
		auth.WriteError(w, http.StatusUnprocessableEntity, "invalid_oui_format", err.Error())
		return
	}

	u := auth.UserFromContext(r.Context())
	version := s.clock.Now().Format("2006-01-02T15:04:05Z")
	if err := s.saveEntries(r.Context(), version, header.Filename, u.Username, entries); err != nil {
		auth.WriteError(w, http.StatusInternalServerError, "db_error", "儲存 OUI 資料失敗")
		return
	}

	s.replaceCache(entries)
	_ = s.RecomputeExistingIPVendors(r.Context())

	if s.audit != nil {
		s.audit.Log(r.Context(), u.Username, u.Role, r.RemoteAddr, "oui.import", "oui", version,
			fmt.Sprintf("匯入本地 OUI 資料檔 %s（共 %d 筆）", header.Filename, len(entries)), nil)
	}

	s.handleGetStatus(w, r)
}
