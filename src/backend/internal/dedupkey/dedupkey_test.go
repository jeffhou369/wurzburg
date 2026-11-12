package dedupkey

import "testing"

// 去重鍵已寫入正式資料庫，格式變動會讓既有「已通知」狀態失效，因此以固定字串鎖定格式。
func TestKeyFormatsAreStable(t *testing.T) {
	cases := map[string]string{
		Unregistered("10.0.0.5", "aa:bb:cc:dd:ee:ff"): "unreg|10.0.0.5|aa:bb:cc:dd:ee:ff",
		Weekly(2026, 7):                                  "weekly|2026-W07",
		Expiry("10.0.0.5", "2026-12-01", 2):              "expiry|10.0.0.5|2026-12-01|2",
		UnregisteredPattern("10.0.0.5"):                  "unreg|10.0.0.5|%",
		ExpiryPattern("10.0.0.5"):                        "expiry|10.0.0.5|%",
		UnregisteredPattern(`a%b_c\`):                    `unreg|a\%b\_c\\|%`,
		UnregisteredSQL("r.ip_address", "r.mac_address"): "CONCAT('unreg|', r.ip_address, '|', r.mac_address)",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("得到 %q，預期 %q", got, want)
		}
	}
}
