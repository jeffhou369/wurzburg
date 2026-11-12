package server

import (
	"context"
	"testing"

	"github.com/jeffhou369/wurzburg/backend/internal/config"
)

func TestDeploymentDefaultsInitializeCampusSettings(t *testing.T) {
	srv := New(&config.Config{
		AppEnv:     "test",
		CampusName: "Example Campus",
		Timezone:   "America/New_York",
	}, nil, nil)

	for key, want := range map[string]string{
		"campus_name": "Example Campus",
		"timezone":    "America/New_York",
	} {
		got, err := srv.Settings.Get(context.Background(), key)
		if err != nil {
			t.Fatalf("讀取 %s 失敗：%v", key, err)
		}
		if got != want {
			t.Errorf("%s = %q，預期 %q", key, got, want)
		}
	}
}
