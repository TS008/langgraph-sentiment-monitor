package store

import (
	"sync"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/pipeline"
)

// MemoryRunStore keeps recent pipeline results in-process (MVP).
type MemoryRunStore struct {
	mu   sync.RWMutex
	runs map[string]pipeline.RunResult
}

func NewMemoryRunStore() *MemoryRunStore {
	return &MemoryRunStore{runs: make(map[string]pipeline.RunResult)}
}

func (s *MemoryRunStore) Put(r pipeline.RunResult) {
	s.mu.Lock()
	s.runs[r.ID] = r
	s.mu.Unlock()
}

func (s *MemoryRunStore) Get(id string) (pipeline.RunResult, bool) {
	s.mu.RLock()
	r, ok := s.runs[id]
	s.mu.RUnlock()
	return r, ok
}

func (s *MemoryRunStore) List() []pipeline.RunResult {
	s.mu.RLock()
	out := make([]pipeline.RunResult, 0, len(s.runs))
	for _, r := range s.runs {
		out = append(out, r)
	}
	s.mu.RUnlock()
	return out
}
