package agentbus

import "testing"

func TestBusPublishAndHistory(t *testing.T) {
	b := NewBus()
	run := "run-1"
	var seen int
	b.OnPublish(func(e Envelope) {
		if e.RunID == run {
			seen++
		}
	})
	b.Publish(NewEnvelope(run, KindTask, AgentOrchestrator, AgentSentinel, "ingest", "start", nil))
	b.Publish(NewEnvelope(run, KindHandoff, AgentSentinel, AgentDiagnostician, "handoff", "signals ready", []string{"art-1"}))

	if len(b.History(run)) != 2 {
		t.Fatalf("history len %d", len(b.History(run)))
	}
	if seen != 2 {
		t.Fatalf("handler seen %d", seen)
	}
	h := b.Filter(run, KindHandoff, AgentDiagnostician)
	if len(h) != 1 || h[0].Refs[0] != "art-1" {
		t.Fatalf("filter: %+v", h)
	}
}
