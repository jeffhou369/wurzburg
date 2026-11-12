package test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/jeffhou369/wurzburg/backend/internal/testutil"
)

func TestReviewBatchReassignReportsWriteFailure(t *testing.T) {
	f := newReviewFixture(t)
	sub := f.seed.SeedSubnet("10.43.0.0/24", "managed", nil)
	owner := f.seed.SeedUser("original-owner", "user", "ad", "original@example.test", "原擁有者")
	f.seed.SeedUser("target-owner", "user", "ad", "target@example.test", "新擁有者")
	a := f.seed.SeedIPRecord("10.43.0.10", "00:11:22:33:44:10", sub, &testutil.IPRecordSeedOpts{Registration: "registered", OwnerUserID: &owner, OwnerMail: "original@example.test"})
	b := f.seed.SeedIPRecord("10.43.0.11", "00:11:22:33:44:11", sub, &testutil.IPRecordSeedOpts{Registration: "registered", OwnerUserID: &owner, OwnerMail: "original@example.test"})
	_, err := f.db.Exec(`CREATE TRIGGER review_fail_reassign BEFORE UPDATE ON ip_records FOR EACH ROW BEGIN IF OLD.ip_address = '10.43.0.10' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = '測試改派失敗'; END IF; END`)
	if err != nil {
		t.Fatal(err)
	}
	w := f.request(http.MethodPost, "/api/v1/ip-records/actions/reassign", map[string]any{"ip_record_ids": []int64{a, b}, "target_username": "target-owner"}, f.adminID, "admin")
	if w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	var batch struct {
		Succeeded int `json:"succeeded_count"`
		Skipped   int `json:"skipped_count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &batch); err != nil {
		t.Fatal(err)
	}
	if batch.Succeeded != 1 || batch.Skipped != 1 {
		t.Fatalf("一筆更新失敗應回報一成功一略過：%s", w.Body.String())
	}
	w = f.request(http.MethodGet, "/api/v1/ip-records?search=10.43.0.10", nil, f.adminID, "admin")
	if !strings.Contains(w.Body.String(), "original-owner") {
		t.Fatal("失敗紀錄的擁有者必須保留")
	}
	w = f.request(http.MethodGet, "/api/v1/ip-records?search=10.43.0.11", nil, f.adminID, "admin")
	if !strings.Contains(w.Body.String(), "target-owner") {
		t.Fatal("其他紀錄仍應成功改派")
	}
	w = f.request(http.MethodGet, "/api/v1/audit/logs", nil, f.adminID, "admin")
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "10.43.0.10") || !strings.Contains(w.Body.String(), "10.43.0.11") {
		t.Fatalf("成功稽核只能包含實際成功的紀錄：%s", w.Body.String())
	}
	if _, err := f.db.Exec(`DROP TRIGGER review_fail_reassign`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`CREATE TRIGGER review_fail_reassign_audit BEFORE INSERT ON audit_logs FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = '測試稽核失敗'`); err != nil {
		t.Fatal(err)
	}
	w = f.request(http.MethodPost, "/api/v1/ip-records/actions/reassign", map[string]any{"ip_record_ids": []int64{a}, "target_username": "target-owner"}, f.adminID, "admin")
	if err := json.Unmarshal(w.Body.Bytes(), &batch); err != nil {
		t.Fatal(err)
	}
	if batch.Succeeded != 0 || batch.Skipped != 1 {
		t.Fatalf("稽核失敗不得提交改派：%s", w.Body.String())
	}
	w = f.request(http.MethodGet, "/api/v1/ip-records?search=10.43.0.10", nil, f.adminID, "admin")
	if !strings.Contains(w.Body.String(), "original-owner") {
		t.Fatal("稽核失敗時擁有者異動也必須回滾")
	}
}

func TestReviewBatchConfirmAndReleaseReportWriteFailure(t *testing.T) {
	f := newReviewFixture(t)
	sub := f.seed.SeedSubnet("10.47.0.0/24", "managed", nil)
	owner := f.seed.SeedUser("batch-owner", "user", "ad", "owner@example.test", "擁有者")
	expiry := f.clk.Now().AddDate(0, 0, 3)
	a := f.seed.SeedIPRecord("10.47.0.10", "00:11:22:33:44:10", sub, &testutil.IPRecordSeedOpts{Registration: "registered", OwnerUserID: &owner, OwnerMail: "owner@example.test", ExpiresAt: &expiry})
	b := f.seed.SeedIPRecord("10.47.0.11", "00:11:22:33:44:11", sub, &testutil.IPRecordSeedOpts{Registration: "registered", OwnerUserID: &owner, OwnerMail: "owner@example.test", ExpiresAt: &expiry})
	if _, err := f.db.Exec(`CREATE TRIGGER review_fail_batch BEFORE UPDATE ON ip_records FOR EACH ROW BEGIN IF OLD.ip_address = '10.47.0.10' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = '測試寫入失敗'; END IF; END`); err != nil {
		t.Fatal(err)
	}
	var batch struct {
		Succeeded int `json:"succeeded_count"`
		Skipped   int `json:"skipped_count"`
	}
	for _, action := range []string{"confirm", "release"} {
		w := f.request(http.MethodPost, "/api/v1/ip-records/actions/"+action, map[string]any{"ip_record_ids": []int64{a, b}}, owner, "user")
		if w.Code != http.StatusOK {
			t.Fatalf("%s 失敗：%d %s", action, w.Code, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &batch); err != nil {
			t.Fatal(err)
		}
		// 10.47.0.11 每一步都應成功（釋放前已成功重新確認），10.47.0.10 每一步都應因寫入失敗略過。
		if batch.Succeeded != 1 || batch.Skipped != 1 {
			t.Fatalf("%s：寫入失敗的一筆必須回報略過，實際 %s", action, w.Body.String())
		}
	}
	w := f.request(http.MethodGet, "/api/v1/ip-records?search=10.47.0.10", nil, f.adminID, "admin")
	if !strings.Contains(w.Body.String(), "batch-owner") {
		t.Fatal("寫入失敗的紀錄必須保留原登記")
	}
	w = f.request(http.MethodGet, "/api/v1/audit/logs", nil, f.adminID, "admin")
	if strings.Contains(w.Body.String(), "10.47.0.10") || !strings.Contains(w.Body.String(), "10.47.0.11") {
		t.Fatalf("成功稽核只能包含實際成功的紀錄：%s", w.Body.String())
	}
}
