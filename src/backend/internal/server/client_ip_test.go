package server

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/jeffhou369/wurzburg/backend/internal/config"
)

// echoRemoteAddr 回傳處理器看到的 r.RemoteAddr；登入限流與稽核都以它作為來源 IP。
type echoRemoteAddr struct{}

func (echoRemoteAddr) RegisterRoutes(api chi.Router) {
	api.Get("/test-remote-addr", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.RemoteAddr))
	})
}

func remoteAddrSeen(t *testing.T, trusted []netip.Prefix, peer string, headers map[string]string) string {
	t.Helper()
	srv := New(&config.Config{AppEnv: "test", TrustedProxies: trusted}, nil, nil, echoRemoteAddr{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/test-remote-addr", nil)
	req.RemoteAddr = peer
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("測試端點回應 %d：%s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// 預設沒有信任的代理：客戶端自帶的轉送標頭不得改變來源 IP，否則可藉偽造 IP 繞過登入限流與竄改稽核來源。
func TestClientIPIgnoresForwardedHeadersFromUntrustedPeer(t *testing.T) {
	for _, h := range []map[string]string{
		{"X-Forwarded-For": "198.51.100.7"},
		{"X-Real-IP": "198.51.100.7"},
		{"True-Client-IP": "198.51.100.7"},
	} {
		if got := remoteAddrSeen(t, nil, "192.0.2.10:51000", h); got != "192.0.2.10:51000" {
			t.Errorf("標頭 %v：來源 IP 被改成 %q", h, got)
		}
	}
}

func TestClientIPUsesForwardedHeaderFromTrustedProxy(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("172.20.0.0/16")}
	cases := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		// nginx 範例設定 X-Real-IP 為實際連線來源
		{"X-Real-IP", map[string]string{"X-Real-IP": "198.51.100.7"}, "198.51.100.7"},
		// 客戶端可在 X-Forwarded-For 前面塞假值，只能採信最右側、非信任代理的位址
		{"X-Forwarded-For 最右側", map[string]string{"X-Forwarded-For": "203.0.113.9, 198.51.100.7"}, "198.51.100.7"},
		{"略過多層信任代理", map[string]string{"X-Forwarded-For": "198.51.100.7, 172.20.0.3"}, "198.51.100.7"},
		{"無效值不採用", map[string]string{"X-Real-IP": "not-an-ip"}, "172.20.0.2:40000"},
		{"沒有標頭", nil, "172.20.0.2:40000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := remoteAddrSeen(t, trusted, "172.20.0.2:40000", tc.headers); got != tc.want {
				t.Errorf("得到 %q，預期 %q", got, tc.want)
			}
		})
	}
}

// 直接連線來源不在信任清單內時，即使已設定信任代理也不得採信標頭。
func TestClientIPIgnoresHeadersWhenPeerOutsideTrustedProxies(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("172.20.0.0/16")}
	if got := remoteAddrSeen(t, trusted, "192.0.2.10:51000", map[string]string{"X-Real-IP": "198.51.100.7"}); got != "192.0.2.10:51000" {
		t.Errorf("來源 IP 被改成 %q", got)
	}
}
