package secretscan

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jeffhou369/wurzburg/backend/internal/testutil/apicontract"
)

const sample = `Tr0ub4dor&3 <x>"y"`

func TestRegisterRejectsShortSamples(t *testing.T) {
	s := New()
	if err := s.Register("snmp_community", "public"); err == nil {
		t.Error("太短的 Secret 樣本應被拒絕")
	}
	if err := s.Register("password", ""); err != nil {
		t.Errorf("空字串不登錄也不算錯誤: %v", err)
	}
	if err := s.Register("password", sample); err != nil {
		t.Errorf("可辨識的樣本應可登錄: %v", err)
	}
}

func TestScanFindsPlainAndEscapedForms(t *testing.T) {
	cases := map[string]string{
		"明文":           "error: bind failed with " + sample,
		"URL 查詢字串":     "/api/v1/x?community=Tr0ub4dor%263+%3Cx%3E%22y%22",
		"HTML 跳脫":      "<td>Tr0ub4dor&amp;3 &lt;x&gt;&#34;y&#34;</td>",
		"JSON 跳脫":      `{"detail":"Tr0ub4dor&3 <x>\"y\""}`,
		"JSON HTML 安全": `{"detail":"Tr0ub4dor&3 <x>\"y\""}`,
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			s := New()
			if err := s.Register("smtp_password", sample); err != nil {
				t.Fatal(err)
			}
			found := s.Scan(ChannelLog, "測試", text)
			if len(found) != 1 {
				t.Fatalf("應發現 1 處洩漏，實際 %d", len(found))
			}
			report := found[0].String()
			if strings.Contains(report, sample) || strings.Contains(report, "Tr0ub4dor") {
				t.Errorf("洩漏報告不可印出 Secret：%s", report)
			}
			if !strings.Contains(report, "smtp_password") || !strings.Contains(report, masked) {
				t.Errorf("洩漏報告應包含標籤與遮蔽後的片段：%s", report)
			}
		})
	}

	s := New()
	_ = s.Register("smtp_password", sample)
	if found := s.Scan(ChannelLog, "測試", "一切正常，沒有任何機敏資料"); len(found) != 0 {
		t.Errorf("沒有 Secret 的文字不應回報：%v", found)
	}
}

func TestRetainedTextIsRescannedWithLaterSecrets(t *testing.T) {
	s := New()
	if found := s.Retain(ChannelLog, "第 1 筆輸出", "建立初始管理員，密碼為 "+sample); len(found) != 0 {
		t.Fatal("Secret 尚未登錄時不應回報")
	}
	_ = s.Register("password", sample)
	if len(s.Findings()) != 0 {
		t.Fatal("登錄本身不應產生洩漏")
	}
	s.Rescan()
	s.Rescan() // 重複掃描不應重複回報
	if got := s.Findings(); len(got) != 1 || got[0].Channel != ChannelLog {
		t.Fatalf("重新掃描應發現先前保留的日誌洩漏一次，實際 %v", got)
	}
}

func TestExtractSecretsFindsNestedSecretFields(t *testing.T) {
	body := `{"name":"sw","snmp_community":"CommunityValue#1","vrfs":[{"community":"NestedValue#2"}],"notes":"password"}`
	got := ExtractSecrets([]byte(body))
	if len(got["snmp_community"]) != 1 || got["snmp_community"][0] != "CommunityValue#1" {
		t.Errorf("應取出 snmp_community：%v", got)
	}
	if len(got["community"]) != 1 || got["community"][0] != "NestedValue#2" {
		t.Errorf("應取出巢狀的 community：%v", got)
	}
	if len(got) != 2 {
		t.Errorf("不是 Secret 的欄位不應取出：%v", got)
	}
	if len(ExtractSecrets([]byte("not json"))) != 0 {
		t.Error("不是 JSON 的本文應回傳空結果")
	}
}

type fakeTB struct{ errors []string }

func (f *fakeTB) Helper() {}
func (f *fakeTB) Errorf(format string, args ...any) {
	f.errors = append(f.errors, format)
}

func TestWrapRegistersRequestSecretsAndScansResponse(t *testing.T) {
	const community = "WrapCommunity#77"
	request := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/switches", strings.NewReader(`{"snmp_community":"`+community+`"}`))
		r.Header.Set("Content-Type", "application/json")
		return r
	}

	t.Run("回應回顯 Secret", func(t *testing.T) {
		s, tb := New(), &fakeTB{}
		var seenBody string
		h := s.Wrap(tb, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			buf := make([]byte, 256)
			n, _ := r.Body.Read(buf)
			seenBody = string(buf[:n])
			_, _ = w.Write([]byte(`{"snmp_community":"` + community + `"}`))
		}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, request())
		if !strings.Contains(seenBody, community) {
			t.Error("處理器應仍能讀到完整的請求本文")
		}
		if len(tb.errors) != 1 {
			t.Fatalf("回應回顯 Secret 應回報 1 次，實際 %d", len(tb.errors))
		}
		if got := s.Findings(); len(got) != 1 || got[0].Channel != ChannelResponse {
			t.Errorf("應記錄 HTTP 回應的洩漏：%v", got)
		}
	})

	t.Run("標頭洩漏 Secret", func(t *testing.T) {
		s, tb := New(), &fakeTB{}
		h := s.Wrap(tb, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Debug", community)
			w.WriteHeader(http.StatusCreated)
		}))
		h.ServeHTTP(httptest.NewRecorder(), request())
		if len(tb.errors) != 1 {
			t.Fatalf("標頭洩漏應回報 1 次，實際 %d", len(tb.errors))
		}
	})

	t.Run("回應只有旗標", func(t *testing.T) {
		s, tb := New(), &fakeTB{}
		h := s.Wrap(tb, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"has_credential":true}`))
		}))
		h.ServeHTTP(httptest.NewRecorder(), request())
		if len(tb.errors) != 0 {
			t.Errorf("沒有洩漏不應回報：%v", tb.errors)
		}
	})

	t.Run("Secret 樣本太短", func(t *testing.T) {
		s, tb := New(), &fakeTB{}
		h := s.Wrap(tb, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		r := httptest.NewRequest(http.MethodPost, "/api/v1/switches", strings.NewReader(`{"snmp_community":"public"}`))
		h.ServeHTTP(httptest.NewRecorder(), r)
		if len(tb.errors) != 1 {
			t.Fatalf("太短的 Secret 樣本應回報 1 次，實際 %d", len(tb.errors))
		}
	})
}

func TestScanMailCoversAllParts(t *testing.T) {
	const pw = "MailLeakSample#55"
	for name, mail := range map[string][5]string{
		"主旨":      {"", "", "重設完成 " + pw, "", ""},
		"純文字本文":   {"", "", "通知", "密碼是 " + pw, ""},
		"HTML 本文": {"", "", "通知", "", "<p>" + pw + "</p>"},
		"收件者":     {pw + "@example.test", "", "通知", "", ""},
	} {
		t.Run(name, func(t *testing.T) {
			s := New()
			_ = s.Register("smtp_password", pw)
			found := s.ScanMail([]string{mail[0]}, []string{mail[1]}, mail[2], mail[3], mail[4])
			if len(found) != 1 || found[0].Channel != ChannelMail {
				t.Fatalf("應發現信件洩漏：%v", found)
			}
			if strings.Contains(found[0].String(), pw) {
				t.Errorf("洩漏報告不可印出 Secret：%s", found[0])
			}
		})
	}
}

func TestLogWriterForwardsAndRetains(t *testing.T) {
	s := New()
	var forwarded strings.Builder
	w := s.LogWriter(&forwarded)
	_, _ = w.Write([]byte("level=INFO msg=連線失敗 dsn=user:LogLeakSample#66@tcp(db)\n"))
	if !strings.Contains(forwarded.String(), "連線失敗") {
		t.Error("日誌應照常轉送")
	}
	_ = s.Register("資料庫密碼", "LogLeakSample#66")
	s.Rescan()
	if got := s.Findings(); len(got) != 1 || got[0].Channel != ChannelLog {
		t.Fatalf("應發現日誌洩漏：%v", got)
	}
}

// 描述檔中標為 format: password 的欄位都必須在 Secret 欄位清單內，否則新增的 Secret 欄位不會被自動登錄。
func TestSecretFieldNamesCoverSpecPasswordFields(t *testing.T) {
	dir := filepath.Join(filepath.Dir(apicontract.SpecPath()), "resources")
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("找不到 API 資源描述檔: %v", err)
	}
	re := regexp.MustCompile(`(?m)^\s+([a-z0-9_]+):\n\s+type: string\n\s+format: password`)
	found := 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
			found++
			if !secretFieldNames[m[1]] {
				t.Errorf("%s 的欄位 %q 標為 format: password，但不在 Secret 欄位清單內", filepath.Base(f), m[1])
			}
		}
	}
	if found < 8 {
		t.Fatalf("描述檔中 format: password 的欄位數量異常：%d", found)
	}
}
