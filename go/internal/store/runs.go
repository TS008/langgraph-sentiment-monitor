package store

import (
	"sync"
	"time"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/pipeline"
)

// DefaultMaxRuns bounds retained results when no explicit limit is configured.
const DefaultMaxRuns = 200

// MemoryRunStore keeps the most recent pipeline results in-process (MVP).
// When over capacity it evicts the oldest finished runs; in-flight runs are kept.
type MemoryRunStore struct {
	mu    sync.RWMutex
	max   int
	runs  map[string]pipeline.RunResult
	order []string // insertion order, oldest first
}

// RunSummary is the lightweight list view (no signals or transcripts).
type RunSummary struct {
	ID          string    `json:"id"`
	Status      string    `json:"status"`
	Checkpoint  string    `json:"checkpoint"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	RiskScore   int       `json:"risk_score"`
	SignalCount int       `json:"signal_count"`
	Error       string    `json:"error,omitempty"`
}

func NewMemoryRunStore(max int) *MemoryRunStore {
	if max < 1 {
		max = DefaultMaxRuns
	}
	return &MemoryRunStore{max: max, runs: make(map[string]pipeline.RunResult)}
}

func (s *MemoryRunStore) Put(r pipeline.RunResult) {
	s.mu.Lock()
	if _, exists := s.runs[r.ID]; !exists {
		s.order = append(s.order, r.ID)
	}
	s.runs[r.ID] = r
	s.evictLocked()
	s.mu.Unlock()
}

func (s *MemoryRunStore) evictLocked() {
	for len(s.runs) > s.max {
		victim := -1
		for i, id := range s.order {
			if !isActive(s.runs[id].Status) {
				victim = i
				break
			}
		}
		if victim < 0 {
			return
		}
		delete(s.runs, s.order[victim])
		s.order = append(s.order[:victim], s.order[victim+1:]...)
	}
}

func isActive(status string) bool {
	return status == "pending" || status == "running" || status == "queued"
}

func (s *MemoryRunStore) Get(id string) (pipeline.RunResult, bool) {
	s.mu.RLock()
	r, ok := s.runs[id]
	s.mu.RUnlock()
	return r, ok
}

func (s *MemoryRunStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.runs)
}

// List returns up to limit summaries, newest first. limit <= 0 means all retained runs.
func (s *MemoryRunStore) List(limit int) []RunSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := len(s.order)
	if limit <= 0 || limit > n {
		limit = n
	}
	out := make([]RunSummary, 0, limit)
	for i := n - 1; i >= 0 && len(out) < limit; i-- {
		r := s.runs[s.order[i]]
		out = append(out, RunSummary{
			ID:          r.ID,
			Status:      r.Status,
			Checkpoint:  r.Checkpoint,
			StartedAt:   r.StartedAt,
			FinishedAt:  r.FinishedAt,
			RiskScore:   r.Sentiment.RiskScore,
			SignalCount: len(r.Signals),
			Error:       r.Error,
		})
	}
	return out
}
