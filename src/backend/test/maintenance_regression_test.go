package test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/jeffhou369/wurzburg/backend/internal/purge"
	"github.com/jeffhou369/wurzburg/backend/internal/testutil"
)

func TestReviewMaintenanceRunsDueCleanupAndRetriesFailure(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	sub := f.seed.SeedSubnet("10.42.0.0/24", "managed", nil)
	old := f.clk.Now().AddDate(0, 0, -91)
	f.seed.SeedIPRecord("10.42.0.10", "00:11:22:33:44:10", sub, &testutil.IPRecordSeedOpts{Presence: "offline", LastSeenAt: &old})
	owner := f.seed.SeedUser("retained-owner", "user", "ad", "owner@example.test", "擁有者")
	f.seed.SeedIPRecord("10.42.0.11", "00:11:22:33:44:11", sub, &testutil.IPRecordSeedOpts{Presence: "offline", LastSeenAt: &old, Registration: "registered", OwnerUserID: &owner})
	service := purge.NewService(f.db, f.clk, f.srv.Settings, f.srv.Audit)
	runner := purge.NewMaintenanceScheduler(service, f.clk)
	// 模擬資料庫暫時失敗；失敗不得將今天標記成已完成。
	if _, err := f.db.Exec(`CREATE TRIGGER review_fail_purge BEFORE DELETE ON ip_records FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = '測試刪除失敗'`); err != nil {
		t.Fatal(err)
	}
	if err := runner.Tick(ctx); err == nil {
		t.Fatal("維護工作必須回報清除失敗")
	}
	if _, err := f.db.Exec(`DROP TRIGGER review_fail_purge`); err != nil {
		t.Fatal(err)
	}
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	w := f.request(http.MethodGet, "/api/v1/ip-records", nil, f.adminID, "admin")
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "10.42.0.10") || !strings.Contains(w.Body.String(), "10.42.0.11") {
		t.Fatalf("到期維護必須自動清除未登記離線紀錄、保留有擁有者紀錄：%s", w.Body.String())
	}
	// 當日成功後不應再次清除新加入的紀錄；隔日重新執行。
	f.seed.SeedIPRecord("10.42.0.12", "00:11:22:33:44:12", sub, &testutil.IPRecordSeedOpts{Presence: "offline", LastSeenAt: &old})
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	w = f.request(http.MethodGet, "/api/v1/ip-records", nil, f.adminID, "admin")
	if !strings.Contains(w.Body.String(), "10.42.0.12") {
		t.Fatal("同日不應重複執行成功的維護工作")
	}
	f.clk.Set(f.clk.Now().AddDate(0, 0, 1))
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	w = f.request(http.MethodGet, "/api/v1/ip-records", nil, f.adminID, "admin")
	if strings.Contains(w.Body.String(), "10.42.0.12") {
		t.Fatal("隔日應重新執行到期維護")
	}
}
