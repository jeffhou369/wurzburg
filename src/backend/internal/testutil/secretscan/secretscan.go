// Package secretscan 提供測試用的 Secret 洩漏掃描。
//
// 規則：Secret 儲存後不回顯，不出現在日誌、稽核紀錄、錯誤頁、URL 與郵件本文。
// 掃描器記住測試中用過的每個 Secret，再檢查系統對外輸出的每個管道：
// HTTP 回應、請求 URL、寄出的信、應用程式日誌，以及資料庫的所有文字欄位
// （Secret 依規格應加密或雜湊後儲存，明文出現在任何資料表都算洩漏）。
//
// 回報洩漏時只給管道、位置與遮蔽後的片段，不印出 Secret 的值。本套件只供測試匯入。
package secretscan

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/jeffhou369/wurzburg/backend/internal/testutil/httpcapture"
)

// MinSecretLength 是可登錄的 Secret 最短長度。太短的字串（例如 public）會出現在一般文字中，
// 掃描結果沒有意義；測試要改用可辨識的值。
const MinSecretLength = 8

const masked = "«已遮蔽»"

// 各掃描管道的名稱。
const (
	ChannelResponse = "HTTP 回應"
	ChannelURL      = "請求 URL"
	ChannelMail     = "寄出的信"
	ChannelLog      = "應用程式日誌"
	ChannelDatabase = "資料庫"
)

// secretFieldNames 是請求本文中屬於 Secret 的欄位名稱：
// docs/api 中所有 format: password 的欄位，加上交換器 API 相容的別名 community。
var secretFieldNames = map[string]bool{
	"password":         true,
	"new_password":     true,
	"old_password":     true,
	"snmp_community":   true,
	"community":        true,
	"v3_auth_pass":     true,
	"v3_priv_pass":     true,
	"smtp_password":    true,
	"ad_bind_password": true,
	"radius_secret":    true,
}

// SecretFieldNames 回傳請求本文中視為 Secret 的欄位名稱，已排序。
func SecretFieldNames() []string {
	out := make([]string, 0, len(secretFieldNames))
	for name := range secretFieldNames {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// TB 是 testing.TB 中本套件用到的部分。
type TB interface {
	Helper()
	Errorf(format string, args ...any)
}

// Finding 是一次洩漏：哪個管道、哪個位置、哪個 Secret（只有標籤，沒有值）。
type Finding struct {
	Channel  string
	Location string
	Secret   string // 例如「snmp_community（長度 20）」
	Excerpt  string // 洩漏處前後的片段，Secret 已遮蔽
}

func (f Finding) String() string {
	return fmt.Sprintf("%s｜%s｜洩漏的 Secret：%s｜片段：%s", f.Channel, f.Location, f.Secret, f.Excerpt)
}

type secret struct {
	label    string
	variants []string // 明文與常見的編碼形式
}

type retainedText struct {
	channel, location, text string
}

// Scanner 記住登錄過的 Secret 與掃描到的洩漏。可由多個測試並行使用。
type Scanner struct {
	mu       sync.Mutex
	secrets  map[string]*secret // 以明文為鍵
	findings []Finding
	seen     map[string]bool
	retained []retainedText
}

// New 建立空的掃描器。
func New() *Scanner {
	return &Scanner{secrets: map[string]*secret{}, seen: map[string]bool{}}
}

var shared = New()

// Shared 回傳整個測試行程共用的掃描器。
func Shared() *Scanner { return shared }

// Register 登錄一個 Secret。label 說明它是什麼（例如欄位名稱），會出現在洩漏報告中。
// 空字串不登錄；長度不足 MinSecretLength 時回傳錯誤。
func (s *Scanner) Register(label, value string) error {
	if value == "" {
		return nil
	}
	if len(value) < MinSecretLength {
		return fmt.Errorf("測試用的 Secret 樣本太短（%s，長度 %d）：至少要 %d 個字元才能可靠地辨識，請改用可辨識的值", label, len(value), MinSecretLength)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.secrets[value]; ok {
		return nil
	}
	s.secrets[value] = &secret{
		label:    fmt.Sprintf("%s（長度 %d）", label, len(value)),
		variants: variantsOf(value),
	}
	return nil
}

// variantsOf 列出 Secret 可能出現的形式：明文，以及 URL、HTML、JSON 跳脫後的樣子。
func variantsOf(value string) []string {
	jsonEscaped := strconv.Quote(value)
	jsonEscaped = jsonEscaped[1 : len(jsonEscaped)-1]
	htmlSafeJSON, _ := json.Marshal(value) // encoding/json 會把 <、>、& 轉成 < 等形式
	candidates := []string{
		value,
		url.QueryEscape(value),
		url.PathEscape(value),
		html.EscapeString(value),
		jsonEscaped,
		string(htmlSafeJSON[1 : len(htmlSafeJSON)-1]),
	}
	seen := map[string]bool{}
	var out []string
	for _, c := range candidates {
		if c != "" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// Scan 以目前登錄的 Secret 掃描一段文字，回傳並記錄發現的洩漏。
func (s *Scanner) Scan(channel, location, text string) []Finding {
	if text == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scanLocked(channel, location, text)
}

func (s *Scanner) scanLocked(channel, location, text string) []Finding {
	var found []Finding
	for _, sec := range s.secrets {
		for _, variant := range sec.variants {
			idx := strings.Index(text, variant)
			if idx < 0 {
				continue
			}
			f := Finding{Channel: channel, Location: location, Secret: sec.label, Excerpt: excerptAround(text, idx, variant)}
			key := channel + "\x00" + location + "\x00" + sec.label
			if !s.seen[key] {
				s.seen[key] = true
				s.findings = append(s.findings, f)
			}
			found = append(found, f)
			break
		}
	}
	return found
}

// Retain 掃描一段文字並保留它，讓 Rescan 能用之後才登錄的 Secret 再掃一次。
// 用於日誌與信件：Secret 可能在這段文字產生之後才第一次經由 API 送出而被登錄。
func (s *Scanner) Retain(channel, location, text string) []Finding {
	if text == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retained = append(s.retained, retainedText{channel, location, text})
	return s.scanLocked(channel, location, text)
}

// Rescan 以目前登錄的全部 Secret 重新掃描保留的文字。整套測試結束時呼叫。
func (s *Scanner) Rescan() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.retained {
		s.scanLocked(r.channel, r.location, r.text)
	}
}

// Findings 回傳到目前為止發現的所有洩漏。
func (s *Scanner) Findings() []Finding {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Finding, len(s.findings))
	copy(out, s.findings)
	return out
}

// excerptAround 取出洩漏處前後各約 40 個位元組，並把 Secret 遮蔽。
func excerptAround(text string, idx int, variant string) string {
	const context = 40
	start := idx - context
	if start < 0 {
		start = 0
	}
	end := idx + len(variant) + context
	if end > len(text) {
		end = len(text)
	}
	out := text[start:idx] + masked + text[idx+len(variant):end]
	out = strings.ToValidUTF8(out, "")
	return strings.Join(strings.Fields(out), " ")
}

// ExtractSecrets 從 JSON 請求本文取出 Secret 欄位的值，鍵為欄位名稱。不是 JSON 時回傳空結果。
func ExtractSecrets(body []byte) map[string][]string {
	out := map[string][]string{}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return out
	}
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			for key, child := range t {
				if str, ok := child.(string); ok && secretFieldNames[key] {
					out[key] = append(out[key], str)
					continue
				}
				walk(child)
			}
		case []any:
			for _, child := range t {
				walk(child)
			}
		}
	}
	walk(value)
	return out
}

// Wrap 包住路由器：登錄請求本文中的 Secret 欄位，照常處理請求，再掃描請求 URL 與回應。
// 發現洩漏或 Secret 樣本太短時以 t.Errorf 回報。
func (s *Scanner) Wrap(t TB, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		where := r.Method + " " + r.URL.Path
		if r.Body != nil && r.Body != http.NoBody {
			body, err := io.ReadAll(r.Body)
			_ = r.Body.Close()
			if err == nil {
				for field, values := range ExtractSecrets(body) {
					for _, v := range values {
						if err := s.Register(field, v); err != nil {
							t.Helper()
							t.Errorf("%s：%v", where, err)
						}
					}
				}
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		requestURL := r.URL.String()

		rec := httpcapture.NewRecorder(w)
		next.ServeHTTP(rec, r)

		var findings []Finding
		findings = append(findings, s.Scan(ChannelURL, where, requestURL)...)
		findings = append(findings, s.Scan(ChannelResponse, where+" 標頭", headerText(w.Header()))...)
		findings = append(findings, s.Scan(ChannelResponse, fmt.Sprintf("%s → %d", where, rec.Status()), string(rec.Body()))...)
		for _, f := range findings {
			t.Helper()
			t.Errorf("Secret 洩漏：%s", f)
		}
	})
}

func headerText(h http.Header) string {
	var b strings.Builder
	for name, values := range h {
		for _, v := range values {
			b.WriteString(name)
			b.WriteString(": ")
			b.WriteString(v)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// ScanMail 掃描一封信的收件者、主旨與兩種本文，並保留內容供 Rescan 使用。
func (s *Scanner) ScanMail(to, cc []string, subject, textBody, htmlBody string) []Finding {
	// 位置用主旨標示；主旨若含 Secret 也會在掃描時被抓到並遮蔽
	location := "主旨「" + s.redact(subject) + "」"
	text := strings.Join([]string{
		"To: " + strings.Join(to, ", "),
		"Cc: " + strings.Join(cc, ", "),
		"Subject: " + subject,
		textBody,
		htmlBody,
	}, "\n")
	return s.Retain(ChannelMail, location, text)
}

// redact 把文字中登錄過的 Secret 換成遮蔽字樣。
func (s *Scanner) redact(text string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sec := range s.secrets {
		for _, variant := range sec.variants {
			text = strings.ReplaceAll(text, variant, masked)
		}
	}
	return text
}

// LogWriter 回傳一個 io.Writer：寫入的內容照常轉給 forward，同時掃描並保留。
// 把 slog 與標準 log 的輸出都導到這裡，就能涵蓋應用程式的全部日誌。
func (s *Scanner) LogWriter(forward io.Writer) io.Writer {
	return &logWriter{scanner: s, forward: forward}
}

type logWriter struct {
	scanner *Scanner
	forward io.Writer
	mu      sync.Mutex
	count   int
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.count++
	n := w.count
	w.mu.Unlock()
	w.scanner.Retain(ChannelLog, fmt.Sprintf("第 %d 筆輸出", n), string(p))
	return w.forward.Write(p)
}

// textColumnTypes 是要掃描的資料庫欄位型別。
var textColumnTypes = map[string]bool{
	"char": true, "varchar": true, "tinytext": true, "text": true, "mediumtext": true, "longtext": true, "json": true,
}

// ScanDatabase 掃描目前資料庫中每個資料表的所有文字欄位。
// Secret 應加密或雜湊後儲存，所以明文出現在任何欄位都算洩漏，包含稽核紀錄與通知紀錄。
func (s *Scanner) ScanDatabase(ctx context.Context, db *sql.DB) ([]Finding, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT TABLE_NAME, COLUMN_NAME, DATA_TYPE FROM information_schema.COLUMNS
		 WHERE TABLE_SCHEMA = DATABASE() ORDER BY TABLE_NAME, ORDINAL_POSITION`)
	if err != nil {
		return nil, fmt.Errorf("讀取資料表結構失敗: %w", err)
	}
	columns := map[string][]string{}
	var tables []string
	for rows.Next() {
		var table, column, dataType string
		if err := rows.Scan(&table, &column, &dataType); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("讀取資料表結構失敗: %w", err)
		}
		if !textColumnTypes[strings.ToLower(dataType)] {
			continue
		}
		if _, ok := columns[table]; !ok {
			tables = append(tables, table)
		}
		columns[table] = append(columns[table], column)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("讀取資料表結構失敗: %w", err)
	}
	_ = rows.Close()

	var findings []Finding
	for _, table := range tables {
		found, err := s.scanTable(ctx, db, table, columns[table])
		if err != nil {
			return findings, err
		}
		findings = append(findings, found...)
	}
	return findings, nil
}

func (s *Scanner) scanTable(ctx context.Context, db *sql.DB, table string, cols []string) ([]Finding, error) {
	selects := make([]string, len(cols))
	for i, c := range cols {
		selects[i] = "COALESCE(`" + c + "`, '')"
	}
	rows, err := db.QueryContext(ctx, "SELECT "+strings.Join(selects, ", ")+" FROM `"+table+"`")
	if err != nil {
		return nil, fmt.Errorf("讀取資料表 %s 失敗: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	values := make([]sql.RawBytes, len(cols))
	dest := make([]any, len(cols))
	for i := range values {
		dest[i] = &values[i]
	}
	var findings []Finding
	for rows.Next() {
		if err := rows.Scan(dest...); err != nil {
			return findings, fmt.Errorf("讀取資料表 %s 失敗: %w", table, err)
		}
		for i, v := range values {
			if len(v) == 0 {
				continue
			}
			findings = append(findings, s.Scan(ChannelDatabase, "資料表 "+table+" 欄位 "+cols[i], string(v))...)
		}
	}
	if err := rows.Err(); err != nil {
		return findings, fmt.Errorf("讀取資料表 %s 失敗: %w", table, err)
	}
	return findings, nil
}
