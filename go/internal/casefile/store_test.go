package casefile

import (
	"testing"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/agentbus"
)

func TestScopedViewHidesLegalFromSentinel(t *testing.T) {
	s := NewStore()
	s.Init("r1")
	s.Put("r1", SlotLegal, "privileged legal note", map[string]string{"x": "y"}, agentbus.AgentLegal)

	view := s.ScopedView("r1", agentbus.AgentSentinel)
	if _, ok := view[SlotLegal]; ok {
		t.Fatal("sentinel must not read legal slot")
	}
	view = s.ScopedView("r1", agentbus.AgentLegal)
	if view[SlotLegal] == "" {
		t.Fatal("legal role should read legal slot")
	}
}
