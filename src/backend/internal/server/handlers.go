package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// HealthResponse 定義健康檢查端點之回應資料結構。
type HealthResponse struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
	Database  string    `json:"database"`
	Campus    string    `json:"campus"`
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleDetailedHealth(w http.ResponseWriter, r *http.Request) {
	dbStatus := "connected"
	if s.db != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := s.db.PingContext(ctx); err != nil {
			dbStatus = "disconnected"
		}
	} else {
		dbStatus = "unconfigured"
	}

	resp := HealthResponse{
		Status:    "ok",
		Timestamp: s.clock.Now(),
		Database:  dbStatus,
		Campus:    s.cfg.CampusName,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
