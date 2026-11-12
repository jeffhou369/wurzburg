package notifications

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

type parsedCron struct {
	minute int
	hour   int
	dow    time.Weekday // 0=Sunday .. 6=Saturday
	anyDOW bool
}

// ParseWeeklyCron 解析每週定時設定 (例如 "0 9 * * 1" 代表週一 09:00)。
func ParseWeeklyCron(expr string) (parsedCron, error) {
	fields := strings.Fields(strings.TrimSpace(expr))
	if len(fields) != 5 {
		return parsedCron{}, fmt.Errorf("無效的 Cron 格式（必須為 5 個欄位，如 0 9 * * 1）")
	}
	minVal, err := strconv.Atoi(fields[0])
	if err != nil || minVal < 0 || minVal > 59 {
		return parsedCron{}, fmt.Errorf("無效的分鐘欄位: %s", fields[0])
	}
	hourVal, err := strconv.Atoi(fields[1])
	if err != nil || hourVal < 0 || hourVal > 23 {
		return parsedCron{}, fmt.Errorf("無效的小時欄位: %s", fields[1])
	}

	var dow time.Weekday
	anyDOW := false
	if fields[4] == "*" {
		anyDOW = true
	} else {
		dVal, err := strconv.Atoi(fields[4])
		if err != nil || dVal < 0 || dVal > 7 {
			return parsedCron{}, fmt.Errorf("無效的星期欄位 (0-7): %s", fields[4])
		}
		if dVal == 7 {
			dVal = 0
		}
		dow = time.Weekday(dVal)
	}

	return parsedCron{
		minute: minVal,
		hour:   hourVal,
		dow:    dow,
		anyDOW: anyDOW,
	}, nil
}

// SchedulerTracker 追蹤各通知任務上次執行時間點並依時鐘觸發到期任務。
type SchedulerTracker struct {
	svc           *Service
	mu            sync.Mutex
	lastDigestHr  string
	lastExpiryDay string
}

func NewSchedulerTracker(svc *Service) *SchedulerTracker {
	return &SchedulerTracker{svc: svc}
}

// Tick 依目前時鐘檢查並觸發所有到期的通知工作與日誌清理（供背景迴圈及測試控制時鐘呼叫）。
func (t *SchedulerTracker) Tick(ctx context.Context) {
	if t.svc == nil {
		return
	}
	loc, _ := t.svc.getLocation(ctx)
	nowLocal := t.svc.clock.Now().In(loc)

	t.mu.Lock()
	curHr := nowLocal.Format("2006-01-02 15")
	runDigest := nowLocal.Minute() < 2 && t.lastDigestHr != curHr
	if runDigest {
		t.lastDigestHr = curHr
	}

	curDay := nowLocal.Format("2006-01-02")
	runDaily := nowLocal.Hour() >= 8 && t.lastExpiryDay != curDay
	if runDaily {
		t.lastExpiryDay = curDay
	}
	t.mu.Unlock()

	if runDigest {
		_, _ = t.svc.RunUnregisteredDigest(ctx)
	}

	// 檢查每週總表排程
	cronExpr := "0 9 * * 1"
	if t.svc.settings != nil {
		if val, err := t.svc.settings.Get(ctx, "weekly_summary_cron"); err == nil && val != "" {
			cronExpr = val
		}
	}
	if pc, err := ParseWeeklyCron(cronExpr); err == nil {
		if pc.anyDOW || nowLocal.Weekday() == pc.dow {
			scheduled := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), pc.hour, pc.minute, 0, 0, loc)
			if !nowLocal.Before(scheduled) && nowLocal.Sub(scheduled) < 15*time.Minute {
				_, _ = t.svc.RunWeeklySummary(ctx)
			}
		}
	}

	if runDaily {
		_, _ = t.svc.RunExpiryReminders(ctx)
		_, _ = t.svc.CleanupOldLogs(ctx)
	}
}

// StartBackgroundLoop 啟動背景每 30 秒檢查一次之通知排程迴圈。
func (t *SchedulerTracker) StartBackgroundLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.Tick(ctx)
		}
	}
}

// CleanupOldLogs 清除超過保留期（預設 1 年）之通知紀錄。
func (s *Service) CleanupOldLogs(ctx context.Context) (int64, error) {
	if s.db == nil {
		return 0, nil
	}
	days := 365
	if s.settings != nil {
		days = s.settings.GetInt(ctx, "notification_retention_days", 365)
	}
	if days <= 0 {
		return 0, nil
	}
	cutoff := s.clock.Now().AddDate(0, 0, -days)
	res, err := s.db.ExecContext(ctx, `DELETE FROM notification_logs WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
