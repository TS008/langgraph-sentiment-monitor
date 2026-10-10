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

// Hub broadcasts events to SSE clients.
type Hub struct {
	mu      sync.RWMutex
	clients map[chan []byte]struct{}
}

func NewHub() *Hub {
	return &Hub{clients: make(map[chan []byte]struct{})}
}

func (h *Hub) Publish(ev Event) {
	h.mu.RLock()
	if len(h.clients) == 0 {
		h.mu.RUnlock()
		return
	}
	h.mu.RUnlock()
	data, _ := json.Marshal(ev)
	h.mu.RLock()
	for ch := range h.clients {
		select {
		case ch <- data:
		default:
		}
	}
	h.mu.RUnlock()
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
