package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/approval"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/config"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/crawl"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/llm"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/social"
)

// slowCompletor takes `delay` per call and fails if its context expires first.
type slowCompletor struct {
	delay time.Duration
	stub  *llm.StubCompletor
}

func (s *slowCompletor) wait(ctx context.Context) error {
	select {
	case <-time.After(s.delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *slowCompletor) AnalyzeSignals(ctx context.Context, corpus string) (llm.SentimentResult, error) {
	if err := s.wait(ctx); err != nil {
		return llm.SentimentResult{}, err
	}
	return s.stub.AnalyzeSignals(ctx, corpus)
}

func (s *slowCompletor) Complete(ctx context.Context, system, user string) (string, error) {
	if err := s.wait(ctx); err != nil {
		return "", err
	}
	return s.stub.Complete(ctx, system, user)
}

func TestLLMTimeoutAppliesPerCall(t *testing.T) {
	cfg := config.Config{ApprovalTimeout: time.Millisecond, LLMTimeout: 150 * time.Millisecond}
	reg := &social.Registry{Sources: []social.Source{&social.FixtureSource{Signals: []crawl.Signal{{Source: "f", Title: "t"}}}}}
	orch := NewOrchestrator(cfg, approval.NewGate(cfg.ApprovalTimeout), nil, nil, nil, nil, &OrchestratorDeps{
		Completor: &slowCompletor{delay: 60 * time.Millisecond, stub: llm.NewStubCompletor()},
		Social:    reg,
	})

	res := orch.Execute(context.Background(), RunOptions{}, nil)
	if res.Status != "completed" {
		t.Fatalf("each call fits its own budget; status=%s err=%s", res.Status, res.Error)
	}
	if res.PostMortem == "" {
		t.Fatal("last stage should still have LLM budget")
	}
}
