package test

import (
	"context"
	"testing"

	"github.com/jeffhou369/wurzburg/backend/internal/accounts"
)

func TestResetAdminPasswordRevokesSessionsAtomically(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	otherAdminID := f.seed.SeedAdmin("other-admin")

	var adminTokens []string
	for range 2 {
		token, err := f.srv.Sessions.CreateSession(ctx, f.adminID, 8, "192.0.2.10", "test")
		if err != nil {
			t.Fatal(err)
		}
		adminTokens = append(adminTokens, token)
	}
	otherToken, err := f.srv.Sessions.CreateSession(ctx, otherAdminID, 8, "192.0.2.11", "test")
	if err != nil {
		t.Fatal(err)
	}

	if err := accounts.ResetAdminPassword(ctx, f.db, f.clk, "review-admin", useSecret(t, "本機帳號密碼", "new-secure-password"), 12); err != nil {
		t.Fatal(err)
	}
	for _, token := range adminTokens {
		if _, err := f.srv.Sessions.ValidateSession(ctx, token, 8); err == nil {
			t.Fatal("重設密碼後舊工作階段必須失效")
		}
	}
	if _, err := f.srv.Sessions.ValidateSession(ctx, otherToken, 8); err != nil {
		t.Fatalf("重設其他管理員密碼不得撤銷其工作階段：%v", err)
	}
}
