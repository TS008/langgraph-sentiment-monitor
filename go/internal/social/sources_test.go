package social

import (
	"context"
	"testing"
	"time"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/crawl"
)

type slowSource struct {
	name  string
	delay time.Duration
}

func (s *slowSource) Name() string { return s.name }
func (s *slowSource) Ready() bool  { return true }
func (s *slowSource) Fetch(ctx context.Context) ([]crawl.Signal, error) {
	time.Sleep(s.delay)
	return []crawl.Signal{{Source: s.name}}, nil
}

func TestCollectFetchesSourcesConcurrently(t *testing.T) {
	const delay = 150 * time.Millisecond
	reg := &Registry{Sources: []Source{
		&slowSource{name: "a", delay: delay},
		&StubSource{Platform: "stub"},
		&slowSource{name: "b", delay: delay},
	}}
	start := time.Now()
	sigs, notes := reg.Collect(context.Background())
	if elapsed := time.Since(start); elapsed > delay*3/2 {
		t.Fatalf("collect took %v; sources should run in parallel", elapsed)
	}
	if len(sigs) != 2 || sigs[0].Source != "a" || sigs[1].Source != "b" {
		t.Fatalf("signals out of source order: %+v", sigs)
	}
	if len(notes) != 3 || notes[1] != "stub: skipped (not configured)" {
		t.Fatalf("notes=%v", notes)
	}
}
