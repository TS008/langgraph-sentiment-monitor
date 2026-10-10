package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/agentbus"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/approval"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/config"
)

func TestOrchestratorPublishesTypedHandoffs(t *testing.T) {
	cfg := config.Load()
	cfg.ApprovalTimeout = 1 * time.Millisecond
	bus := agentbus.NewBus()
	orch := NewOrchestrator(cfg, approval.NewGate(cfg.ApprovalTimeout), nil, bus, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	res := orch.Execute(ctx, RunOptions{}, nil)
	if res.Error != "" && res.Status == "failed" && len(res.Signals) == 0 {
		t.Skip("network ingest unavailable in CI")
	}

	handoffs := bus.Filter(res.ID, agentbus.KindHandoff, "")
	if len(handoffs) < 3 {
		t.Fatalf("expected >=3 handoffs, got %d (messages %d)", len(handoffs), len(res.AgentMessages))
	}
	audits := bus.Filter(res.ID, agentbus.KindAudit, "")
	if len(audits) == 0 {
		t.Fatal("expected audit envelopes on bus")
	}
}
