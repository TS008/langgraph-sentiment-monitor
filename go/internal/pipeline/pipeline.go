package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/agentbus"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/approval"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/config"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/crawl"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/llm"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/rbac"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/telemetry"
)

// Stage names mirror the legacy LangGraph agent flow.
const (
	StageMonitor    = "sentiment_sentinel"
	StageSentiment  = "sentiment_analysis"
	StageDiagnose   = "technical_diagnostician"
	StageStrategy   = "pr_strategist"
	StageLegal      = "legal_counsel"
	StageApproval   = "decision_gateway"
	StageAction     = "action_coordinator"
	StageFeedback   = "realtime_sentiment_feedback"
	StageStatus     = "crisis_status_check"
	StagePostMortem = "post_mortem_analyst"
)

// RunOptions configures a single pipeline execution.
type RunOptions struct {
	RunID    string
	HTMLURL  string
	JSRender bool
}

// StageLog is one step in a run.
type StageLog struct {
	Stage     string    `json:"stage"`
	Message   string    `json:"message"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
}

// RunResult is the full output of one pipeline execution.
type RunResult struct {
	ID             string                `json:"id"`
	Status         string                `json:"status"`
	Checkpoint     string                `json:"checkpoint"`
	StartedAt      time.Time             `json:"started_at"`
	FinishedAt     time.Time             `json:"finished_at"`
	Signals        []crawl.Signal        `json:"signals"`
	IngestNotes    []string              `json:"ingest_notes,omitempty"`
	Sentiment      llm.SentimentResult   `json:"sentiment"`
	Diagnosis      string                `json:"diagnosis"`
	Strategy       string                `json:"strategy"`
	LegalReview    string                `json:"legal_review"`
	Approval       approval.Decision     `json:"approval"`
	ActionStub     string                `json:"action_stub"`
	Feedback       string                `json:"realtime_sentiment"`
	StatusCheck    string                `json:"status_check"`
	CrisisResolved bool                  `json:"crisis_resolved"`
	PostMortem     string                `json:"post_mortem"`
	Stages         []StageLog            `json:"stages"`
	AgentMessages  []agentbus.Envelope   `json:"agent_messages"`
	AuditLog       []rbac.AuditRecord    `json:"audit_log"`
	Error          string                `json:"error,omitempty"`
}

// OnProgress is called after each stage (for SSE / store updates).
type OnProgress func(res RunResult)

// Runner is the public entrypoint; delegates to multi-agent Orchestrator.
type Runner struct {
	orch *Orchestrator
}

func NewRunner(cfg config.Config, gate *approval.Gate, metrics *telemetry.Metrics) *Runner {
	bus := agentbus.NewBus()
	return &Runner{
		orch: NewOrchestrator(cfg, gate, metrics, bus, nil, nil),
	}
}

// Bus returns the in-process agent mailbox (for SSE wiring).
func (r *Runner) Bus() *agentbus.Bus {
	return r.orch.Bus()
}

func (r *Runner) Run(ctx context.Context, opts RunOptions, onProgress OnProgress) RunResult {
	return r.orch.Execute(ctx, opts, onProgress)
}

func buildCorpus(signals []crawl.Signal) string {
	var b strings.Builder
	for i, s := range signals {
		if i >= 20 {
			break
		}
		fmt.Fprintf(&b, "- [%s] %s — %s\n", s.Source, s.Title, s.Snippet)
	}
	return b.String()
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
