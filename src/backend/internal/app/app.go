// Package app 組裝 Würzburg 的全部功能模組。
//
// 正式執行檔（cmd/wurzburg）與端到端測試使用同一份組裝：真正的 SNMP 採集、SMTP 寄信、AD／RADIUS 驗證，
// 差別只在時鐘與背景迴圈——正式環境以實際時間驅動背景迴圈，測試則推進測試時鐘後直接觸發到期工作。
package app

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/jeffhou369/wurzburg/backend/internal/accounts"
	"github.com/jeffhou369/wurzburg/backend/internal/audit"
	"github.com/jeffhou369/wurzburg/backend/internal/authsource"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/collector"
	"github.com/jeffhou369/wurzburg/backend/internal/conflicts"
	"github.com/jeffhou369/wurzburg/backend/internal/dashboard"
	"github.com/jeffhou369/wurzburg/backend/internal/iprecords"
	"github.com/jeffhou369/wurzburg/backend/internal/mailer"
	"github.com/jeffhou369/wurzburg/backend/internal/notifications"
	"github.com/jeffhou369/wurzburg/backend/internal/oui"
	"github.com/jeffhou369/wurzburg/backend/internal/pipeline"
	"github.com/jeffhou369/wurzburg/backend/internal/purge"
	"github.com/jeffhou369/wurzburg/backend/internal/reconciliation"
	"github.com/jeffhou369/wurzburg/backend/internal/scheduler"
	"github.com/jeffhou369/wurzburg/backend/internal/server"
	"github.com/jeffhou369/wurzburg/backend/internal/subnets"
	"github.com/jeffhou369/wurzburg/backend/internal/switches"
	"github.com/jeffhou369/wurzburg/backend/internal/wizard"
)

// Modules 是組裝完成的功能模組中，需要由外部驅動的排程入口。
type Modules struct {
	// Scheduler 的 RunCycle 執行一輪 SNMP 輪詢。
	Scheduler *scheduler.Scheduler
	// Notifications 的 Tick 依時鐘觸發到期的通知工作（未登記彙整、每週總表、到期催認）。
	Notifications *notifications.SchedulerTracker
	// Maintenance 的 Tick 依時鐘觸發每日維護（自動清除）。
	Maintenance *purge.MaintenanceScheduler
}

// Mount 建立所有業務功能模組並把路由掛載到 srv。
func Mount(ctx context.Context, srv *server.Server, db *sql.DB, clk clock.Clock) *Modules {
	ouiSvc := oui.NewService(db, clk, srv.Audit)
	if err := ouiSvc.EnsureLoaded(ctx); err != nil {
		slog.Warn("初始化本地 OUI 資料庫失敗", "error", err)
	}

	authSrcMgr := authsource.NewDynamicManager(db, srv.Settings, srv.Audit)
	accountsHandler := accounts.NewHandler(db, clk, srv.Sessions, srv.Settings, srv.Audit, authSrcMgr)
	auditHandler := audit.NewHandler(db)
	purgeSvc := purge.NewService(db, clk, srv.Settings, srv.Audit)

	scopeSvc := subnets.NewScopeService(db, clk, srv.Audit, "")
	resolver := subnets.NewResolver(db)
	reconcileSvc := reconciliation.NewService(db, clk, srv.Settings, resolver, ouiSvc)
	conflictsHandler := conflicts.NewHandler(db, clk, srv.Audit, ouiSvc)

	snmpCol := collector.NewSNMPCollector(clk)
	switchesHandler := switches.NewHandler(db, clk, srv.Cipher, srv.Audit, snmpCol)
	pipe := pipeline.New(scopeSvc, reconcileSvc)
	sched := scheduler.New(db, clk, srv.Cipher, srv.Settings, srv.Audit, snmpCol, pipe)
	dashHandler := dashboard.NewHandler(db, clk, sched)
	ipHandler := iprecords.NewHandler(db, clk, srv.Settings, srv.Audit)

	smtpMailer := mailer.NewSMTPMailer(db, clk, srv.Settings)
	mailHandler := mailer.NewHandler(db, clk, srv.Settings, srv.Audit, smtpMailer)
	notifSvc := notifications.NewService(db, clk, srv.Settings, srv.Audit, smtpMailer)
	notifTracker := notifications.NewSchedulerTracker(notifSvc)
	wizardHandler := wizard.NewHandler(db, clk, srv.Settings, srv.Audit)

	srv.Mount(
		accountsHandler,
		authSrcMgr,
		auditHandler,
		ouiSvc,
		purgeSvc,
		switchesHandler,
		sched,
		scopeSvc,
		conflictsHandler,
		dashHandler,
		ipHandler,
		mailHandler,
		wizardHandler,
	)

	return &Modules{
		Scheduler:     sched,
		Notifications: notifTracker,
		Maintenance:   purge.NewMaintenanceScheduler(purgeSvc, clk),
	}
}

// StartBackgroundLoops 啟動以實際時間驅動的背景迴圈：每 3 分鐘一輪 SNMP 輪詢、通知排程與每日維護。
// 各迴圈在 ctx 結束時停止。
func (m *Modules) StartBackgroundLoops(ctx context.Context) {
	go m.Scheduler.StartBackgroundLoop(ctx)
	go m.Notifications.StartBackgroundLoop(ctx)
	go m.Maintenance.StartBackgroundLoop(ctx)
}
