package test

import (
	"context"
	"net/http"
	"testing"
)

func TestReviewMissingPublicURLStopsMail(t *testing.T) {
	f := newReviewFixture(t)
	w := f.request(http.MethodPost, "/api/v1/settings/mail/test", map[string]string{"recipient": "verify@example.test"}, f.adminID, "admin")
	if w.Code != http.StatusUnprocessableEntity || f.mail.Count() != 0 {
		t.Fatalf("未設公開網址必須停止寄送並提示：%d %s", w.Code, w.Body.String())
	}
	ctx := context.Background()
	if err := f.srv.Settings.Set(ctx, "admin_recipients", `["admin@example.test"]`); err != nil {
		t.Fatal(err)
	}
	sub := f.seed.SeedSubnet("10.44.0.0/24", "managed", nil)
	f.seed.SeedIPRecord("10.44.0.10", "00:11:22:33:44:10", sub, nil)
	if _, err := f.notifications.RunUnregisteredDigest(ctx); err == nil || f.mail.Count() != 0 {
		t.Fatal("未設公開網址不得發送包含本機網址的通知")
	}
}
