package sqlutil

import (
	"reflect"
	"testing"
)

func TestPlaceholders(t *testing.T) {
	for n, want := range map[int]string{-1: "", 0: "", 1: "?", 3: "?,?,?"} {
		if got := Placeholders(n); got != want {
			t.Errorf("Placeholders(%d) = %q，預期 %q", n, got, want)
		}
	}
}

func TestArgs(t *testing.T) {
	if got := Args([]int64{1, 2}); !reflect.DeepEqual(got, []any{int64(1), int64(2)}) {
		t.Errorf("Args = %#v", got)
	}
}

func TestChunk(t *testing.T) {
	got := Chunk([]string{"a", "b", "c", "d", "e"}, 2)
	want := [][]string{{"a", "b"}, {"c", "d"}, {"e"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Chunk = %#v，預期 %#v", got, want)
	}
	if got := Chunk([]int64(nil), 3); got != nil {
		t.Errorf("空切片應回傳 nil，得到 %#v", got)
	}
}
