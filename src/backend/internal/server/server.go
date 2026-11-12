package server

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jeffhou369/wurzburg/backend/internal/audit"
	"github.com/jeffhou369/wurzburg/backend/internal/auth"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/config"
	"github.com/jeffhou369/wurzburg/backend/internal/crypto"
	"github.com/jeffhou369/wurzburg/backend/internal/settings"
	"github.com/jeffhou369/wurzburg/backend/internal/static"
)

// RouteRegistrar 定義各功能模組向核心 API 路由掛載自身端點之介面。
type RouteRegistrar interface {
	RegisterRoutes(api chi.Router)
}

// Server 封裝 HTTP 路由、共用基礎服務與伺服器設定。
type Server struct {
	cfg       *config.Config
	db        *sql.DB
	clock     clock.Clock
	router    chi.Router
	apiRouter chi.Router
	Sessions  *auth.SessionStore
	Settings  *settings.Store
	Audit     *audit.Logger
	Cipher    *crypto.Cipher
}

// Mount 將功能模組之路由掛載至 /api/v1 路由群組下。
func (s *Server) Mount(registrars ...RouteRegistrar) {
	for _, reg := range registrars {
		if reg != nil {
			reg.RegisterRoutes(s.apiRouter)
		}
	}
}

// New 建立並配置一個新的 Server 實例，並掛載傳入的功能路由。
func New(cfg *config.Config, db *sql.DB, clk clock.Clock, registrars ...RouteRegistrar) *Server {
	if clk == nil {
		clk = clock.NewRealClock()
	}

	var cipher *crypto.Cipher
	if cfg.SecretKey != "" {
		if c, err := crypto.NewCipher(cfg.SecretKey); err == nil {
			cipher = c
		}
	}

	s := &Server{
		cfg:      cfg,
		db:       db,
		clock:    clk,
		Sessions: auth.NewSessionStore(db, clk),
		Settings: settings.NewStore(db, cipher, clk),
		Audit:    audit.NewLogger(db, clk),
		Cipher:   cipher,
	}

	s.setupRoutes(registrars...)
	return s
}

// Router 回傳配置完成的 chi 路由實例。
func (s *Server) Router() http.Handler {
	return s.router
}

func (s *Server) setupRoutes(registrars ...RouteRegistrar) {
	r := chi.NewRouter()

	// 基礎中介軟體
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	// 健康檢查端點（不需身分驗證，不含機敏資料）
	r.Get("/healthz", s.handleHealthz)
	r.Get("/api/v1/health", s.handleDetailedHealth)

	allowTestHeaders := s.cfg.AppEnv == "test"

	// API 路由群組
	r.Route("/api/v1", func(api chi.Router) {
		s.apiRouter = api
		api.Use(auth.AuthenticateMiddleware(s.Sessions, s.Settings, allowTestHeaders))
		api.Use(auth.CSRFMiddleware)

		api.Get("/health", s.handleDetailedHealth)

		// 內建權限檢測示範端點（供基礎測試與診斷使用）
		api.With(auth.RequireAuth).Get("/auth/check", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"authenticated":true}`))
		})
		api.With(auth.RequireAdmin).Post("/auth/admin-check", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"admin":true}`))
		})

		// 掛載所有功能模組之路由
		for _, reg := range registrars {
			if reg != nil {
				reg.RegisterRoutes(api)
			}
		}
	})

	// 前端靜態頁面與 Single Page Application 萬用回退路由
	r.NotFound(static.SPAHandler().ServeHTTP)

	s.router = r
}
