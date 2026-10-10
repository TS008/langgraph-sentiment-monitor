package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/agentbus"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/approval"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/casefile"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/config"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/crawl"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/llm"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/rbac"
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

	var handoffs, audits int
	for _, m := range res.AgentMessages {
		switch m.Kind {
		case agentbus.KindHandoff:
			handoffs++
		case agentbus.KindAudit:
			audits++
		}
	}
	if handoffs < 3 {
		t.Fatalf("expected >=3 handoffs, got %d (messages %d)", handoffs, len(res.AgentMessages))
	}
	if audits == 0 {
		t.Fatal("expected audit envelopes on bus")
	}
	if len(res.AuditLog) == 0 {
		t.Fatal("expected audit rows in result")
	}
}

func TestOrchestratorReleasesPerRunStateOnFinish(t *testing.T) {
	cfg := config.Config{ApprovalTimeout: time.Millisecond, LLMTimeout: 5 * time.Second}
	reg := &social.Registry{Sources: []social.Source{&social.FixtureSource{Signals: []crawl.Signal{{Source: "f", Title: "t"}}}}}
	bus := agentbus.NewBus()
	cases := casefile.NewStore()
	audit := rbac.NewAuditLog()
	orch := NewOrchestrator(cfg, approval.NewGate(cfg.ApprovalTimeout), nil, bus, cases, audit,
		&OrchestratorDeps{Completor: llm.NewStubCompletor(), Social: reg})

	res := orch.Execute(context.Background(), RunOptions{}, nil)
	if res.Status != "completed" {
		t.Fatalf("status=%s err=%s", res.Status, res.Error)
	}
	if len(bus.History(res.ID)) != 0 || cases.Get(res.ID) != nil || len(audit.List(res.ID)) != 0 {
		t.Fatal("per-run bus/casefile/audit state should be released after finish")
	}
}
