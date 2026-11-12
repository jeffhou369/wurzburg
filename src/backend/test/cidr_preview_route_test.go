package test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jeffhou369/wurzburg/backend/internal/subnets"
	"github.com/jeffhou369/wurzburg/backend/internal/testutil"
)

func TestReviewCIDRPreviewIsReachableThroughRegisteredRoutes(t *testing.T) {
	f := newReviewFixture(t)
	f.srv.Mount(subnets.NewScopeService(f.db, f.clk, f.srv.Audit, ""))
	sub := f.seed.SeedSubnet("10.45.0.0/24", "managed", nil)
	f.seed.SeedIPRecord("10.45.0.200", "00:11:22:33:44:55", sub, nil)
	path := fmt.Sprintf("/api/v1/subnets/%d", sub)
	w := f.request(http.MethodPost, path+"/cidr/preview", map[string]string{"cidr": "10.45.0.0/25"}, f.adminID, "admin")
	if w.Code != http.StatusOK {
		t.Fatalf("正式註冊路由必須提供CIDR預覽：%d %s", w.Code, w.Body.String())
	}
	var preview struct {
		Token string `json:"preview_token"`
		Count int    `json:"total_ip_records"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatalf("預覽必須回傳JSON：%v", err)
	}
	if preview.Token == "" || preview.Count != 1 {
		t.Fatalf("應預覽一筆刪除並提供確認憑證：%s", w.Body.String())
	}
	w = f.request(http.MethodPut, path, map[string]string{"cidr": "10.45.0.0/25", "description": "修正範圍", "preview_token": preview.Token}, f.adminID, "admin")
	if w.Code != http.StatusOK {
		t.Fatalf("有效預覽應能執行CIDR修正：%d %s", w.Code, w.Body.String())
	}
}

func TestReviewCIDRChangeKeepsRecordsCoveredByAnotherManagedSubnet(t *testing.T) {
	f := newReviewFixture(t)
	f.srv.Mount(subnets.NewScopeService(f.db, f.clk, f.srv.Audit, ""))
	f.seed.SeedSubnet("10.46.0.0/16", "managed", nil)
	sub := f.seed.SeedSubnet("10.46.1.0/24", "managed", nil)
	owner := f.seed.SeedUser("cidr-owner", "user", "ad", "owner@example.test", "擁有者")
	f.seed.SeedIPRecord("10.46.1.200", "00:11:22:33:44:66", sub, &testutil.IPRecordSeedOpts{Registration: "registered", OwnerUserID: &owner, OwnerMail: "owner@example.test"})
	path := fmt.Sprintf("/api/v1/subnets/%d", sub)

	w := f.request(http.MethodPost, path+"/cidr/preview", map[string]string{"cidr": "10.46.1.0/25"}, f.adminID, "admin")
	if w.Code != http.StatusOK {
		t.Fatalf("CIDR 預覽失敗：%d %s", w.Code, w.Body.String())
	}
	var preview struct {
		Count   int  `json:"total_ip_records"`
		Confirm bool `json:"requires_confirm"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Count != 0 || preview.Confirm {
		t.Fatalf("仍被其他納管網段涵蓋的位址不應列入刪除：%s", w.Body.String())
	}

	w = f.request(http.MethodPut, path, map[string]string{"cidr": "10.46.1.0/25", "description": "縮小範圍"}, f.adminID, "admin")
	if w.Code != http.StatusOK {
		t.Fatalf("無需刪除時不應要求確認：%d %s", w.Code, w.Body.String())
	}
	w = f.request(http.MethodGet, "/api/v1/ip-records?search=10.46.1.200", nil, f.adminID, "admin")
	if !strings.Contains(w.Body.String(), `"subnet_cidr":"10.46.0.0/16"`) || !strings.Contains(w.Body.String(), "cidr-owner") {
		t.Fatalf("位址應改歸屬涵蓋它的納管網段並保留登記：%s", w.Body.String())
	}
}
