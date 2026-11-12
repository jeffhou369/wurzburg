package main

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/jeffhou369/wurzburg/backend/internal/accounts"
	"github.com/jeffhou369/wurzburg/backend/internal/audit"
	"github.com/jeffhou369/wurzburg/backend/internal/authsource"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/oui"
	"github.com/jeffhou369/wurzburg/backend/internal/purge"
	"github.com/jeffhou369/wurzburg/backend/internal/server"
)

// mountFeatureModules 集中掛載所有業務功能模組之路由。
func mountFeatureModules(srv *server.Server, db *sql.DB, clk clock.Clock) {
	ouiSvc := oui.NewService(db, clk, srv.Audit)
	if err := ouiSvc.EnsureLoaded(context.Background()); err != nil {
		slog.Warn("初始化本地 OUI 資料庫失敗", "error", err)
	}

	authSrcMgr := authsource.NewDynamicManager(db, srv.Settings, srv.Audit)
	accountsHandler := accounts.NewHandler(db, clk, srv.Sessions, srv.Settings, srv.Audit, authSrcMgr)
	auditHandler := audit.NewHandler(db)
	purgeSvc := purge.NewService(db, clk, srv.Settings, srv.Audit)

	srv.Mount(accountsHandler, authSrcMgr, auditHandler, ouiSvc, purgeSvc)
}
