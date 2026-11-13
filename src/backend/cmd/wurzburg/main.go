package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/accounts"
	"github.com/jeffhou369/wurzburg/backend/internal/app"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/config"
	"github.com/jeffhou369/wurzburg/backend/internal/database"
	"github.com/jeffhou369/wurzburg/backend/internal/server"
	"github.com/jeffhou369/wurzburg/backend/migrations"
)

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "reset-password" {
		if err := runResetPassword(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "重設密碼失敗: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("密碼重設完成。")
		return
	}

	if err := run(); err != nil {
		slog.Error("伺服器終止失敗", "error", err)
		os.Exit(1)
	}
}

func runResetPassword(args []string) error {
	if len(args) != 2 {
		return errors.New("用法: wurzburg reset-password <username> <new_password>")
	}
	username, newPassword := args[0], args[1]

	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	db, err := database.Connect(ctx, cfg.DSN())
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	return accounts.ResetAdminPassword(ctx, db, clock.NewRealClock(), username, newPassword, 12)
}

func run() error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return fmt.Errorf("載入環境變數設定失敗: %w", err)
	}

	var logHandler slog.Handler
	if cfg.AppEnv == "development" {
		logHandler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	} else {
		logHandler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	}
	slog.SetDefault(slog.New(logHandler))

	slog.Info("正在啟動 Würzburg IP 管理系統...",
		"port", cfg.Port,
		"campus", cfg.CampusName,
		"env", cfg.AppEnv,
	)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	clk := clock.NewRealClock()

	var db *sql.DB
	db, err = database.Connect(ctx, cfg.DSN())
	if err != nil {
		slog.Warn("目前無法連線至 MariaDB，稍後將於背景重試，伺服器先以降級模式啟動", "error", err)
	} else {
		defer func() { _ = db.Close() }()
		if err := database.RunMigrations(db, migrations.FS, "."); err != nil {
			return fmt.Errorf("套用資料庫遷移失敗: %w", err)
		}
		if err := accounts.EnsureFirstAdmin(ctx, db, clk, cfg.AdminUsername, cfg.AdminPassword); err != nil {
			return fmt.Errorf("建立初始管理員失敗: %w", err)
		}
	}

	srv := server.New(cfg, db, clk)
	modules := app.Mount(ctx, srv, db, clk)
	if db != nil {
		modules.StartBackgroundLoops(ctx)
	}

	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           srv.Router(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("HTTP 伺服器已就緒並開始接收請求", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("HTTP 伺服器異常終止: %w", err)
	case <-ctx.Done():
		slog.Info("接收到停止信號，正在優雅關閉伺服器...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("伺服器優雅關閉超時: %w", err)
		}
		slog.Info("Würzburg 伺服器已正常停止")
	}

	return nil
}
