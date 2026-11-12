package accounts

import (
	"sync"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/clock"
)

// RateLimiter 提供簡單的滑動視窗登入速率限制，防止暴力猜測密碼。
type RateLimiter struct {
	mu       sync.Mutex
	clock    clock.Clock
	maxHits  int
	window   time.Duration
	attempts map[string][]time.Time
}

func NewRateLimiter(clk clock.Clock, maxHits int, window time.Duration) *RateLimiter {
	if clk == nil {
		clk = clock.NewRealClock()
	}
	return &RateLimiter{
		clock:    clk,
		maxHits:  maxHits,
		window:   window,
		attempts: make(map[string][]time.Time),
	}
}

// Allow 檢查指定來源鍵（例如 IP）是否仍允許進行登入嘗試。
func (r *RateLimiter) Allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.clock.Now()
	cutoff := now.Add(-r.window)

	var recent []time.Time
	for _, t := range r.attempts[key] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}

	if len(recent) >= r.maxHits {
		r.attempts[key] = recent
		return false
	}

	recent = append(recent, now)
	r.attempts[key] = recent
	return true
}
