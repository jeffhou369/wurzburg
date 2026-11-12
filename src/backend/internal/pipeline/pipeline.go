package pipeline

import (
	"context"

	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
)

// SubnetScopeProcessor 定義處理管線第一步：「管理範圍」（由第 12 張實作）。
type SubnetScopeProcessor interface {
	ApplyInterfaceAddresses(ctx context.Context, outputs []*interfaces.PollOutput) error
}

// ReconcilerProcessor 定義處理管線第二步：「狀態整併」（由第 13 張實作）。
type ReconcilerProcessor interface {
	ReconcileObservations(ctx context.Context, outputs []*interfaces.PollOutput, failedVRFConfigIDs []int64) error
}

// PollPipeline 依序協調一輪採集輸出進入「管理範圍」與「狀態整併」。
// 在功能工作項目尚未填入時，預設步驟可為空實作，讓測試可直接注入結果驗證呼叫。
type PollPipeline struct {
	ScopeProcessor      SubnetScopeProcessor
	ReconcilerProcessor ReconcilerProcessor
}

// New 建立一輪讀取結果的處理管線。
func New(scope SubnetScopeProcessor, reconciler ReconcilerProcessor) *PollPipeline {
	return &PollPipeline{
		ScopeProcessor:      scope,
		ReconcilerProcessor: reconciler,
	}
}

// Process 依序執行：1. 更新網段與閘道位址 -> 2. 整併 IP 紀錄與觀測。
func (p *PollPipeline) Process(ctx context.Context, outputs []*interfaces.PollOutput, failedVRFConfigIDs []int64) error {
	if p.ScopeProcessor != nil {
		if err := p.ScopeProcessor.ApplyInterfaceAddresses(ctx, outputs); err != nil {
			return err
		}
	}
	if p.ReconcilerProcessor != nil {
		if err := p.ReconcilerProcessor.ReconcileObservations(ctx, outputs, failedVRFConfigIDs); err != nil {
			return err
		}
	}
	return nil
}
