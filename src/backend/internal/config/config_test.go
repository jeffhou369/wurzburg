package config

import (
	"net/netip"
	"reflect"
	"testing"
)

func TestParseTrustedProxies(t *testing.T) {
	got, err := ParseTrustedProxies(" 172.20.0.0/16 , 10.0.0.5 ,,")
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.Prefix{netip.MustParsePrefix("172.20.0.0/16"), netip.MustParsePrefix("10.0.0.5/32")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("得到 %v，預期 %v", got, want)
	}
	if got, err := ParseTrustedProxies(""); err != nil || got != nil {
		t.Errorf("空白設定應為不信任任何代理，得到 %v, %v", got, err)
	}
	for _, bad := range []string{"not-an-ip", "172.20.0.0/40", "2001:db8::/32"} {
		if _, err := ParseTrustedProxies(bad); err == nil {
			t.Errorf("%q 應回報設定錯誤", bad)
		}
	}
}
