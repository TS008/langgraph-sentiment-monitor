package casefile

import (
	"encoding/json"
	"sync"

	"github.com/google/uuid"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/agentbus"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/rbac"
)

// Artifact is a bounded chunk of run state (reference by id in handoffs).
type Artifact struct {
	ID       string           `json:"id"`
	Slot     Slot             `json:"slot"`
	Summary  string           `json:"summary"`
	Payload  json.RawMessage  `json:"payload,omitempty"`
	Author   agentbus.AgentID `json:"author"`
}

// RunState is checkpoint-friendly case file for one crisis run.
type RunState struct {
	RunID      string             `json:"run_id"`
	Checkpoint agentbus.AgentID   `json:"checkpoint"`
	Artifacts  map[string]Artifact `json:"artifacts"`
}

// Store holds in-memory case files (v1; swap for Redis/DB later).
type Store struct {
	mu    sync.RWMutex
	runs  map[string]*RunState
}

func NewStore() *Store {
	return &Store{runs: make(map[string]*RunState)}
}

func (s *Store) Init(runID string) *RunState {
	st := &RunState{
		RunID:     runID,
		Artifacts: make(map[string]Artifact),
	}
	s.mu.Lock()
	s.runs[runID] = st
	s.mu.Unlock()
	return st
}

func (s *Store) Get(runID string) *RunState {
	s.mu.RLock()
	st := s.runs[runID]
	s.mu.RUnlock()
	return st
}

// Put stores an artifact and returns its id.
func (s *Store) Put(runID string, slot Slot, summary string, payload interface{}, author agentbus.AgentID) (string, Artifact) {
	raw, _ := json.Marshal(payload)
	art := Artifact{
		ID:      uuid.NewString(),
		Slot:    slot,
		Summary: summary,
		Payload: raw,
		Author:  author,
	}
	s.mu.Lock()
	st := s.runs[runID]
	if st == nil {
		st = &RunState{RunID: runID, Artifacts: make(map[string]Artifact)}
		s.runs[runID] = st
	}
	st.Artifacts[art.ID] = art
	s.mu.Unlock()
	return art.ID, art
}

func (s *Store) Checkpoint(runID string, at agentbus.AgentID) {
	s.mu.Lock()
	if st := s.runs[runID]; st != nil {
		st.Checkpoint = at
	}
	s.mu.Unlock()
}

// ScopedView returns summaries the role may read (least privilege).
func (s *Store) ScopedView(runID string, agent agentbus.AgentID) map[Slot]string {
	role := rbac.RoleForAgent(agent)
	allowed := slotsForRole(role)
	st := s.Get(runID)
	if st == nil {
		return nil
	}
	out := make(map[Slot]string)
	for _, art := range st.Artifacts {
		if !allowed[art.Slot] {
			continue
		}
		out[art.Slot] = art.Summary
	}
	return out
}

func slotsForRole(role rbac.Role) map[Slot]bool {
	all := func(slots ...Slot) map[Slot]bool {
		m := make(map[Slot]bool)
		for _, sl := range slots {
			m[sl] = true
		}
		return m
	}
	switch role {
	case rbac.RoleSentinel:
		return all(SlotSignals, SlotSentiment, SlotFeedback)
	case rbac.RoleDiagnostician:
		return all(SlotSignals, SlotSentiment, SlotDiagnosis)
	case rbac.RoleStrategist:
		return all(SlotSentiment, SlotDiagnosis, SlotStrategy)
	case rbac.RoleLegal:
		return all(SlotStrategy, SlotDiagnosis, SlotLegal)
	case rbac.RoleApprover, rbac.RoleAction:
		return all(SlotSentiment, SlotDiagnosis, SlotStrategy, SlotLegal, SlotApproval, SlotAction)
	case rbac.RoleAuditor, rbac.RoleOrchestrator:
		m := make(map[Slot]bool)
		for _, sl := range []Slot{SlotSignals, SlotSentiment, SlotDiagnosis, SlotStrategy, SlotLegal, SlotApproval, SlotAction, SlotFeedback, SlotStatus, SlotPostMortem} {
			m[sl] = true
		}
		return m
	default:
		return map[Slot]bool{}
	}
}

// HandoffSummary builds a compact string for LLM prompts from scoped view.
func HandoffSummary(view map[Slot]string, maxLen int) string {
	if len(view) == 0 {
		return ""
	}
	var b []byte
	for slot, sum := range view {
		line := string(slot) + ": " + sum + "\n"
		if len(b)+len(line) > maxLen {
			break
		}
		b = append(b, line...)
	}
	return string(b)
}
