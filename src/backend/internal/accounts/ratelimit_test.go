package accounts

import "testing"

func TestRateLimitKeyUsesHostFromRemoteAddr(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		want       string
	}{
		{name: "IPv4 with port", remoteAddr: "192.0.2.10:54321", want: "192.0.2.10"},
		{name: "IPv6 with port", remoteAddr: "[2001:db8::1]:54321", want: "2001:db8::1"},
		{name: "plain IPv4", remoteAddr: "192.0.2.10", want: "192.0.2.10"},
		{name: "plain IPv6", remoteAddr: "2001:db8::1", want: "2001:db8::1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rateLimitKey(tt.remoteAddr); got != tt.want {
				t.Fatalf("rateLimitKey(%q) = %q, want %q", tt.remoteAddr, got, tt.want)
			}
		})
	}
}
