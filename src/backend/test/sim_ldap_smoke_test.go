package test

import (
	"context"
	"errors"
	"testing"

	"github.com/jeffhou369/wurzburg/backend/internal/authsource"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
)

// TestLDAPSimulatorWithRealADClient 確認真正的 AD 驗證模組能對 LDAP 模擬伺服器完成整個驗證流程。
func TestLDAPSimulatorWithRealADClient(t *testing.T) {
	if testing.Short() {
		t.Skip("略過需啟動 Docker 容器的整合測試")
	}
	ctx := context.Background()
	sim := startLDAPSim(t, []ldapSimUser{
		{Username: "user01", Password: "SimUserPass#01", DisplayName: "王工程師", Mail: "user01@campus.test", InGroup: true},
		{Username: "outsider", Password: "SimUserPass#99", DisplayName: "外部人員", Mail: "outsider@campus.test", InGroup: false},
	})
	client := authsource.NewADClient(authsource.ADConfig{
		URL: sim.url, UseTLS: false,
		BindDN: ldapSimBindDN, BindPassword: ldapSimBindPassword,
		BaseDN: ldapSimBaseDN, AllowedGroup: ldapSimAllowedGroup,
	})

	info, err := client.Authenticate(ctx, "user01", "SimUserPass#01")
	if err != nil {
		t.Fatalf("正確的帳密應驗證成功: %v", err)
	}
	if info.DisplayName != "王工程師" || info.Mail != "user01@campus.test" || info.AuthSource != "ad" {
		t.Errorf("取得的帳號資訊不符：%+v", info)
	}

	if _, err := client.Authenticate(ctx, "user01", "WrongUserPass#01"); !errors.Is(err, interfaces.ErrInvalidCredentials) {
		t.Errorf("密碼錯誤應回 ErrInvalidCredentials，實際 %v", err)
	}
	if _, err := client.Authenticate(ctx, "nobody", "SimUserPass#01"); !errors.Is(err, interfaces.ErrInvalidCredentials) {
		t.Errorf("帳號不存在應回 ErrInvalidCredentials，實際 %v", err)
	}
	if _, err := client.Authenticate(ctx, "outsider", "SimUserPass#99"); !errors.Is(err, interfaces.ErrGroupRestricted) {
		t.Errorf("不在允許群組應回 ErrGroupRestricted，實際 %v", err)
	}

	wrongBind := authsource.NewADClient(authsource.ADConfig{
		URL: sim.url, BindDN: ldapSimBindDN, BindPassword: "WrongBindPass#41", BaseDN: ldapSimBaseDN,
	})
	if _, err := wrongBind.Authenticate(ctx, "user01", "SimUserPass#01"); err == nil {
		t.Error("服務帳號密碼錯誤時應驗證失敗")
	}

	sim.stop(t)
	if _, err := client.Authenticate(ctx, "user01", "SimUserPass#01"); !errors.Is(err, interfaces.ErrAuthUnavailable) {
		t.Errorf("伺服器中斷應回 ErrAuthUnavailable，實際 %v", err)
	}
}
