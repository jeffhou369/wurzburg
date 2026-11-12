package authsource

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"strings"

	"github.com/go-ldap/ldap/v3"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
)

type ADConfig struct {
	URL          string // ldaps://dc.example.com:636 or ldap://...
	UseTLS       bool
	BindDN       string
	BindPassword string
	BaseDN       string
	AllowedGroup string // 留空代表不限制
	UserAttr     string // sAMAccountName / userPrincipalName
	MailAttr     string // mail
	NameAttr     string // displayName
}

type ADClient struct {
	cfg ADConfig
}

func NewADClient(cfg ADConfig) *ADClient {
	if cfg.UserAttr == "" {
		cfg.UserAttr = "sAMAccountName"
	}
	if cfg.MailAttr == "" {
		cfg.MailAttr = "mail"
	}
	if cfg.NameAttr == "" {
		cfg.NameAttr = "displayName"
	}
	return &ADClient{cfg: cfg}
}

func (c *ADClient) Authenticate(ctx context.Context, username, password string) (*interfaces.OrgUserInfo, error) {
	if username == "" || password == "" {
		return nil, interfaces.ErrInvalidCredentials
	}

	u, err := url.Parse(c.cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("無效的 AD 伺服器網址: %w", err)
	}

	var conn *ldap.Conn
	if strings.EqualFold(u.Scheme, "ldaps") {
		conn, err = ldap.DialURL(c.cfg.URL, ldap.DialWithTLSConfig(&tls.Config{InsecureSkipVerify: false}))
	} else {
		conn, err = ldap.DialURL(c.cfg.URL)
		if err == nil && c.cfg.UseTLS {
			err = conn.StartTLS(&tls.Config{InsecureSkipVerify: false})
		}
	}

	if err != nil {
		return nil, fmt.Errorf("%w: %v", interfaces.ErrAuthUnavailable, err)
	}
	defer func() { _ = conn.Close() }()

	// 1. 若有指定 BindDN，先以管理帳號 Bind 進行搜尋
	if c.cfg.BindDN != "" {
		if err := conn.Bind(c.cfg.BindDN, c.cfg.BindPassword); err != nil {
			return nil, fmt.Errorf("AD 服務帳號繫結失敗: %w", err)
		}
	}

	// 2. 搜尋使用者 Entry（防止 LDAP 注入）
	escapedUser := ldap.EscapeFilter(username)
	filter := fmt.Sprintf("(&(objectClass=user)(%s=%s))", c.cfg.UserAttr, escapedUser)

	searchReq := ldap.NewSearchRequest(
		c.cfg.BaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		1,
		10,
		false,
		filter,
		[]string{"dn", c.cfg.UserAttr, c.cfg.MailAttr, c.cfg.NameAttr, "memberOf"},
		nil,
	)

	sr, err := conn.SearchWithPaging(searchReq, 1)
	if err != nil || len(sr.Entries) == 0 {
		return nil, interfaces.ErrInvalidCredentials
	}

	entry := sr.Entries[0]
	userDN := entry.DN

	// 3. 檢查群組限制
	if c.cfg.AllowedGroup != "" {
		inGroup := false
		members := entry.GetAttributeValues("memberOf")
		for _, g := range members {
			if strings.EqualFold(g, c.cfg.AllowedGroup) || strings.Contains(strings.ToLower(g), strings.ToLower(c.cfg.AllowedGroup)) {
				inGroup = true
				break
			}
		}
		if !inGroup {
			return nil, interfaces.ErrGroupRestricted
		}
	}

	// 4. 以使用者的 DN 與輸入的密碼進行實際 Bind 驗證
	if err := conn.Bind(userDN, password); err != nil {
		return nil, interfaces.ErrInvalidCredentials
	}

	displayName := entry.GetAttributeValue(c.cfg.NameAttr)
	if displayName == "" {
		displayName = username
	}

	return &interfaces.OrgUserInfo{
		Username:    username,
		DisplayName: displayName,
		Mail:        entry.GetAttributeValue(c.cfg.MailAttr),
		AuthSource:  "ad",
	}, nil
}
