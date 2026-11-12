package clock

import (
	"sync"
	"time"
)

// Clock 定義系統所有與時間相關邏輯所依賴的介面。
type Clock interface {
	Now() time.Time
	Sleep(d time.Duration)
}

// realClock 是在生產環境使用的真實時鐘實作。
type realClock struct{}

// NewRealClock 建立並回傳一個基於系統時間的真實時鐘。
func NewRealClock() Clock {
	return &realClock{}
}

func (r *realClock) Now() time.Time {
	return time.Now().UTC()
}

func (r *realClock) Sleep(d time.Duration) {
	time.Sleep(d)
}

// TestClock 是供自動化測試使用的可控時鐘。
type TestClock struct {
	mu      sync.RWMutex
	current time.Time
}

// NewTestClock 建立以指定時間為起始點的測試時鐘。
func NewTestClock(initial time.Time) *TestClock {
	return &TestClock{
		current: initial.UTC(),
	}
}

// Now 回傳測試時鐘當前時間（UTC）。
func (c *TestClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current
}

// Sleep 在測試時鐘中為空實作，避免測試被無效等待拖慢。
func (c *TestClock) Sleep(d time.Duration) {
	c.Advance(d)
}

// Advance 將測試時鐘向前推進指定的時間長度。
func (c *TestClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = c.current.Add(d)
}

// Set 直接將測試時鐘設定至指定的時間。
func (c *TestClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = t.UTC()
}
