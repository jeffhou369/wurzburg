package test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jeffhou369/wurzburg/backend/internal/testutil"
)

func TestReviewPendingConfirmationUsesAccountAndIncludesExpired(t *testing.T) {
	f := newReviewFixture(t)
	sub := f.seed.SeedSubnet("10.41.0.0/24", "managed", nil)
	owner := f.seed.SeedUser("pending-owner", "user", "ad", "shared@example.test", "擁有者")
	other := f.seed.SeedUser("pending-other", "user", "ad", "shared@example.test", "其他帳號")
	for i, days := range []int{-1, 7, 90} {
		exp := f.clk.Now().AddDate(0, 0, days)
		ip := []string{"10.41.0.10", "10.41.0.11", "10.41.0.12"}[i]
		f.seed.SeedIPRecord(ip, "00:11:22:33:44:10", sub, &testutil.IPRecordSeedOpts{Registration: "registered", OwnerUserID: &owner, OwnerMail: "shared@example.test", ExpiresAt: &exp})
	}
	exp := f.clk.Now().AddDate(0, 0, -1)
	f.seed.SeedIPRecord("10.41.0.20", "00:11:22:33:44:20", sub, &testutil.IPRecordSeedOpts{Registration: "registered", OwnerUserID: &other, OwnerMail: "shared@example.test", ExpiresAt: &exp})
	w := f.request(http.MethodGet, "/api/v1/ip-records?owner_me=true&pending_confirmation=true", nil, owner, "user")
	if w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	var response struct {
		Items []struct {
			IP string `json:"ip_address"`
		}
		Pagination struct {
			Total int `json:"total_items"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Pagination.Total != 2 || len(response.Items) != 2 || response.Items[0].IP != "10.41.0.10" || response.Items[1].IP != "10.41.0.11" {
		t.Fatalf("待確認清單應包含自己的已到期/即將到期，不含未到期或共用信箱的其他帳號：%s", w.Body.String())
	}
}
