package test

import (
	"context"
	"testing"

	"github.com/jeffhou369/wurzburg/backend/internal/crypto"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
	"github.com/jeffhou369/wurzburg/backend/internal/pipeline"
	"github.com/jeffhou369/wurzburg/backend/internal/reconciliation"
	"github.com/jeffhou369/wurzburg/backend/internal/scheduler"
	"github.com/jeffhou369/wurzburg/backend/internal/subnets"
)

// 範圍步驟（網段發現）寫入資料庫失敗時，一輪輪詢必須回報失敗，不能被靜默吞掉。
func TestReviewRunCycleReportsRealScopeStepFailure(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	ciph, err := crypto.NewCipher("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	swID, vrfID := f.seed.SeedSwitch("scope-err-sw", "10.0.9.1", "ios-xe", "v2c")
	col := interfaces.NewFakeCollector()
	col.Outputs[vrfID] = &interfaces.PollOutput{
		VRFConfigID: vrfID, SwitchID: swID, VRFName: "default",
		InterfaceAddresses: []interfaces.InterfaceAddress{{IPAddress: "10.48.0.1", NetMask: "255.255.255.0", IfIndex: 1, IfName: "Vlan48"}},
		InterfaceNames:     map[int64]string{1: "Vlan48"},
	}
	scope := subnets.NewScopeService(f.db, f.clk, f.srv.Audit, "")
	recon := reconciliation.NewService(f.db, f.clk, f.srv.Settings, subnets.NewResolver(f.db), nil)
	sched := scheduler.New(f.db, f.clk, ciph, f.srv.Settings, f.srv.Audit, col, pipeline.New(scope, recon))

	if _, err := f.db.Exec(`CREATE TRIGGER review_fail_subnet_insert BEFORE INSERT ON subnets FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = '測試網段寫入失敗'`); err != nil {
		t.Fatal(err)
	}
	if err := sched.RunCycle(ctx); err == nil {
		t.Fatal("網段發現寫入失敗時 RunCycle 必須回傳錯誤")
	}
	if _, err := f.db.Exec(`DROP TRIGGER review_fail_subnet_insert`); err != nil {
		t.Fatal(err)
	}
	if err := sched.RunCycle(ctx); err != nil {
		t.Fatalf("故障排除後應恢復正常：%v", err)
	}
}
