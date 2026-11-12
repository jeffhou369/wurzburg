package clock

import (
	"testing"
	"time"
)

func TestRealClock(t *testing.T) {
	c := NewRealClock()
	now1 := c.Now()
	if now1.IsZero() {
		t.Fatal("RealClock.Now() returned zero time")
	}
	if now1.Location() != time.UTC {
		t.Fatalf("expected UTC time, got %v", now1.Location())
	}
}

func TestTestClock(t *testing.T) {
	start := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	tc := NewTestClock(start)

	if !tc.Now().Equal(start) {
		t.Fatalf("expected %v, got %v", start, tc.Now())
	}

	tc.Advance(24 * time.Hour)
	expectedNext := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	if !tc.Now().Equal(expectedNext) {
		t.Fatalf("expected %v, got %v", expectedNext, tc.Now())
	}

	override := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	tc.Set(override)
	if !tc.Now().Equal(override) {
		t.Fatalf("expected %v, got %v", override, tc.Now())
	}
}
