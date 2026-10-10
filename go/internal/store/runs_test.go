package store

import (
	"fmt"
	"testing"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/pipeline"
)

func TestStoreEvictsOldestFinishedRuns(t *testing.T) {
	s := NewMemoryRunStore(3)
	s.Put(pipeline.RunResult{ID: "active", Status: "running"})
	for i := 0; i < 5; i++ {
		s.Put(pipeline.RunResult{ID: fmt.Sprint(i), Status: "completed"})
	}
	if s.Len() != 3 {
		t.Fatalf("len=%d want 3", s.Len())
	}
	if _, ok := s.Get("active"); !ok {
		t.Fatal("in-flight run must not be evicted")
	}
	if _, ok := s.Get("0"); ok {
		t.Fatal("oldest finished run should be evicted")
	}
	if _, ok := s.Get("4"); !ok {
		t.Fatal("newest run should be retained")
	}
}

func TestStoreUpdateDoesNotDuplicateOrder(t *testing.T) {
	s := NewMemoryRunStore(10)
	for i := 0; i < 5; i++ {
		s.Put(pipeline.RunResult{ID: "r", Status: "running"})
	}
	s.Put(pipeline.RunResult{ID: "r", Status: "completed"})
	if got := s.List(0); len(got) != 1 || got[0].Status != "completed" {
		t.Fatalf("list=%+v", got)
	}
}

func TestStoreListNewestFirstWithLimit(t *testing.T) {
	s := NewMemoryRunStore(10)
	for i := 0; i < 5; i++ {
		s.Put(pipeline.RunResult{ID: fmt.Sprint(i), Status: "completed"})
	}
	got := s.List(2)
	if len(got) != 2 || got[0].ID != "4" || got[1].ID != "3" {
		t.Fatalf("list=%+v", got)
	}
}
