package social

import (
	"context"
	"fmt"
	"sync"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/crawl"
)

// Source ingests public or authenticated social/web signals.
type Source interface {
	Name() string
	// Ready reports whether credentials/config are present for live fetch.
	Ready() bool
	Fetch(ctx context.Context) ([]crawl.Signal, error)
}

// RSSSource wraps RSS feed URLs (public path — fully implemented).
type RSSSource struct {
	NameLabel string
	Feeds     []string
	Ingest    *crawl.Ingestor
}

func (r *RSSSource) Name() string { return r.NameLabel }

func (r *RSSSource) Ready() bool { return len(r.Feeds) > 0 }

func (r *RSSSource) Fetch(ctx context.Context) ([]crawl.Signal, error) {
	return r.Ingest.FetchRSS(ctx, r.Feeds)
}

// StubSource documents OAuth follow-up for walled gardens.
type StubSource struct {
	Platform    string
	EnvVars     []string
	Description string
}

func (s *StubSource) Name() string { return s.Platform }

func (s *StubSource) Ready() bool { return false }

func (s *StubSource) Fetch(ctx context.Context) ([]crawl.Signal, error) {
	return nil, fmt.Errorf("%s ingest not configured: set %v and implement OAuth (stub only)", s.Platform, s.EnvVars)
}

// Registry runs all ready sources and skips stubs gracefully.
type Registry struct {
	Sources []Source
}

// Collect fetches all ready sources concurrently; results and notes keep source order.
func (reg *Registry) Collect(ctx context.Context) ([]crawl.Signal, []string) {
	type result struct {
		sigs []crawl.Signal
		err  error
	}
	results := make([]result, len(reg.Sources))
	var wg sync.WaitGroup
	for i, src := range reg.Sources {
		if !src.Ready() {
			continue
		}
		wg.Add(1)
		go func(i int, src Source) {
			defer wg.Done()
			results[i].sigs, results[i].err = src.Fetch(ctx)
		}(i, src)
	}
	wg.Wait()

	var all []crawl.Signal
	var notes []string
	for i, src := range reg.Sources {
		if !src.Ready() {
			notes = append(notes, fmt.Sprintf("%s: skipped (not configured)", src.Name()))
			continue
		}
		sigs, err := results[i].sigs, results[i].err
		if len(sigs) > 0 {
			all = append(all, sigs...)
			notes = append(notes, fmt.Sprintf("%s: %d signals", src.Name(), len(sigs)))
		}
		if err != nil {
			notes = append(notes, fmt.Sprintf("%s: %v", src.Name(), err))
			continue
		}
		if len(sigs) == 0 {
			notes = append(notes, fmt.Sprintf("%s: 0 signals", src.Name()))
		}
	}
	return all, notes
}
