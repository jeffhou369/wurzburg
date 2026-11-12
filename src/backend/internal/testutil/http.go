package testutil

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
)

// NewAuthRequest 建立已帶入測試用身分標頭與 CSRF 防護標頭的 HTTP 請求，
// 讓功能測試可用一行直接以系統管理員或一般使用者身分呼叫 API。
func NewAuthRequest(method, path string, body any, userID int64, role, username string) *http.Request {
	var bodyReader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(raw)
	} else {
		bodyReader = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, path, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("X-Test-User-ID", formatInt64(userID))
	req.Header.Set("X-Test-Role", role)
	if username != "" {
		req.Header.Set("X-Test-Username", username)
	}
	return req
}

func formatInt64(v int64) string {
	// 避免額外 import strconv 的簡單轉字串輔助
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
