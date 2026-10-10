package approval

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func waitPending(t *testing.T, g *Gate, id string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		g.mu.Lock()
		_, ok := g.pending[id]
		g.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("request never became pending")
}

func TestConcurrentApprovalsNeverBlock(t *testing.T) {
	g := NewGate(time.Minute)
	got := make(chan Decision, 1)
	go func() { got <- g.Request(context.Background(), "r1", "x") }()
	waitPending(t, g, "r1")

	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(approve bool) {
			defer wg.Done()
			var ok bool
			if approve {
				ok = g.Approve("r1", "")
			} else {
				ok = g.Reject("r1", "")
			}
			if ok {
				wins.Add(1)
			}
		}(i%2 == 0)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("duplicate approve/reject calls blocked")
	}
	if n := wins.Load(); n != 1 {
		t.Fatalf("exactly one resolver should win, got %d", n)
	}
	if d := <-got; d.By != "human" {
		t.Fatalf("decision=%+v", d)
	}
}

func TestApproveAfterTimeoutReturnsFalse(t *testing.T) {
	g := NewGate(5 * time.Millisecond)
	if d := g.Request(context.Background(), "r2", "x"); d.By != "auto-timeout" {
		t.Fatalf("decision=%+v", d)
	}
	if g.Approve("r2", "") {
		t.Fatal("late approve should report no pending request")
	}
}
