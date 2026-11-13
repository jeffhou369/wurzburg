package apicontract

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func mustLoad(t *testing.T) *Checker {
	t.Helper()
	c, err := Shared()
	if err != nil {
		t.Fatalf("描述檔應可載入並通過 OpenAPI 文件驗證: %v", err)
	}
	return c
}

func TestSpecLoadsAndListsOperations(t *testing.T) {
	c := mustLoad(t)
	ops := c.Operations()
	if len(ops) < 40 {
		t.Fatalf("描述檔的操作數量異常：%d", len(ops))
	}
	for _, want := range []string{"GET /healthz", "POST /api/v1/auth/login/local", "GET /api/v1/ip-records"} {
		found := false
		for _, op := range ops {
			if op == want {
				found = true
			}
		}
		if !found {
			t.Errorf("描述檔應包含操作 %q", want)
		}
	}
}

func TestCheckReportsContractViolations(t *testing.T) {
	c := mustLoad(t)
	jsonHeader := http.Header{"Content-Type": {"application/json; charset=utf-8"}}
	cases := []struct {
		name    string
		method  string
		path    string
		status  int
		header  http.Header
		body    string
		wantErr string // 空字串表示應通過
	}{
		{"符合描述檔", http.MethodGet, "/healthz", 200, jsonHeader, `{"status":"ok"}`, ""},
		{"未定義的路徑", http.MethodGet, "/api/v1/no-such-endpoint", 200, jsonHeader, `{}`, "沒有定義這個路徑"},
		{"未定義的方法", http.MethodDelete, "/healthz", 200, jsonHeader, `{}`, "沒有為這個路徑定義此方法"},
		{"未個別列出的錯誤狀態碼，本文符合共用錯誤格式", http.MethodGet, "/healthz", 418, jsonHeader, `{"code":"teapot","message":"m"}`, ""},
		{"未個別列出的錯誤狀態碼，本文不是共用錯誤格式", http.MethodGet, "/healthz", 418, jsonHeader, `{}`, "code"},
		{"未列出的成功狀態碼不可靠 default 涵蓋", http.MethodGet, "/healthz", 203, jsonHeader, `{"code":"x","message":"m"}`, "成功狀態碼 203"},
		{"有本文但描述檔沒有定義內容", http.MethodPost, "/api/v1/auth/logout", 204, jsonHeader, `{"message":"m"}`, "沒有為狀態碼 204 定義內容"},
		{"欄位型別錯誤", http.MethodGet, "/healthz", 200, jsonHeader, `{"status":123}`, "status"},
		{"描述檔沒有的欄位", http.MethodGet, "/healthz", 200, jsonHeader, `{"status":"ok","extra":1}`, "$.extra"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			err := c.Check(req, tc.status, tc.header, []byte(tc.body))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("應通過，卻回報: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("應回報含 %q 的錯誤，實際: %v", tc.wantErr, err)
			}
		})
	}
}

type fakeTB struct{ errors []string }

func (f *fakeTB) Helper() {}
func (f *fakeTB) Errorf(format string, args ...any) {
	f.errors = append(f.errors, format)
}

func TestWrapValidatesAPIResponsesOnly(t *testing.T) {
	c := mustLoad(t)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"status":123}`))
	})

	tb := &fakeTB{}
	rec := httptest.NewRecorder()
	c.Wrap(tb, handler, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if len(tb.errors) != 1 {
		t.Fatalf("不符合契約的 API 回應應回報 1 次，實際 %d 次", len(tb.errors))
	}
	if rec.Body.String() != `{"status":123}` {
		t.Errorf("包裝後回應本文應原樣送出，實際 %q", rec.Body.String())
	}

	tb = &fakeTB{}
	c.Wrap(tb, handler, nil).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/login", nil))
	if len(tb.errors) != 0 {
		t.Errorf("前端頁面路徑不屬於 API 契約，不應回報")
	}

	tb = &fakeTB{}
	ignore := func(r *http.Request) bool { return r.URL.Path == "/healthz" }
	c.Wrap(tb, handler, ignore).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if len(tb.errors) != 0 {
		t.Errorf("被忽略的請求不應回報")
	}
}

func TestUndocumentedFields(t *testing.T) {
	str := openapi3.NewStringSchema().NewRef()
	gateway := openapi3.NewObjectSchema().WithPropertyRef("ip_address", str).NewRef()
	base := openapi3.NewObjectSchema().WithPropertyRef("id", openapi3.NewIntegerSchema().NewRef()).WithPropertyRef("cidr", str).NewRef()
	extension := openapi3.NewObjectSchema().WithPropertyRef("gateways", openapi3.NewArraySchema().WithItems(gateway.Value).NewRef()).NewRef()
	detail := openapi3.NewAllOfSchema(base.Value, extension.Value).NewRef()

	// allOf：兩個成員的欄位都算已定義；陣列元素的多餘欄位回報一次並帶路徑
	value := map[string]any{
		"id": 1.0, "cidr": "10.0.0.0/24",
		"gateways": []any{
			map[string]any{"ip_address": "10.0.0.1", "surprise": true},
			map[string]any{"ip_address": "10.0.0.2", "surprise": true},
		},
		"not_in_spec": "x",
	}
	got := undocumentedFields(detail, value, "$")
	want := map[string]bool{"$.not_in_spec": true, "$.gateways[].surprise": true}
	if len(got) != len(want) {
		t.Fatalf("得到 %v，預期 %v", got, want)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("不應回報 %q", g)
		}
	}

	// additionalProperties 指定 schema 的物件可帶任意鍵
	fields := openapi3.NewObjectSchema().WithAdditionalProperties(openapi3.NewStringSchema())
	errSchema := openapi3.NewObjectSchema().WithPropertyRef("code", str).WithPropertyRef("fields", fields.NewRef()).NewRef()
	errBody := map[string]any{"code": "validation_error", "fields": map[string]any{"任意欄位": "說明"}}
	if got := undocumentedFields(errSchema, errBody, "$"); len(got) != 0 {
		t.Errorf("fields 內的鍵不應回報：%v", got)
	}

	// 沒有列出任何欄位的物件屬於自由格式，不回報
	free := openapi3.NewObjectSchema().NewRef()
	if got := undocumentedFields(free, map[string]any{"anything": 1.0}, "$"); len(got) != 0 {
		t.Errorf("自由格式物件不應回報：%v", got)
	}
}
