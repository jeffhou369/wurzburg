package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jeffhou369/wurzburg/backend/internal/audit"
	"github.com/jeffhou369/wurzburg/backend/internal/auth"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/crypto"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
	"github.com/jeffhou369/wurzburg/backend/internal/pipeline"
	"github.com/jeffhou369/wurzburg/backend/internal/settings"
)

// Scheduler 負責協調 3 分鐘定期 SNMP 輪詢、重試、並行控管與讀取結果紀錄。
type Scheduler struct {
	db        *sql.DB
	clock     clock.Clock
	cipher    *crypto.Cipher
	settings  *settings.Store
	audit     *audit.Logger
	collector interfaces.Collector
	pipeline  *pipeline.PollPipeline

	mu              sync.Mutex
	inProgress      bool
	roundIncomplete bool
	lastSyncAt      *time.Time
	lastSyncSuccess bool
}

func New(
	db *sql.DB,
	clk clock.Clock,
	cipher *crypto.Cipher,
	sett *settings.Store,
	aud *audit.Logger,
	col interfaces.Collector,
	pipe *pipeline.PollPipeline,
) *Scheduler {
	if clk == nil {
		clk = clock.NewRealClock()
	}
	return &Scheduler{
		db:        db,
		clock:     clk,
		cipher:    cipher,
		settings:  sett,
		audit:     aud,
		collector: col,
		pipeline:  pipe,
	}
}

func (s *Scheduler) RegisterRoutes(api chi.Router) {
	api.With(auth.RequireAdmin).Post("/sync/trigger", s.handleTrigger)
	api.With(auth.RequireAuth).Get("/sync/status", s.handleStatus)
}

func (s *Scheduler) Status() (inProgress, roundIncomplete bool, lastSyncAt *time.Time, lastSyncSuccess bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inProgress, s.roundIncomplete, s.lastSyncAt, s.lastSyncSuccess
}

func (s *Scheduler) handleStatus(w http.ResponseWriter, r *http.Request) {
	inProg, incomplete, last, lastSuccess := s.Status()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"in_progress":       inProg,
		"round_incomplete":  incomplete,
		"last_sync_at":      last,
		"last_sync_success": lastSuccess,
	})
}

func (s *Scheduler) handleTrigger(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.inProgress {
		s.mu.Unlock()
		auth.WriteError(w, http.StatusConflict, "sync_in_progress", "目前已有同步輪次正在執行中")
		return
	}
	s.mu.Unlock()

	u := auth.UserFromContext(r.Context())
	if s.audit != nil && u != nil {
		s.audit.Log(r.Context(), u.Username, u.Role, r.RemoteAddr, "sync.manual_trigger", "sync", "all", "手動觸發立即同步", nil)
	}

	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if err := s.RunCycle(bgCtx); err != nil {
			slog.Warn("手動觸發的同步輪次執行異常", "error", err)
		}
	}()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"message":"已觸發立即同步"}`))
}

// StartBackgroundLoop 啟動背景 3 分鐘定時輪詢迴圈（供生產環境 main.go 呼叫）。
func (s *Scheduler) StartBackgroundLoop(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.RunCycle(ctx); err != nil {
				slog.Warn("輪詢週期未能啟動或執行異常", "error", err)
			}
		}
	}
}

type switchBatch struct {
	switchID int64
	inputs   []interfaces.VRFReadInput
}

// RunCycle 執行完整的一輪 SNMP 輪詢。若上一輪尚未結束，設定 roundIncomplete 並立即略過。
func (s *Scheduler) RunCycle(ctx context.Context) (cycleErr error) {
	s.mu.Lock()
	if s.inProgress {
		s.roundIncomplete = true
		s.mu.Unlock()
		return errors.New("上一輪輪詢尚未結束，跳過本次啟動")
	}
	s.inProgress = true
	s.roundIncomplete = false
	s.mu.Unlock()

	defer func() {
		now := s.clock.Now()
		s.mu.Lock()
		s.inProgress = false
		s.lastSyncAt = &now
		s.lastSyncSuccess = cycleErr == nil
		s.mu.Unlock()
	}()

	if s.db == nil || s.collector == nil {
		return nil
	}

	batches, err := s.loadEnabledBatches(ctx)
	if err != nil {
		return fmt.Errorf("載入交換器讀取清單失敗: %w", err)
	}

	maxConcurrency := 12
	if s.settings != nil {
		maxConcurrency = s.settings.GetInt(ctx, "poll_concurrency", 12)
	}
	if maxConcurrency < 1 {
		maxConcurrency = 1
	}

	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	var resMu sync.Mutex
	var successOutputs []*interfaces.PollOutput
	var failedVRFConfigIDs []int64

	// 不同交換器之間受上限並行，同一台交換器內部的多筆 VRF 讀取設定循序執行
	for _, batch := range batches {
		wg.Add(1)
		go func(b switchBatch) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			for _, input := range b.inputs {
				if ctx.Err() != nil {
					return
				}
				out, ok := s.pollOneWithRetry(ctx, input)
				resMu.Lock()
				if ok && out != nil {
					successOutputs = append(successOutputs, out)
				} else {
					failedVRFConfigIDs = append(failedVRFConfigIDs, input.VRFConfigID)
				}
				resMu.Unlock()
			}
		}(batch)
	}

	wg.Wait()

	// 單一 VRF 讀取失敗不算本輪失敗（已記入該 VRF 的狀態，並以 failedVRFConfigIDs 交給整併步驟）；
	// 但管理範圍或狀態整併步驟失敗代表本輪結果沒有正確落地，必須往上傳回，不可只寫日誌。
	var pipelineErr error
	if s.pipeline != nil && (len(successOutputs) > 0 || len(failedVRFConfigIDs) > 0) {
		if err := s.pipeline.Process(ctx, successOutputs, failedVRFConfigIDs); err != nil {
			slog.ErrorContext(ctx, "處理輪詢讀取管線失敗", "error", err)
			pipelineErr = fmt.Errorf("處理輪詢讀取管線失敗: %w", err)
		}
	}

	// 歷史紀錄清理與管線結果無關，即使管線失敗也照常執行
	_, _ = s.CleanupOldPollRuns(ctx)
	return pipelineErr
}

// pollOneWithRetry 讀取單一 VRF 設定，失敗時立即重試一次（總嘗試次數固定為 2）。
func (s *Scheduler) pollOneWithRetry(ctx context.Context, input interfaces.VRFReadInput) (*interfaces.PollOutput, bool) {
	startedAt := s.clock.Now()
	_, _ = s.db.ExecContext(ctx, `UPDATE vrf_read_configs SET last_status = 'running', last_poll_at = ? WHERE id = ?`, startedAt, input.VRFConfigID)

	var out *interfaces.PollOutput
	var err error
	retried := false

	out, err = s.collector.Collect(ctx, input)
	if err != nil {
		retried = true
		out, err = s.collector.Collect(ctx, input)
	}

	finishedAt := s.clock.Now()

	if err == nil && out != nil {
		learned := len(out.ARPObservations)
		_, _ = s.db.ExecContext(
			ctx,
			`INSERT INTO poll_runs (vrf_config_id, switch_id, started_at, finished_at, is_success, retried, learned_count, error_class, error_summary, created_at)
			 VALUES (?, ?, ?, ?, TRUE, ?, ?, '', '', ?)`,
			input.VRFConfigID, input.SwitchID, startedAt, finishedAt, retried, learned, finishedAt,
		)
		_, _ = s.db.ExecContext(
			ctx,
			`UPDATE vrf_read_configs SET last_status = 'success', last_poll_at = ?, last_success_at = ?, last_error_class = '', last_error_summary = '', last_learned_count = ? WHERE id = ?`,
			finishedAt, finishedAt, learned, input.VRFConfigID,
		)
		return out, true
	}

	errClass := "unknown"
	errMsg := "讀取失敗"
	if err != nil {
		errMsg = err.Error()
		var ce *interfaces.CollectError
		if errors.As(err, &ce) {
			errClass = string(ce.Class)
		}
	}

	_, _ = s.db.ExecContext(
		ctx,
		`INSERT INTO poll_runs (vrf_config_id, switch_id, started_at, finished_at, is_success, retried, learned_count, error_class, error_summary, created_at)
		 VALUES (?, ?, ?, ?, FALSE, ?, 0, ?, ?, ?)`,
		input.VRFConfigID, input.SwitchID, startedAt, finishedAt, retried, errClass, errMsg, finishedAt,
	)
	_, _ = s.db.ExecContext(
		ctx,
		`UPDATE vrf_read_configs SET last_status = 'failed', last_poll_at = ?, last_error_class = ?, last_error_summary = ? WHERE id = ?`,
		finishedAt, errClass, errMsg, input.VRFConfigID,
	)
	return nil, false
}

// CleanupOldPollRuns 清除超過保留天數（預設 30 天）的讀取歷史紀錄。
func (s *Scheduler) CleanupOldPollRuns(ctx context.Context) (int64, error) {
	if s.db == nil {
		return 0, nil
	}
	days := 30
	if s.settings != nil {
		days = s.settings.GetInt(ctx, "poll_run_retention_days", 30)
	}
	if days <= 0 {
		return 0, nil
	}
	cutoff := s.clock.Now().AddDate(0, 0, -days)
	res, err := s.db.ExecContext(ctx, `DELETE FROM poll_runs WHERE started_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Scheduler) loadEnabledBatches(ctx context.Context) ([]switchBatch, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT s.id, v.id, s.mgmt_ip, s.platform, s.snmp_version, s.snmp_port, s.timeout_ms,
		        v.vrf_name, v.snmp_context, COALESCE(v.snmp_community_encrypted, ''),
		        s.v3_username, s.v3_auth_protocol, COALESCE(s.v3_auth_pass_encrypted, ''),
		        s.v3_priv_protocol, COALESCE(s.v3_priv_pass_encrypted, '')
		 FROM switches s
		 JOIN vrf_read_configs v ON s.id = v.switch_id
		 WHERE s.enabled = TRUE AND v.enabled = TRUE
		 ORDER BY s.id ASC, v.is_default DESC, v.id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	batchMap := make(map[int64][]interfaces.VRFReadInput)
	var order []int64

	for rows.Next() {
		var in interfaces.VRFReadInput
		var commEnc, authPassEnc, privPassEnc string
		if err := rows.Scan(
			&in.SwitchID, &in.VRFConfigID, &in.MgmtIP, &in.Platform, &in.SNMPVersion, &in.SNMPPort, &in.TimeoutMs,
			&in.VRFName, &in.SNMPContext, &commEnc,
			&in.V3Username, &in.V3AuthProto, &authPassEnc, &in.V3PrivProto, &privPassEnc,
		); err != nil {
			continue
		}
		if h := s.cipher; h != nil {
			if commEnc != "" {
				in.SNMPCommunity, _ = h.Decrypt(commEnc)
			}
			if authPassEnc != "" {
				in.V3AuthPass, _ = h.Decrypt(authPassEnc)
			}
			if privPassEnc != "" {
				in.V3PrivPass, _ = h.Decrypt(privPassEnc)
			}
		}
		if _, seen := batchMap[in.SwitchID]; !seen {
			order = append(order, in.SwitchID)
		}
		batchMap[in.SwitchID] = append(batchMap[in.SwitchID], in)
	}

	var batches []switchBatch
	for _, swID := range order {
		batches = append(batches, switchBatch{switchID: swID, inputs: batchMap[swID]})
	}
	return batches, nil
}
