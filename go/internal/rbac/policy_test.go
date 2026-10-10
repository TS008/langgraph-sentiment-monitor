package rbac

import (
	"testing"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/agentbus"
)

func TestDenyExternalDispatchForSentinel(t *testing.T) {
	if Allow(agentbus.AgentSentinel, ToolExternalDispatch) {
		t.Fatal("sentinel must not dispatch externally")
	}
	if !Allow(agentbus.AgentAction, ToolExternalDispatch) {
		t.Fatal("action coordinator may dispatch (with approval)")
	}
}

func TestAuditRecordsDeny(t *testing.T) {
	log := NewAuditLog()
	rec := log.Record("r1", agentbus.AgentSentinel, ToolExternalDispatch, "attempt")
	if rec.Allowed {
		t.Fatal("expected deny")
	}
	if len(log.List("r1")) != 1 {
		t.Fatal("audit append")
	}
}
