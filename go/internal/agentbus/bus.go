package agentbus

import (
	"sync"
)

// Handler receives envelopes for a run (optional tap for SSE).
type Handler func(Envelope)

// Bus is an in-process pub/sub mailbox per run (default for `aegis run`).
type Bus struct {
	mu       sync.RWMutex
	byRun    map[string][]Envelope
	handlers []Handler
}

func NewBus() *Bus {
	return &Bus{byRun: make(map[string][]Envelope)}
}

func (b *Bus) OnPublish(h Handler) {
	b.mu.Lock()
	b.handlers = append(b.handlers, h)
	b.mu.Unlock()
}

// Publish appends an envelope to the run log and notifies subscribers.
func (b *Bus) Publish(env Envelope) {
	b.mu.Lock()
	b.byRun[env.RunID] = append(b.byRun[env.RunID], env)
	handlers := append([]Handler(nil), b.handlers...)
	b.mu.Unlock()
	for _, h := range handlers {
		if h != nil {
			h(env)
		}
	}
}

// History returns all envelopes for a run (immutable copy).
func (b *Bus) History(runID string) []Envelope {
	b.mu.RLock()
	src := b.byRun[runID]
	out := make([]Envelope, len(src))
	copy(out, src)
	b.mu.RUnlock()
	return out
}

// Delete drops a run's mailbox once its history has been captured elsewhere.
func (b *Bus) Delete(runID string) {
	b.mu.Lock()
	delete(b.byRun, runID)
	b.mu.Unlock()
}

// Filter returns envelopes matching kind and/or recipient.
func (b *Bus) Filter(runID string, kind Kind, to AgentID) []Envelope {
	all := b.History(runID)
	var out []Envelope
	for _, e := range all {
		if kind != "" && e.Kind != kind {
			continue
		}
		if to != "" && e.To != "" && e.To != to {
			continue
		}
		out = append(out, e)
	}
	return out
}
