package oui

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestExamplePrefixesRemainTestOnly(t *testing.T) {
	fixture, err := os.ReadFile("testdata/oui-snapshot.csv")
	if err != nil {
		t.Fatal(err)
	}
	testEntries, err := ParseOUI(strings.NewReader(string(fixture)))
	if err != nil {
		t.Fatal(err)
	}
	if len(testEntries) != 2 {
		t.Fatalf("測試資料應包含兩筆範例前綴，得到 %d 筆", len(testEntries))
	}

	service := NewService(nil, nil, nil)
	if err := service.EnsureLoaded(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := service.Lookup("70:B3:D5:12:34:56"); got != "未知" {
		t.Fatalf("正式內建快照不應採用虛構長前綴，查詢結果為 %q", got)
	}
}
