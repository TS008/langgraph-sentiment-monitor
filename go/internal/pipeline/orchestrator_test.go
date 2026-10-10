package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/agentbus"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/approval"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/config"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/crawl"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/llm"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/social"
)

func TestOrchestratorPublishesTypedHandoffs(t *testing.T) {
	cfg := config.Config{
		ApprovalTimeout: time.Millisecond,
		LLMTimeout:      5 * time.Second,
	}
	signals := []crawl.Signal{{
		Source:  "fixture",
		URL:     "https://example.test/incident",
		Title:   "Test outage",
		Snippet: "users report login failure",
		Fetched: time.Now().UTC(),
	}}
	reg := &social.Registry{
		Sources: []social.Source{&social.FixtureSource{Signals: signals}},
	}
	bus := agentbus.NewBus()
	orch := NewOrchestrator(
		cfg,
		approval.NewGate(cfg.ApprovalTimeout),
		nil,
		bus,
		nil,
		nil,
		&OrchestratorDeps{
			Completor: llm.NewStubCompletor(),
			Social:    reg,
		},
	)

	res := orch.Execute(context.Background(), RunOptions{}, nil)
	if res.Status != "completed" {
		t.Fatalf("status=%s err=%s", res.Status, res.Error)
	}
	if len(res.Signals) != 1 {
		t.Fatalf("signals %d", len(res.Signals))
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
