package casefile

import (
	"strings"
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

func TestHandoffSummaryIsOrderedAndSkipsOversizedSlots(t *testing.T) {
	view := map[Slot]string{
		SlotLegal:     "legal ok",
		SlotDiagnosis: strings.Repeat("x", 500),
		SlotSentiment: "risk=70",
	}
	want := "sentiment: risk=70\nlegal: legal ok\n"
	for i := 0; i < 20; i++ {
		if got := HandoffSummary(view, 100); got != want {
			t.Fatalf("got %q want %q", got, want)
		}
	}
}
