package purge

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/clock"
)

// MaintenanceScheduler 在獨立背景工作清除未登記離線紀錄，不等待 SMTP 或採集。
// Tick 與正式背景迴圈共用到期判斷，僅成功後標記該園區日期已完成。
type MaintenanceScheduler struct {
	service *Service
	clock   clock.Clock
	mu      sync.Mutex
	lastDay string
}

func NewMaintenanceScheduler(service *Service, clk clock.Clock) *MaintenanceScheduler {
	if clk == nil {
		clk = clock.NewRealClock()
	}
	return &MaintenanceScheduler{service: service, clock: clk}
}

func (s *MaintenanceScheduler) Tick(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.service == nil {
		return nil
	}
	tz, err := s.service.settings.Get(ctx, "timezone")
	if err != nil {
		return err
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return err
	}
	day := s.clock.Now().In(loc).Format("2006-01-02")
	if s.lastDay == day {
		return nil
	}
	if _, err := s.service.RunAutoPurge(ctx); err != nil {
		return err
	}
	s.lastDay = day
	return nil
}

func (s *MaintenanceScheduler) StartBackgroundLoop(ctx context.Context) {
	// 啟動時即補做今日維護；失敗時由下一次 tick 重試。
	run := func() {
		if err := s.Tick(ctx); err != nil && ctx.Err() == nil {
			slog.WarnContext(ctx, "自動清除失敗，將於下次維護檢查重試", "error", err)
		}
	}
	run()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
