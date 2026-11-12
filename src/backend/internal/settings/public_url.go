package settings

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// NormalizePublicBaseURL 驗證園區對外網址；目前僅支援根路由部署。
// 空字串代表尚未設定，不能以請求 Host 或 localhost 取代。
func NormalizePublicBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(raw, "\\\r\n\t#?") || (u.Path != "" && u.Path != "/") {
		return "", errors.New("公開網址必須是無帳密、路徑、查詢參數或片段的 HTTP(S) 根網址")
	}
	if p := u.Port(); p != "" {
		port, err := strconv.Atoi(p)
		if err != nil || port < 1 || port > 65535 {
			return "", errors.New("公開網址的埠號必須介於 1 到 65535")
		}
	}
	if strings.Contains(u.Hostname(), ":") && net.ParseIP(u.Hostname()) == nil {
		return "", errors.New("公開網址主機格式無效")
	}
	u.Path, u.RawPath = "", ""
	return u.String(), nil
}

// PublicLink 以設定值建構需登入的工作臺連結，不信任外部請求標頭。
func (s *Store) PublicLink(ctx context.Context, query string) (string, error) {
	raw, err := s.Get(ctx, "public_base_url")
	if err != nil {
		return "", err
	}
	base, err := NormalizePublicBaseURL(raw)
	if err != nil {
		return "", err
	}
	if base == "" {
		return "", errors.New("請先在園區資訊設定公開網址，才能寄送可操作的通知連結")
	}
	u, _ := url.Parse(base)
	u.Path, u.RawQuery = "/", query
	return u.String(), nil
}
