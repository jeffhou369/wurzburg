package server

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// clientIPMiddleware 決定請求的客戶端 IP，寫回 r.RemoteAddr，供登入限流與稽核紀錄使用。
//
// 取代 chi 已棄用的 middleware.RealIP：RealIP 無條件採信 X-Forwarded-For 最左側、X-Real-IP 與
// True-Client-IP，任何客戶端都能自帶標頭偽造來源 IP，繞過以 IP 為鍵的登入限流並竄改稽核來源。
//
// 規則：
//   - 直接連線來源不在 trusted 清單內（預設清單為空）：完全不看轉送標頭，維持原本的 RemoteAddr。
//   - 來源是信任的代理：由 X-Forwarded-For 最右側往左，略過同為信任代理的位址，取第一個不是信任代理的位址；
//     最左側的值由客戶端自行填寫，不可採信。沒有 X-Forwarded-For 時才使用 X-Real-IP。
//   - 任何無法解析的值都不採用，維持原本的 RemoteAddr。
func clientIPMiddleware(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if len(trusted) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ip, ok := forwardedClientIP(r, trusted); ok {
				r.RemoteAddr = ip.String()
			}
			next.ServeHTTP(w, r)
		})
	}
}

func forwardedClientIP(r *http.Request, trusted []netip.Prefix) (netip.Addr, bool) {
	peer, ok := parseHostAddr(r.RemoteAddr)
	if !ok || !isTrusted(peer, trusted) {
		return netip.Addr{}, false
	}

	if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
		hops := strings.Split(strings.Join(xff, ","), ",")
		for i := len(hops) - 1; i >= 0; i-- {
			addr, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
			if err != nil {
				return netip.Addr{}, false
			}
			addr = addr.Unmap()
			if !isTrusted(addr, trusted) {
				return addr, true
			}
		}
		return netip.Addr{}, false
	}

	if real := strings.TrimSpace(r.Header.Get("X-Real-IP")); real != "" {
		if addr, err := netip.ParseAddr(real); err == nil {
			return addr.Unmap(), true
		}
	}
	return netip.Addr{}, false
}

// parseHostAddr 解析 "host:port" 或單純的位址。
func parseHostAddr(remoteAddr string) (netip.Addr, bool) {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func isTrusted(addr netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
