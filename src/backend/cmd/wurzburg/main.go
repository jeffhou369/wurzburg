package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/config"
	"github.com/jeffhou369/wurzburg/backend/internal/database"
	"github.com/jeffhou369/wurzburg/backend/internal/server"
	"github.com/jeffhou369/wurzburg/backend/migrations"
)

func main() {
	if err := run(); err != nil {
		slog.Error("伺服器終止失敗", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return fmt.Errorf("載入環境變數設定失敗: %w", err)
	}

	// 初始化結構化日誌（輸出至 stdout）
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

	// 連線至資料庫
	db, err := database.Connect(ctx, cfg.DSN())
	if err != nil {
		slog.Warn("目前無法連線至 MariaDB，稍後將於背景重試，伺服器先以降級模式啟動", "error", err)
	} else {
		defer db.Close()
		// 套用資料庫遷移
		if err := database.RunMigrations(db, migrations.FS, "."); err != nil {
			return fmt.Errorf("套用資料庫遷移失敗: %w", err)
		}
	}

	clk := clock.NewRealClock()
	srv := server.New(cfg, db, clk)

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
