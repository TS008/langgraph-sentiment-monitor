package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/approval"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/config"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/crawl"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/llm"
)

// Stage names mirror the legacy LangGraph agent flow.
const (
	StageMonitor   = "sentiment_sentinel"
	StageDiagnose  = "technical_diagnostician"
	StageStrategy  = "pr_strategist"
	StageApproval  = "decision_gateway"
	StageAction    = "action_coordinator"
)

// StageLog is one step in a run.
type StageLog struct {
	Stage     string    `json:"stage"`
	Message   string    `json:"message"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
}

// RunResult is the full output of one pipeline execution.
type RunResult struct {
	ID           string           `json:"id"`
	StartedAt    time.Time        `json:"started_at"`
	FinishedAt   time.Time        `json:"finished_at"`
	Signals      []crawl.Signal   `json:"signals"`
	Sentiment    llm.SentimentResult `json:"sentiment"`
	Diagnosis    string           `json:"diagnosis"`
	Strategy     string           `json:"strategy"`
	Approval     approval.Decision `json:"approval"`
	ActionStub   string           `json:"action_stub"`
	Stages       []StageLog       `json:"stages"`
	Error        string           `json:"error,omitempty"`
}

// Runner executes monitor → diagnose → strategy → approval → action stub.
type Runner struct {
	cfg      config.Config
	ingestor *crawl.Ingestor
	llm      *llm.Client
	gate     *approval.Gate
}

func NewRunner(cfg config.Config, gate *approval.Gate) *Runner {
	return &Runner{
		cfg:      cfg,
		ingestor: crawl.NewIngestor(cfg.CrawlParallelism),
		llm:      llm.New(cfg),
		gate:     gate,
	}
}

func (r *Runner) Run(ctx context.Context, extraHTMLURL string) RunResult {
	runID := uuid.NewString()
	started := time.Now().UTC()
	res := RunResult{
		ID:        runID,
		StartedAt: started,
		Stages:    []StageLog{},
	}

	logStage := func(stage, msg string, t0 time.Time) {
		res.Stages = append(res.Stages, StageLog{
			Stage:     stage,
			Message:   msg,
			StartedAt: t0,
			EndedAt:   time.Now().UTC(),
		})
	}

	// 1. Monitor / ingest
	t0 := time.Now().UTC()
	signals, err := r.ingestor.FetchRSS(ctx, r.cfg.RSSFeeds)
	if err != nil {
		res.Error = err.Error()
		logStage(StageMonitor, "RSS ingest failed: "+err.Error(), t0)
		res.FinishedAt = time.Now().UTC()
		return res
	}
	if extraHTMLURL != "" {
		sig, err := r.ingestor.FetchHTML(ctx, extraHTMLURL)
		if err == nil {
			signals = append(signals, sig)
		}
	}
	res.Signals = signals
	corpus := buildCorpus(signals)
	logStage(StageMonitor, fmt.Sprintf("Ingested %d signals", len(signals)), t0)

	// 2. Analyze sentiment / risk
	t0 = time.Now().UTC()
	llmCtx, cancel := context.WithTimeout(ctx, r.cfg.LLMTimeout)
	defer cancel()
	sent, err := r.llm.AnalyzeSignals(llmCtx, corpus)
	if err != nil {
		res.Error = err.Error()
		logStage(StageDiagnose, "sentiment analysis failed: "+err.Error(), t0)
		res.FinishedAt = time.Now().UTC()
		return res
	}
	res.Sentiment = sent
	logStage(StageDiagnose, fmt.Sprintf("Risk score %d (%s)", sent.RiskScore, sent.Sentiment), t0)

	// 3. Technical diagnosis (LLM)
	t0 = time.Now().UTC()
	diag, err := r.llm.Complete(llmCtx,
		"You are a chief technical diagnostician for a global social platform.",
		fmt.Sprintf("Crisis signals:\n%s\n\nProvide concise root-cause hypotheses and mitigation steps.", corpus),
	)
	if err != nil {
		res.Error = err.Error()
		res.FinishedAt = time.Now().UTC()
		return res
	}
	res.Diagnosis = diag
	logStage(StageDiagnose, "Technical diagnosis complete", t0)

	// 4. PR strategy
	t0 = time.Now().UTC()
	strat, err := r.llm.Complete(llmCtx,
		"You are a senior PR strategist during a product crisis.",
		fmt.Sprintf("Risk: %d\nSummary: %s\nDiagnosis excerpt: %s\n\nDraft a brief crisis comms plan (3 bullets).",
			sent.RiskScore, sent.Summary, truncate(diag, 400)),
	)
	if err != nil {
		res.Error = err.Error()
		res.FinishedAt = time.Now().UTC()
		return res
	}
	res.Strategy = strat
	logStage(StageStrategy, "PR strategy drafted", t0)

	// 5. Approval gate
	t0 = time.Now().UTC()
	proposed := fmt.Sprintf("Execute comms stub + technical playbook for risk %d: %s", sent.RiskScore, sent.Recommended)
	approvalID := runID + "-action"
	dec := r.gate.Request(ctx, approvalID, proposed)
	res.Approval = dec
	logStage(StageApproval, fmt.Sprintf("Approval by %s (approved=%v)", dec.By, dec.Approved), t0)

	if !dec.Approved {
		res.ActionStub = "No action taken (rejected at approval gate)"
		res.FinishedAt = time.Now().UTC()
		return res
	}

	// 6. Action stub (would fan out to departments in production)
	t0 = time.Now().UTC()
	res.ActionStub = fmt.Sprintf(
		"[STUB] Would dispatch: tech_runbook=%q pr_plan=%q channels=[status_page,social,email]",
		truncate(diag, 80), truncate(strat, 80),
	)
	logStage(StageAction, res.ActionStub, t0)

	res.FinishedAt = time.Now().UTC()
	return res
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
