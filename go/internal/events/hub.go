package events

import (
	"encoding/json"
	"sync"
)

// Event is pushed to commander UI subscribers (SSE).
type Event struct {
	Type    string `json:"type"`
	RunID   string `json:"run_id,omitempty"`
	Stage   string `json:"stage,omitempty"`
	Message string `json:"message,omitempty"`
}

// Hub broadcasts events to SSE clients and, optionally, to another process.
type Hub struct {
	mu        sync.RWMutex
	clients   map[chan []byte]struct{}
	forwarder func([]byte)
}

func NewHub() *Hub {
	return &Hub{clients: make(map[chan []byte]struct{})}
}

// SetForwarder receives every published event (e.g. to relay worker events to
// the API process). It must not block.
func (h *Hub) SetForwarder(f func([]byte)) {
	h.mu.Lock()
	h.forwarder = f
	h.mu.Unlock()
}

func (h *Hub) Publish(ev Event) {
	h.mu.RLock()
	idle := len(h.clients) == 0 && h.forwarder == nil
	h.mu.RUnlock()
	if idle {
		return
	}
	data, _ := json.Marshal(ev)
	h.mu.RLock()
	fwd := h.forwarder
	h.broadcastLocked(data)
	h.mu.RUnlock()
	if fwd != nil {
		fwd(data)
	}
}

// Broadcast delivers an already-encoded event to local clients only (no forwarding).
func (h *Hub) Broadcast(data []byte) {
	h.mu.RLock()
	h.broadcastLocked(data)
	h.mu.RUnlock()
}

func (h *Hub) broadcastLocked(data []byte) {
	for ch := range h.clients {
		select {
		case ch <- data:
		default:
		}
	}
}

func (h *Hub) Subscribe() (chan []byte, func()) {
	ch := make(chan []byte, 16)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	unsub := func() {
		h.mu.Lock()
		delete(h.clients, ch)
		close(ch)
		h.mu.Unlock()
	}
	return ch, unsub
}
