package server

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/config"
	"github.com/jeffhou369/wurzburg/backend/internal/static"
)

// Server 封裝 HTTP 路由、依賴項目與伺服器設定。
type Server struct {
	cfg    *config.Config
	db     *sql.DB
	clock  clock.Clock
	router chi.Router
}

// New 建立並配置一個新的 Server 實例。
func New(cfg *config.Config, db *sql.DB, clk clock.Clock) *Server {
	if clk == nil {
		clk = clock.NewRealClock()
	}

	s := &Server{
		cfg:   cfg,
		db:    db,
		clock: clk,
	}

	s.setupRoutes()
	return s
}

// Router 回傳配置完成的 chi 路由實例。
func (s *Server) Router() http.Handler {
	return s.router
}

func (s *Server) setupRoutes() {
	r := chi.NewRouter()

	// 基礎中介軟體
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	// 健康檢查端點（不需身分驗證，不含機敏資料）
	r.Get("/healthz", s.handleHealthz)
	r.Get("/api/v1/health", s.handleDetailedHealth)

	// API 路由群組
	r.Route("/api/v1", func(api chi.Router) {
		api.Get("/health", s.handleDetailedHealth)
	})

	// 前端靜態頁面與 Single Page Application 萬用回退路由
	r.NotFound(static.SPAHandler().ServeHTTP)

	s.router = r
}
