package social

import (
	"context"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/crawl"
)

// FixtureSource returns fixed signals without network I/O.
type FixtureSource struct {
	NameLabel string
	Signals   []crawl.Signal
}

func (f *FixtureSource) Name() string {
	if f.NameLabel != "" {
		return f.NameLabel
	}
	return "fixture"
}

func (f *FixtureSource) Ready() bool { return len(f.Signals) > 0 }

func (f *FixtureSource) Fetch(_ context.Context) ([]crawl.Signal, error) {
	return f.Signals, nil
}
