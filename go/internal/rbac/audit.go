package rbac

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/agentbus"
)

// AuditRecord is an immutable permission / action log entry.
type AuditRecord struct {
	ID      string           `json:"id"`
	RunID   string           `json:"run_id"`
	At      time.Time        `json:"at"`
	Agent   agentbus.AgentID `json:"agent"`
	Tool    Tool             `json:"tool"`
	Allowed bool             `json:"allowed"`
	Detail  string           `json:"detail"`
}

// AuditLog stores append-only records per run.
type AuditLog struct {
	mu   sync.RWMutex
	rows map[string][]AuditRecord
}

func NewAuditLog() *AuditLog {
	return &AuditLog{rows: make(map[string][]AuditRecord)}
}

// Record checks permission and appends an audit row (also returned for bus publish).
func (l *AuditLog) Record(runID string, agent agentbus.AgentID, tool Tool, detail string) AuditRecord {
	allowed := Allow(agent, tool)
	rec := AuditRecord{
		ID:      uuid.NewString(),
		RunID:   runID,
		At:      time.Now().UTC(),
		Agent:   agent,
		Tool:    tool,
		Allowed: allowed,
		Detail:  detail,
	}
	l.mu.Lock()
	l.rows[runID] = append(l.rows[runID], rec)
	l.mu.Unlock()
	return rec
}

func (l *AuditLog) Delete(runID string) {
	l.mu.Lock()
	delete(l.rows, runID)
	l.mu.Unlock()
}

func (l *AuditLog) List(runID string) []AuditRecord {
	l.mu.RLock()
	src := l.rows[runID]
	out := make([]AuditRecord, len(src))
	copy(out, src)
	l.mu.RUnlock()
	return out
}
