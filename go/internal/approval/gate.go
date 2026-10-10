package approval

import (
	"context"
	"sync"
	"time"
)

// Decision records human/AI approval for a proposed action.
type Decision struct {
	Approved bool
	By       string // "human", "auto-timeout", "policy"
	Note     string
	At       time.Time
}

// Gate implements a simple approval wait with auto-approve on timeout (AI twin stub).
type Gate struct {
	timeout time.Duration
	mu      sync.Mutex
	pending map[string]chan Decision

	onPending func(id string, timeout time.Duration)
	onDone    func(id string)
}

// SetHooks observes requests becoming pending and leaving the pending set (resolved,
// timed out or cancelled), e.g. to advertise them to another process. Set before use.
func (g *Gate) SetHooks(onPending func(id string, timeout time.Duration), onDone func(id string)) {
	g.mu.Lock()
	g.onPending, g.onDone = onPending, onDone
	g.mu.Unlock()
}

func NewGate(timeout time.Duration) *Gate {
	return &Gate{
		timeout: timeout,
		pending: make(map[string]chan Decision),
	}
}

// Request registers an approval request and waits until Approve/Reject or timeout.
func (g *Gate) Request(ctx context.Context, requestID string, summary string) Decision {
	ch := make(chan Decision, 1)
	g.mu.Lock()
	g.pending[requestID] = ch
	onPending, onDone := g.onPending, g.onDone
	g.mu.Unlock()
	if onPending != nil {
		onPending(requestID, g.timeout)
	}

	defer func() {
		g.mu.Lock()
		delete(g.pending, requestID)
		g.mu.Unlock()
		if onDone != nil {
			onDone(requestID)
		}
	}()

	timer := time.NewTimer(g.timeout)
	defer timer.Stop()

	select {
	case d := <-ch:
		return d
	case <-timer.C:
		return Decision{
			Approved: true,
			By:       "auto-timeout",
			Note:     "No human response; AI digital twin auto-approved (stub): " + summary,
			At:       time.Now().UTC(),
		}
	case <-ctx.Done():
		return Decision{
			Approved: false,
			By:       "cancelled",
			Note:     ctx.Err().Error(),
			At:       time.Now().UTC(),
		}
	}
}

// Resolve approves or rejects a pending request; false if it is not pending here.
func (g *Gate) Resolve(_ context.Context, requestID string, approved bool, note string) (bool, error) {
	if approved {
		return g.Approve(requestID, note), nil
	}
	return g.Reject(requestID, note), nil
}

// Approve resolves a pending request (HTTP hook for future commander UI).
func (g *Gate) Approve(requestID string, note string) bool {
	return g.resolve(requestID, Decision{Approved: true, By: "human", Note: note, At: time.Now().UTC()})
}

// Reject resolves a pending request.
func (g *Gate) Reject(requestID string, note string) bool {
	return g.resolve(requestID, Decision{Approved: false, By: "human", Note: note, At: time.Now().UTC()})
}

// resolve claims the request under the lock so at most one caller ever sends on
// its 1-buffered channel; duplicate or late clicks return false instead of blocking.
func (g *Gate) resolve(requestID string, d Decision) bool {
	g.mu.Lock()
	ch, ok := g.pending[requestID]
	if ok {
		delete(g.pending, requestID)
	}
	g.mu.Unlock()
	if !ok {
		return false
	}
	ch <- d
	return true
}
