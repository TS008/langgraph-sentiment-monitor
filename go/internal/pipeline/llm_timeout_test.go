package pipeline

import (
	"context"
	"sync"
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

// concurrencyCompletor records the peak number of in-flight Complete calls.
type concurrencyCompletor struct {
	stub     *llm.StubCompletor
	mu       sync.Mutex
	inFlight int
	peak     int
}

func (c *concurrencyCompletor) AnalyzeSignals(ctx context.Context, corpus string) (llm.SentimentResult, error) {
	return c.stub.AnalyzeSignals(ctx, corpus)
}

func (c *concurrencyCompletor) Complete(ctx context.Context, system, user string) (string, error) {
	c.mu.Lock()
	c.inFlight++
	c.peak = max(c.peak, c.inFlight)
	c.mu.Unlock()
	time.Sleep(40 * time.Millisecond)
	c.mu.Lock()
	c.inFlight--
	c.mu.Unlock()
	return system, nil
}

func TestPostMortemOverlapsFeedback(t *testing.T) {
	cfg := config.Config{ApprovalTimeout: time.Millisecond, LLMTimeout: 5 * time.Second}
	reg := &social.Registry{Sources: []social.Source{&social.FixtureSource{Signals: []crawl.Signal{{Source: "f", Title: "t"}}}}}
	cc := &concurrencyCompletor{stub: llm.NewStubCompletor()}
	orch := NewOrchestrator(cfg, approval.NewGate(cfg.ApprovalTimeout), nil, nil, nil, nil, &OrchestratorDeps{Completor: cc, Social: reg})

	res := orch.Execute(context.Background(), RunOptions{}, nil)
	if res.Status != "completed" {
		t.Fatalf("status=%s err=%s", res.Status, res.Error)
	}
	if cc.peak < 2 {
		t.Fatalf("peak in-flight LLM calls=%d; post-mortem should overlap feedback", cc.peak)
	}
	if res.PostMortem != "Post-mortem analyst." || res.Feedback != "Sentiment sentinel." {
		t.Fatalf("results swapped: feedback=%q post_mortem=%q", res.Feedback, res.PostMortem)
	}
	last := res.Stages[len(res.Stages)-1]
	if last.Stage != StagePostMortem {
		t.Fatalf("post-mortem should still be the final stage, got %s", last.Stage)
	}
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
