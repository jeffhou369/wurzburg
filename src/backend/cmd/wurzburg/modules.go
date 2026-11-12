package main

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
	"github.com/jeffhou369/wurzburg/backend/internal/oui"
	"github.com/jeffhou369/wurzburg/backend/internal/pipeline"
	"github.com/jeffhou369/wurzburg/backend/internal/purge"
	"github.com/jeffhou369/wurzburg/backend/internal/reconciliation"
	"github.com/jeffhou369/wurzburg/backend/internal/scheduler"
	"github.com/jeffhou369/wurzburg/backend/internal/server"
	"github.com/jeffhou369/wurzburg/backend/internal/subnets"
	"github.com/jeffhou369/wurzburg/backend/internal/switches"
)

// mountFeatureModules 集中掛載所有業務功能模組之路由並啟動排程。
func mountFeatureModules(ctx context.Context, srv *server.Server, db *sql.DB, clk clock.Clock) {
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
	)

	if db != nil {
		go sched.StartBackgroundLoop(ctx)
	}
}
