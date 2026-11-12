package main

import (
	"database/sql"

	"github.com/jeffhou369/wurzburg/backend/internal/accounts"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/server"
)

// mountFeatureModules 集中掛載所有業務功能模組之路由。
func mountFeatureModules(srv *server.Server, db *sql.DB, clk clock.Clock) {
	accountsHandler := accounts.NewHandler(db, clk, srv.Sessions, srv.Settings, srv.Audit, nil)
	srv.Mount(accountsHandler)
}
