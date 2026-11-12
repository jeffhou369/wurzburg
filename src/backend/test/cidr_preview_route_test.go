package test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/jeffhou369/wurzburg/backend/internal/subnets"
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
