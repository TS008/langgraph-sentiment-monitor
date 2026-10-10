package agentbus

import (
	"time"

	"github.com/google/uuid"
)

// Kind classifies inter-agent traffic (A2A-style envelopes, not free-form chat).
type Kind string

const (
	KindTask     Kind = "task"
	KindHandoff  Kind = "handoff"
	KindArtifact Kind = "artifact"
	KindDecision Kind = "decision"
	KindAudit    Kind = "audit"
)

// AgentID names a crisis-stage agent peer.
type AgentID string

const (
	AgentSentinel     AgentID = "sentiment_sentinel"
	AgentSentiment    AgentID = "sentiment_analysis"
	AgentDiagnostician AgentID = "technical_diagnostician"
	AgentStrategist   AgentID = "pr_strategist"
	AgentLegal        AgentID = "legal_counsel"
	AgentGateway      AgentID = "decision_gateway"
	AgentAction       AgentID = "action_coordinator"
	AgentFeedback     AgentID = "realtime_sentiment_feedback"
	AgentStatus       AgentID = "crisis_status_check"
	AgentPostMortem   AgentID = "post_mortem_analyst"
	AgentOrchestrator AgentID = "orchestrator"
	AgentHuman        AgentID = "human_commander"
)

// Envelope is a typed message on the bus.
type Envelope struct {
	ID      string    `json:"id"`
	RunID   string    `json:"run_id"`
	Kind    Kind      `json:"kind"`
	From    AgentID   `json:"from"`
	To      AgentID   `json:"to,omitempty"`
	Subject string    `json:"subject"`
	Body    string    `json:"body"`
	Refs    []string  `json:"refs,omitempty"`
	At      time.Time `json:"at"`
}

func NewEnvelope(runID string, kind Kind, from, to AgentID, subject, body string, refs []string) Envelope {
	return Envelope{
		ID:      uuid.NewString(),
		RunID:   runID,
		Kind:    kind,
		From:    from,
		To:      to,
		Subject: subject,
		Body:    body,
		Refs:    refs,
		At:      time.Now().UTC(),
	}
}
