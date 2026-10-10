package agents

import (
	"fmt"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/agentbus"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/casefile"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/rbac"
)

// Runtime bundles bus, case file, and audit for one run.
type Runtime struct {
	RunID string
	Bus   *agentbus.Bus
	Case  *casefile.Store
	Audit *rbac.AuditLog
}

// RecordTool checks RBAC, appends audit, and publishes an audit envelope.
func (rt *Runtime) RecordTool(agent agentbus.AgentID, tool rbac.Tool, detail string) rbac.AuditRecord {
	rec := rt.Audit.Record(rt.RunID, agent, tool, detail)
	rt.Bus.Publish(agentbus.NewEnvelope(rt.RunID, agentbus.KindAudit, agent, "",
		string(tool), fmt.Sprintf("allowed=%v %s", rec.Allowed, detail), nil))
	return rec
}

// PublishHandoff sends a scoped handoff to the next agent.
func (rt *Runtime) PublishHandoff(from, to agentbus.AgentID, summary string, refs []string) error {
	rec := rt.RecordTool(from, rbac.ToolPublishBus, "handoff to "+string(to))
	if !rec.Allowed {
		return fmt.Errorf("rbac: agent %s denied publish", from)
	}
	rt.Bus.Publish(agentbus.NewEnvelope(rt.RunID, agentbus.KindHandoff, from, to, "handoff", summary, refs))
	return nil
}

// PublishArtifact announces a new case-file artifact.
func (rt *Runtime) PublishArtifact(from agentbus.AgentID, art casefile.Artifact) error {
	rec := rt.RecordTool(from, rbac.ToolWriteArtifact, string(art.Slot))
	if !rec.Allowed {
		return fmt.Errorf("rbac: agent %s denied write", from)
	}
	rt.Bus.Publish(agentbus.NewEnvelope(rt.RunID, agentbus.KindArtifact, from, "",
		string(art.Slot), art.Summary, []string{art.ID}))
	return nil
}

// PublishDecision records a gateway decision on the bus.
func (rt *Runtime) PublishDecision(from, to agentbus.AgentID, subject, body string) {
	rt.RecordTool(from, rbac.ToolPublishBus, "decision")
	rt.Bus.Publish(agentbus.NewEnvelope(rt.RunID, agentbus.KindDecision, from, to, subject, body, nil))
}
