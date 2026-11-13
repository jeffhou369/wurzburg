// Package apicontract 提供測試用的 OpenAPI 回應契約檢查：把整合測試送進路由器的每一次
// HTTP 交換拿去比對 docs/api/openapi.yaml，確認描述檔與實作沒有脫節。
//
// 只驗證回應。測試會刻意送出不合法的請求來驗證錯誤處理，請求本文不在檢查範圍。
//
// 比 OpenAPI 的預設語意嚴格的地方（目的是抓出描述檔與實作脫節，而不只是「不違反 schema」）：
//   - 成功（2xx、3xx）的狀態碼必須明確列在操作的 responses，不能靠 default 涵蓋。
//   - 回應有本文時，描述檔必須為該狀態碼定義內容；沒定義等於本文沒被驗證。
//   - JSON 物件中出現描述檔沒有列出的欄位視為不符合（除非 schema 明確宣告 additionalProperties）。
//
// 本套件只供測試匯入，不會被 cmd/wurzburg 連結進執行檔。
package apicontract

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/jeffhou369/wurzburg/backend/internal/testutil/httpcapture"
)

// TB 是 testing.TB 中本套件用到的部分。
type TB interface {
	Helper()
	Errorf(format string, args ...any)
}

// Checker 持有載入後的描述檔，並累計哪些操作被測試打到。可由多個測試並行使用。
type Checker struct {
	doc    *openapi3.T
	router routers.Router

	mu      sync.Mutex
	covered map[string]int
}

// SpecPath 回傳儲存庫中 OpenAPI 總入口的絕對路徑。
func SpecPath() string {
	_, file, _, _ := runtime.Caller(0)
	// 本檔位於 src/backend/internal/testutil/apicontract/
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "..", "docs", "api", "openapi.yaml")
}

// Load 載入並驗證描述檔（含各資源檔的外部 $ref）。
func Load(specPath string) (*Checker, error) {
	ctx := context.Background()
	loader := &openapi3.Loader{Context: ctx, IsExternalRefsAllowed: true}
	doc, err := loader.LoadFromFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("載入 OpenAPI 描述檔失敗: %w", err)
	}
	if err := doc.Validate(ctx); err != nil {
		return nil, fmt.Errorf("OpenAPI 描述檔不合法: %w", err)
	}
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		return nil, fmt.Errorf("建立描述檔路由失敗: %w", err)
	}
	return &Checker{doc: doc, router: router, covered: make(map[string]int)}, nil
}

var (
	sharedOnce sync.Once
	shared     *Checker
	sharedErr  error
)

// Shared 回傳整個測試行程共用的檢查器；描述檔只載入一次，涵蓋率也累計在同一份。
func Shared() (*Checker, error) {
	sharedOnce.Do(func() { shared, sharedErr = Load(SpecPath()) })
	return shared, sharedErr
}

// InScope 判斷路徑是否屬於 API 契約的範圍；前端靜態檔與 SPA 回退路由不在描述檔內。
func InScope(path string) bool {
	return path == "/healthz" || strings.HasPrefix(path, "/api/")
}

// Wrap 包住路由器：照常處理請求，之後驗證回應。不符合契約時以 t.Errorf 回報。
// ignore 回傳 true 的請求不驗證（例如測試自行掛載、不屬於產品契約的路由）。
func (c *Checker) Wrap(t TB, next http.Handler, ignore func(*http.Request) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !InScope(r.URL.Path) || (ignore != nil && ignore(r)) {
			next.ServeHTTP(w, r)
			return
		}
		// 處理器可能改寫 r.URL（chi 的子路由會調整 RoutePath 但不動 URL）；先複製比對用的請求。
		probe := r.Clone(r.Context())
		probe.Body = http.NoBody

		rec := httpcapture.NewRecorder(w)
		next.ServeHTTP(rec, r)

		if err := c.Check(probe, rec.Status(), w.Header(), rec.Body()); err != nil {
			t.Helper()
			t.Errorf("回應不符合 OpenAPI 描述檔：%s %s → %d\n%v\n回應本文：%s", r.Method, r.URL.Path, rec.Status(), err, excerpt(rec.Body()))
		}
	})
}

// Check 驗證一次回應，並記錄被打到的操作。
func (c *Checker) Check(req *http.Request, status int, header http.Header, body []byte) error {
	route, pathParams, err := c.router.FindRoute(req)
	if err != nil {
		if errors.Is(err, routers.ErrPathNotFound) {
			return errors.New("描述檔沒有定義這個路徑")
		}
		if errors.Is(err, routers.ErrMethodNotAllowed) {
			return errors.New("描述檔沒有為這個路徑定義此方法")
		}
		return fmt.Errorf("比對描述檔路由失敗: %w", err)
	}

	c.mu.Lock()
	c.covered[operationKey(req.Method, route.Path)]++
	c.mu.Unlock()

	opts := &openapi3filter.Options{
		IncludeResponseStatus: true,
		MultiError:            true,
		AuthenticationFunc:    openapi3filter.NoopAuthenticationFunc,
	}
	input := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request:    req,
			PathParams: pathParams,
			Route:      route,
			Options:    opts,
		},
		Status:  status,
		Header:  header,
		Options: opts,
	}
	input.SetBodyBytes(body)
	if err := openapi3filter.ValidateResponse(req.Context(), input); err != nil {
		return err
	}
	return strictChecks(route.Operation, status, header, body)
}

// strictChecks 做 kin-openapi 不會回報、但代表描述檔與實作脫節的檢查。
func strictChecks(op *openapi3.Operation, status int, header http.Header, body []byte) error {
	explicit := op.Responses.Status(status)
	if explicit == nil && status < 400 {
		return fmt.Errorf("成功狀態碼 %d 沒有明確列在描述檔（不可只靠 default 涵蓋）", status)
	}
	ref := explicit
	if ref == nil {
		ref = op.Responses.Default()
	}
	if ref == nil || ref.Value == nil {
		return nil
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	if len(ref.Value.Content) == 0 {
		return fmt.Errorf("回應帶有本文（Content-Type: %s），但描述檔沒有為狀態碼 %d 定義內容", header.Get("Content-Type"), status)
	}
	media := ref.Value.Content.Get(header.Get("Content-Type"))
	if media == nil || media.Schema == nil || !isJSON(header.Get("Content-Type")) {
		return nil
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return fmt.Errorf("回應本文不是合法的 JSON: %w", err)
	}
	if extra := undocumentedFields(media.Schema, value, "$"); len(extra) > 0 {
		sort.Strings(extra)
		return fmt.Errorf("回應包含描述檔沒有定義的欄位：%s", strings.Join(extra, "、"))
	}
	return nil
}

func isJSON(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	return err == nil && (mt == "application/json" || strings.HasSuffix(mt, "+json"))
}

// undocumentedFields 回傳 value 中沒有出現在 schema 的欄位路徑。
// allOf 的成員視為同一個物件的欄位來源；oneOf／anyOf 無法判定採用哪個分支，不做這項檢查。
func undocumentedFields(ref *openapi3.SchemaRef, value any, path string) []string {
	if ref == nil || ref.Value == nil {
		return nil
	}
	s := ref.Value
	if len(s.OneOf) > 0 || len(s.AnyOf) > 0 {
		return nil
	}
	switch v := value.(type) {
	case map[string]any:
		props, additional, open := objectShape(ref)
		var out []string
		for key, child := range v {
			childPath := path + "." + key
			if prop, ok := props[key]; ok {
				out = append(out, undocumentedFields(prop, child, childPath)...)
				continue
			}
			if additional != nil {
				out = append(out, undocumentedFields(additional, child, childPath)...)
				continue
			}
			if !open {
				out = append(out, childPath)
			}
		}
		return out
	case []any:
		items := arrayItems(ref)
		var out []string
		seen := map[string]bool{}
		for _, child := range v {
			// 陣列各元素的欄位路徑相同，只回報一次
			for _, e := range undocumentedFields(items, child, path+"[]") {
				if !seen[e] {
					seen[e] = true
					out = append(out, e)
				}
			}
		}
		return out
	}
	return nil
}

// objectShape 彙整 schema（含 allOf 成員）列出的欄位。
// additional 是 additionalProperties 指定的 schema；open 表示允許任意額外欄位
// （明確宣告 additionalProperties: true，或 schema 完全沒有列出欄位、屬於自由格式的物件）。
func objectShape(ref *openapi3.SchemaRef) (props map[string]*openapi3.SchemaRef, additional *openapi3.SchemaRef, open bool) {
	props = map[string]*openapi3.SchemaRef{}
	var walk func(r *openapi3.SchemaRef)
	walk = func(r *openapi3.SchemaRef) {
		if r == nil || r.Value == nil {
			return
		}
		for name, p := range r.Value.Properties {
			props[name] = p
		}
		if ap := r.Value.AdditionalProperties; ap.Schema != nil {
			additional = ap.Schema
		} else if ap.Has != nil && *ap.Has {
			open = true
		}
		for _, member := range r.Value.AllOf {
			walk(member)
		}
	}
	walk(ref)
	if len(props) == 0 && additional == nil {
		open = true
	}
	return props, additional, open
}

func arrayItems(ref *openapi3.SchemaRef) *openapi3.SchemaRef {
	if ref.Value.Items != nil {
		return ref.Value.Items
	}
	for _, member := range ref.Value.AllOf {
		if member != nil && member.Value != nil && member.Value.Items != nil {
			return member.Value.Items
		}
	}
	return nil
}

// Operations 回傳描述檔定義的所有操作，格式為「METHOD /path/{param}」，已排序。
func (c *Checker) Operations() []string {
	var ops []string
	for path, item := range c.doc.Paths.Map() {
		for method := range item.Operations() {
			ops = append(ops, operationKey(method, path))
		}
	}
	sort.Strings(ops)
	return ops
}

// Uncovered 回傳描述檔有定義、但到目前為止沒有任何回應被驗證過的操作。
func (c *Checker) Uncovered() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, op := range c.Operations() {
		if c.covered[op] == 0 {
			out = append(out, op)
		}
	}
	return out
}

// excerpt 截取回應本文的開頭供錯誤訊息使用；非文字內容只顯示長度。
func excerpt(body []byte) string {
	const limit = 600
	if len(body) == 0 {
		return "（空）"
	}
	if !utf8.Valid(body) {
		return fmt.Sprintf("（%d 位元組的非文字內容）", len(body))
	}
	if len(body) > limit {
		return string(bytes.ToValidUTF8(body[:limit], nil)) + "…"
	}
	return string(body)
}

func operationKey(method, path string) string {
	return strings.ToUpper(method) + " " + path
}
