package authsource

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/go-ldap/ldap/v3"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
)

func TestRADIUSClient_RealUDPExchange(t *testing.T) {
	// 啟動測試用本機 RADIUS 模擬伺服器（隨機埠）
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("監聽本機 UDP 失敗: %v", err)
	}
	defer func() { _ = pc.Close() }()

	udpAddr := pc.LocalAddr().(*net.UDPAddr)
	sharedSecret := "testing_radius_secret_123"

	server := radius.PacketServer{
		SecretSource: radius.StaticSecretSource([]byte(sharedSecret)),
		Handler: radius.HandlerFunc(func(w radius.ResponseWriter, r *radius.Request) {
			username := rfc2865.UserName_GetString(r.Packet)
			password := rfc2865.UserPassword_GetString(r.Packet)
			if username == "lin" && password == "correct_pap_pass" {
				_ = w.Write(r.Response(radius.CodeAccessAccept))
				return
			}
			_ = w.Write(r.Response(radius.CodeAccessReject))
		}),
	}

	go func() {
		_ = server.Serve(pc)
	}()
	defer func() { _ = server.Shutdown(context.Background()) }()

	client := NewRADIUSClient(RADIUSConfig{
		Host:      "127.0.0.1",
		Port:      udpAddr.Port,
		Secret:    sharedSecret,
		TimeoutMs: 2000,
	})

	// 1. 正確帳密驗證成功，且無信箱、顯示名稱為帳號
	info, err := client.Authenticate(context.Background(), "lin", "correct_pap_pass")
	if err != nil {
		t.Fatalf("預期驗證成功，實際錯誤: %v", err)
	}
	if info.Username != "lin" || info.DisplayName != "lin" || info.Mail != "" || info.AuthSource != "radius" {
		t.Fatalf("RADIUS 回傳資料不符: %+v", info)
	}

	// 2. 錯誤密碼驗證失敗
	if _, err := client.Authenticate(context.Background(), "lin", "wrong_pass"); !errors.Is(err, interfaces.ErrInvalidCredentials) {
		t.Fatalf("預期 ErrInvalidCredentials，實際 %v", err)
	}

	// 3. 錯誤 shared secret 應回傳不可用/驗證不符
	badSecretClient := NewRADIUSClient(RADIUSConfig{
		Host:      "127.0.0.1",
		Port:      udpAddr.Port,
		Secret:    "wrong_shared_secret",
		TimeoutMs: 500,
	})
	if _, err := badSecretClient.Authenticate(context.Background(), "lin", "correct_pap_pass"); !errors.Is(err, interfaces.ErrAuthUnavailable) {
		t.Fatalf("錯誤 secret 預期 ErrAuthUnavailable，實際 %v", err)
	}
}

func TestLDAPFilterEscaping(t *testing.T) {
	// 驗證 LDAP 查詢字元跳脫防止篩選器注入攻擊
	malicious := "admin)(|(objectClass=*)"
	escaped := ldap.EscapeFilter(malicious)
	if strings.Contains(escaped, "(") || strings.Contains(escaped, ")") || strings.Contains(escaped, "*") {
		t.Fatalf("EscapeFilter 未妥善跳脫特殊字元: %s", escaped)
	}
}
