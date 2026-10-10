package api

import "github.com/ts008/langgraph-sentiment-monitor/go/internal/queue"

// SetQueue wires the enqueuer after construction (avoids circular init).
func (s *Server) SetQueue(q queue.Enqueuer) {
	s.queue = q
}
