package authsource

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
)

type RADIUSConfig struct {
	Host      string
	Port      int
	Secret    string
	TimeoutMs int
}

type RADIUSClient struct {
	cfg RADIUSConfig
}

func NewRADIUSClient(cfg RADIUSConfig) *RADIUSClient {
	if cfg.Port <= 0 {
		cfg.Port = 1812
	}
	if cfg.TimeoutMs <= 0 {
		cfg.TimeoutMs = 5000
	}
	return &RADIUSClient{cfg: cfg}
}

func (c *RADIUSClient) Authenticate(ctx context.Context, username, password string) (*interfaces.OrgUserInfo, error) {
	if username == "" || password == "" {
		return nil, interfaces.ErrInvalidCredentials
	}
	if c.cfg.Host == "" || c.cfg.Secret == "" {
		return nil, interfaces.ErrAuthUnavailable
	}

	packet := radius.New(radius.CodeAccessRequest, []byte(c.cfg.Secret))
	if err := rfc2865.UserName_SetString(packet, username); err != nil {
		return nil, err
	}
	if err := rfc2865.UserPassword_SetString(packet, password); err != nil {
		return nil, err
	}

	addr := fmt.Sprintf("%s:%d", c.cfg.Host, c.cfg.Port)
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.TimeoutMs)*time.Millisecond)
	defer cancel()

	response, err := radius.Exchange(timeoutCtx, packet, addr)
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return nil, fmt.Errorf("%w: RADIUS 連線逾時", interfaces.ErrAuthUnavailable)
		}
		// 若共享密鑰錯誤，RADIUS Response 驗證會回傳 NonAuthenticResponseError
		var nonAuth *radius.NonAuthenticResponseError
		if errors.As(err, &nonAuth) {
			return nil, fmt.Errorf("%w: RADIUS Shared Secret 驗證不符", interfaces.ErrAuthUnavailable)
		}
		return nil, fmt.Errorf("%w: %v", interfaces.ErrAuthUnavailable, err)
	}

	if response.Code != radius.CodeAccessAccept {
		return nil, interfaces.ErrInvalidCredentials
	}

	// RADIUS 只回應驗證成功與否，無電子郵件屬性，顯示名稱即為帳號
	return &interfaces.OrgUserInfo{
		Username:    username,
		DisplayName: username,
		Mail:        "",
		AuthSource:  "radius",
	}, nil
}
