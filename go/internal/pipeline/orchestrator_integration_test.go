//go:build integration

package pipeline

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/agentbus"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/approval"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/config"
)

// Live RSS + LLM smoke test. Run with:
//
//	go test -tags=integration ./internal/pipeline -run Live -timeout 5m
//
// Requires network and OPENCODE_GO_API_KEY (or offline heuristic if unset).
func TestOrchestratorLiveEndToEnd(t *testing.T) {
	if os.Getenv("AEGIS_INTEGRATION") != "1" {
		t.Skip("set AEGIS_INTEGRATION=1 to run live orchestrator test")
	}
	cfg := config.Load()
	cfg.ApprovalTimeout = 5 * time.Second
	bus := agentbus.NewBus()
	orch := NewOrchestrator(cfg, approval.NewGate(cfg.ApprovalTimeout), nil, bus, nil, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.RunTimeout)
	defer cancel()

	res := orch.Execute(ctx, RunOptions{}, nil)
	if res.Error != "" && res.Status == "failed" {
		t.Fatalf("run failed: %s", res.Error)
	}
	if len(bus.History(res.ID)) < 5 {
		t.Fatalf("expected bus traffic, got %d messages", len(bus.History(res.ID)))
	}
}
