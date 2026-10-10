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
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/social"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/telemetry"
)

// Stage names mirror the legacy LangGraph agent flow.
const (
	StageMonitor   = "sentiment_sentinel"
	StageSentiment = "sentiment_analysis"
	StageDiagnose  = "technical_diagnostician"
	StageStrategy  = "pr_strategist"
	StageLegal     = "legal_counsel"
	StageApproval  = "decision_gateway"
	StageAction    = "action_coordinator"
	StageFeedback  = "realtime_sentiment_feedback"
	StageStatus    = "crisis_status_check"
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
	ID           string              `json:"id"`
	Status       string              `json:"status"` // pending|running|completed|failed
	StartedAt    time.Time           `json:"started_at"`
	FinishedAt   time.Time           `json:"finished_at"`
	Signals      []crawl.Signal      `json:"signals"`
	IngestNotes  []string            `json:"ingest_notes,omitempty"`
	Sentiment    llm.SentimentResult `json:"sentiment"`
	Diagnosis    string              `json:"diagnosis"`
	Strategy     string              `json:"strategy"`
	LegalReview  string              `json:"legal_review"`
	Approval     approval.Decision   `json:"approval"`
	ActionStub   string              `json:"action_stub"`
	Feedback     string              `json:"realtime_sentiment"`
	StatusCheck  string              `json:"status_check"`
	CrisisResolved bool              `json:"crisis_resolved"`
	PostMortem   string              `json:"post_mortem"`
	Stages       []StageLog          `json:"stages"`
	Error        string              `json:"error,omitempty"`
}

// OnProgress is called after each stage (for SSE / store updates).
type OnProgress func(res RunResult)

// Runner executes the full AEGIS graph (single pass).
type Runner struct {
	cfg      config.Config
	ingestor *crawl.Ingestor
	social   *social.Registry
	llm      *llm.Client
	gate     *approval.Gate
	metrics  *telemetry.Metrics
}

func NewRunner(cfg config.Config, gate *approval.Gate, metrics *telemetry.Metrics) *Runner {
	ing := crawl.NewIngestor(cfg.CrawlParallelism)
	reg := &social.Registry{
		Sources: []social.Source{
			&social.RSSSource{NameLabel: "rss", Feeds: cfg.RSSFeeds, Ingest: ing},
			&social.StubSource{Platform: "x", EnvVars: []string{"X_API_BEARER_TOKEN", "X_API_KEY"}, Description: "Twitter/X API v2"},
			&social.StubSource{Platform: "weibo", EnvVars: []string{"WEIBO_APP_KEY", "WEIBO_APP_SECRET"}, Description: "Weibo Open API"},
		},
	}
	return &Runner{
		cfg:      cfg,
		ingestor: ing,
		social:   reg,
		llm:      llm.New(cfg),
		gate:     gate,
		metrics:  metrics,
	}
}

func (r *Runner) Run(ctx context.Context, opts RunOptions, onProgress OnProgress) RunResult {
	runID := opts.RunID
	if runID == "" {
		runID = uuid.NewString()
	}
	useJS := opts.JSRender || r.cfg.UseJSRender

	started := time.Now().UTC()
	res := RunResult{
		ID:        runID,
		Status:    "running",
		StartedAt: started,
		Stages:    []StageLog{},
	}
	emit := func() {
		if onProgress != nil {
			onProgress(res)
		}
	}
	emit()

	logStage := func(stage, msg string, t0 time.Time) {
		res.Stages = append(res.Stages, StageLog{
			Stage:     stage,
			Message:   msg,
			StartedAt: t0,
			EndedAt:   time.Now().UTC(),
		})
		emit()
	}

	// 1. Monitor / ingest
	t0 := time.Now().UTC()
	signals, notes := r.social.Collect(ctx)
	if extra := opts.HTMLURL; extra != "" {
		sig, err := r.ingestor.FetchPage(ctx, extra, useJS)
		if err == nil {
			signals = append(signals, sig)
			notes = append(notes, fmt.Sprintf("html: 1 signal (js=%v)", useJS))
		} else {
			notes = append(notes, fmt.Sprintf("html: %v", err))
		}
	}
	if len(signals) == 0 {
		res.Error = "no signals ingested"
		res.Status = "failed"
		logStage(StageMonitor, res.Error, t0)
		res.FinishedAt = time.Now().UTC()
		r.finishMetrics(&res)
		emit()
		return res
	}
	res.Signals = signals
	res.IngestNotes = notes
	if r.metrics != nil {
		r.metrics.CrawlSignals.Add(uint64(len(signals)))
	}
	corpus := buildCorpus(signals)
	logStage(StageMonitor, fmt.Sprintf("Ingested %d signals (%s)", len(signals), strings.Join(notes, "; ")), t0)

	llmCtx, cancel := context.WithTimeout(ctx, r.cfg.LLMTimeout)
	defer cancel()
	llmCtx = llm.WithSession(llmCtx, runID)

	// 2. Sentiment / risk
	t0 = time.Now().UTC()
	if r.metrics != nil {
		r.metrics.LLMCalls.Add(1)
	}
	sent, err := r.llm.AnalyzeSignals(llmCtx, corpus)
	if err != nil {
		res.Error = err.Error()
		res.Status = "failed"
		logStage(StageSentiment, "failed: "+err.Error(), t0)
		res.FinishedAt = time.Now().UTC()
		r.finishMetrics(&res)
		emit()
		return res
	}
	res.Sentiment = sent
	logStage(StageSentiment, fmt.Sprintf("Risk score %d (%s)", sent.RiskScore, sent.Sentiment), t0)

	// 3. Technical diagnosis
	t0 = time.Now().UTC()
	if r.metrics != nil {
		r.metrics.LLMCalls.Add(1)
	}
	diag, err := r.llm.Complete(llmCtx,
		"You are a chief technical diagnostician for a global social platform.",
		fmt.Sprintf("Crisis signals:\n%s\n\nProvide concise root-cause hypotheses and mitigation steps.", corpus),
	)
	if err != nil {
		res.Error = err.Error()
		res.Status = "failed"
		res.FinishedAt = time.Now().UTC()
		r.finishMetrics(&res)
		emit()
		return res
	}
	res.Diagnosis = diag
	logStage(StageDiagnose, "Technical diagnosis complete", t0)

	// 4. PR strategy
	t0 = time.Now().UTC()
	if r.metrics != nil {
		r.metrics.LLMCalls.Add(1)
	}
	strat, err := r.llm.Complete(llmCtx,
		"You are a senior PR strategist during a product crisis.",
		fmt.Sprintf("Risk: %d\nSummary: %s\nDiagnosis excerpt: %s\n\nDraft a brief crisis comms plan (3 bullets).",
			sent.RiskScore, sent.Summary, truncate(diag, 400)),
	)
	if err != nil {
		res.Error = err.Error()
		res.Status = "failed"
		res.FinishedAt = time.Now().UTC()
		r.finishMetrics(&res)
		emit()
		return res
	}
	res.Strategy = strat
	logStage(StageStrategy, "PR strategy drafted", t0)

	// 5. Legal counsel
	t0 = time.Now().UTC()
	if r.metrics != nil {
		r.metrics.LLMCalls.Add(1)
	}
	legal, err := r.llm.Complete(llmCtx,
		"You are legal and compliance counsel reviewing crisis communications.",
		fmt.Sprintf("PR plan:\n%s\n\nSummarize legal risks (low/med/high) and compliance recommendations.", truncate(strat, 600)),
	)
	if err != nil {
		res.Error = err.Error()
		res.Status = "failed"
		res.FinishedAt = time.Now().UTC()
		r.finishMetrics(&res)
		emit()
		return res
	}
	res.LegalReview = legal
	logStage(StageLegal, "Legal review complete", t0)

	// 6. Approval gate
	t0 = time.Now().UTC()
	proposed := fmt.Sprintf("Execute comms + tech playbook for risk %d: %s", sent.RiskScore, sent.Recommended)
	approvalID := runID + "-action"
	dec := r.gate.Request(ctx, approvalID, proposed)
	res.Approval = dec
	logStage(StageApproval, fmt.Sprintf("Approval by %s (approved=%v)", dec.By, dec.Approved), t0)

	if !dec.Approved {
		res.ActionStub = "No action taken (rejected at approval gate)"
		res.Status = "completed"
		res.FinishedAt = time.Now().UTC()
		r.finishMetrics(&res)
		emit()
		return res
	}

	// 7. Action coordinator stub
	t0 = time.Now().UTC()
	res.ActionStub = fmt.Sprintf(
		"[STUB] Dispatch tech_runbook=%q pr_plan=%q legal=%q channels=[status_page,social,email]",
		truncate(diag, 80), truncate(strat, 80), truncate(legal, 80),
	)
	logStage(StageAction, res.ActionStub, t0)

	// 8. Realtime sentiment feedback
	t0 = time.Now().UTC()
	if r.metrics != nil {
		r.metrics.LLMCalls.Add(1)
	}
	fb, _ := r.llm.Complete(llmCtx,
		"You are a sentiment sentinel measuring immediate public reaction.",
		fmt.Sprintf("Actions taken:\n%s\n\nBriefly describe expected short-term sentiment shift.", res.ActionStub),
	)
	res.Feedback = fb
	logStage(StageFeedback, truncate(fb, 120), t0)

	// 9. Crisis status check (single-run: mark unresolved unless low risk)
	t0 = time.Now().UTC()
	resolved := sent.RiskScore < 40
	res.CrisisResolved = resolved
	if resolved {
		res.StatusCheck = "Crisis indicators below threshold; marking resolved for this run."
	} else {
		res.StatusCheck = "Crisis still active; would loop to sentinel in multi-cycle mode."
	}
	logStage(StageStatus, res.StatusCheck, t0)

	// 10. Post-mortem (lightweight)
	t0 = time.Now().UTC()
	if r.metrics != nil {
		r.metrics.LLMCalls.Add(1)
	}
	pm, _ := r.llm.Complete(llmCtx,
		"You are a post-mortem analyst after an incident.",
		fmt.Sprintf("Incident log stages: %d\nSummary: %s\nWrite 3 lessons learned.", len(res.Stages), sent.Summary),
	)
	res.PostMortem = pm
	logStage(StagePostMortem, "Post-mortem drafted", t0)

	res.Status = "completed"
	res.FinishedAt = time.Now().UTC()
	r.finishMetrics(&res)
	emit()
	return res
}

func (r *Runner) finishMetrics(res *RunResult) {
	if r.metrics == nil {
		return
	}
	r.metrics.PipelineRuns.Add(1)
	if res.Error != "" || res.Status == "failed" {
		r.metrics.PipelineErrors.Add(1)
	}
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
