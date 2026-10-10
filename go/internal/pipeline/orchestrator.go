package pipeline

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/agentbus"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/agents"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/approval"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/casefile"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/config"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/crawl"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/llm"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/rbac"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/social"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/telemetry"
)

// Orchestrator runs crisis stages via typed bus messages and scoped case file.
type Orchestrator struct {
	cfg      config.Config
	ingestor *crawl.Ingestor
	social   *social.Registry
	llm      llm.Completor
	gate     *approval.Gate
	metrics  *telemetry.Metrics
	bus      *agentbus.Bus
	cases    *casefile.Store
	audit    *rbac.AuditLog
}

func NewOrchestrator(cfg config.Config, gate *approval.Gate, metrics *telemetry.Metrics, bus *agentbus.Bus, cases *casefile.Store, audit *rbac.AuditLog, deps *OrchestratorDeps) *Orchestrator {
	ing := crawl.NewIngestor(cfg.CrawlParallelism)
	reg := &social.Registry{
		Sources: []social.Source{
			&social.RSSSource{NameLabel: "rss", Feeds: cfg.RSSFeeds, Ingest: ing},
			&social.StubSource{Platform: "x", EnvVars: []string{"X_API_BEARER_TOKEN", "X_API_KEY"}, Description: "Twitter/X API v2"},
			&social.StubSource{Platform: "weibo", EnvVars: []string{"WEIBO_APP_KEY", "WEIBO_APP_SECRET"}, Description: "Weibo Open API"},
		},
	}
	if bus == nil {
		bus = agentbus.NewBus()
	}
	if cases == nil {
		cases = casefile.NewStore()
	}
	if audit == nil {
		audit = rbac.NewAuditLog()
	}
	completor := llm.Completor(llm.New(cfg))
	if deps != nil {
		if deps.Social != nil {
			reg = deps.Social
		}
		if deps.Completor != nil {
			completor = deps.Completor
		}
	}
	return &Orchestrator{
		cfg: cfg, ingestor: ing, social: reg, llm: completor,
		gate: gate, metrics: metrics, bus: bus, cases: cases, audit: audit,
	}
}

// Bus exposes the run mailbox (for API/UI).
func (o *Orchestrator) Bus() *agentbus.Bus { return o.bus }

// Execute runs the multi-agent graph once.
func (o *Orchestrator) Execute(ctx context.Context, opts RunOptions, onProgress OnProgress) RunResult {
	runID := opts.RunID
	if runID == "" {
		runID = uuid.NewString()
	}
	useJS := opts.JSRender || o.cfg.UseJSRender

	rt := &agents.Runtime{RunID: runID, Bus: o.bus, Case: o.cases, Audit: o.audit}
	o.cases.Init(runID)

	res := RunResult{
		ID: runID, Status: "running", StartedAt: time.Now().UTC(),
		Stages: []StageLog{}, Checkpoint: string(agentbus.AgentOrchestrator),
	}
	emit := func() {
		res.AgentMessages = o.bus.History(runID)
		res.AuditLog = o.audit.List(runID)
		if st := o.cases.Get(runID); st != nil {
			res.Checkpoint = string(st.Checkpoint)
		}
		if onProgress != nil {
			onProgress(res)
		}
	}
	emit()

	logStage := func(stage, msg string, t0 time.Time) {
		res.Stages = append(res.Stages, StageLog{Stage: stage, Message: msg, StartedAt: t0, EndedAt: time.Now().UTC()})
		emit()
	}

	o.bus.Publish(agentbus.NewEnvelope(runID, agentbus.KindTask, agentbus.AgentOrchestrator, agentbus.AgentSentinel,
		"crisis_monitor", "Begin public signal ingest", nil))

	// --- Sentinel: ingest ---
	t0 := time.Now().UTC()
	rt.RecordTool(agentbus.AgentSentinel, rbac.ToolCrawlPublic, "rss/html ingest")
	if err := rbacAllow(agentbus.AgentSentinel, rbac.ToolCrawlPublic); err != nil {
		res.Error = err.Error()
		res.Status = "failed"
		logStage(StageMonitor, res.Error, t0)
		return finish(o, &res, emit)
	}
	signals, notes := o.social.Collect(ctx)
	if extra := opts.HTMLURL; extra != "" {
		sig, err := o.ingestor.FetchPage(ctx, extra, useJS)
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
		return finish(o, &res, emit)
	}
	res.Signals = signals
	res.IngestNotes = notes
	if o.metrics != nil {
		o.metrics.CrawlSignals.Add(uint64(len(signals)))
	}
	artID, art := o.cases.Put(runID, casefile.SlotSignals, fmt.Sprintf("%d signals", len(signals)), signals, agentbus.AgentSentinel)
	_ = rt.PublishArtifact(agentbus.AgentSentinel, art)
	o.cases.Checkpoint(runID, agentbus.AgentSentinel)
	logStage(StageMonitor, fmt.Sprintf("Ingested %d signals", len(signals)), t0)
	_ = rt.PublishHandoff(agentbus.AgentSentinel, agentbus.AgentSentiment, "signals:"+artID, []string{artID})

	llmCtx, cancel := context.WithTimeout(ctx, o.cfg.LLMTimeout)
	defer cancel()
	llmCtx = llm.WithSession(llmCtx, runID)
	corpus := buildCorpus(signals)

	// --- Sentiment ---
	t0 = time.Now().UTC()
	o.bus.Publish(agentbus.NewEnvelope(runID, agentbus.KindTask, agentbus.AgentOrchestrator, agentbus.AgentSentiment, "analyze", "Score risk", []string{artID}))
	rt.RecordTool(agentbus.AgentSentiment, rbac.ToolLLMInfer, "sentiment")
	view := o.cases.ScopedView(runID, agentbus.AgentSentiment)
	promptCtx := casefile.HandoffSummary(view, 1200)
	if promptCtx == "" {
		promptCtx = corpus
	}
	if o.metrics != nil {
		o.metrics.LLMCalls.Add(1)
	}
	sent, err := o.llm.AnalyzeSignals(llmCtx, promptCtx+"\n"+corpus)
	if err != nil {
		res.Error = err.Error()
		res.Status = "failed"
		logStage(StageSentiment, err.Error(), t0)
		return finish(o, &res, emit)
	}
	res.Sentiment = sent
	sid, sart := o.cases.Put(runID, casefile.SlotSentiment, fmt.Sprintf("risk=%d %s", sent.RiskScore, sent.Sentiment), sent, agentbus.AgentSentiment)
	_ = rt.PublishArtifact(agentbus.AgentSentiment, sart)
	o.cases.Checkpoint(runID, agentbus.AgentSentiment)
	logStage(StageSentiment, fmt.Sprintf("Risk %d", sent.RiskScore), t0)
	_ = rt.PublishHandoff(agentbus.AgentSentiment, agentbus.AgentDiagnostician, sent.Summary, []string{sid})

	// --- Diagnostician ---
	t0 = time.Now().UTC()
	o.bus.Publish(agentbus.NewEnvelope(runID, agentbus.KindTask, agentbus.AgentOrchestrator, agentbus.AgentDiagnostician, "diagnose", "Root cause", []string{sid}))
	rt.RecordTool(agentbus.AgentDiagnostician, rbac.ToolLLMInfer, "diagnosis")
	diagView := casefile.HandoffSummary(o.cases.ScopedView(runID, agentbus.AgentDiagnostician), 1500)
	if o.metrics != nil {
		o.metrics.LLMCalls.Add(1)
	}
	diag, err := o.llm.Complete(llmCtx, "Chief technical diagnostician.", diagView+"\nProvide root-cause hypotheses and mitigation.")
	if err != nil {
		res.Error = err.Error()
		res.Status = "failed"
		return finish(o, &res, emit)
	}
	res.Diagnosis = diag
	did, dart := o.cases.Put(runID, casefile.SlotDiagnosis, truncate(diag, 200), diag, agentbus.AgentDiagnostician)
	_ = rt.PublishArtifact(agentbus.AgentDiagnostician, dart)
	o.cases.Checkpoint(runID, agentbus.AgentDiagnostician)
	logStage(StageDiagnose, "Diagnosis complete", t0)
	_ = rt.PublishHandoff(agentbus.AgentDiagnostician, agentbus.AgentStrategist, truncate(diag, 120), []string{did})

	// --- Strategist ---
	t0 = time.Now().UTC()
	o.bus.Publish(agentbus.NewEnvelope(runID, agentbus.KindTask, agentbus.AgentOrchestrator, agentbus.AgentStrategist, "strategy", "PR plan", []string{did}))
	rt.RecordTool(agentbus.AgentStrategist, rbac.ToolLLMInfer, "strategy")
	stratView := casefile.HandoffSummary(o.cases.ScopedView(runID, agentbus.AgentStrategist), 1500)
	if o.metrics != nil {
		o.metrics.LLMCalls.Add(1)
	}
	strat, err := o.llm.Complete(llmCtx, "Senior PR strategist.", stratView+"\nDraft 3-bullet crisis comms plan.")
	if err != nil {
		res.Error = err.Error()
		res.Status = "failed"
		return finish(o, &res, emit)
	}
	res.Strategy = strat
	stid, start := o.cases.Put(runID, casefile.SlotStrategy, truncate(strat, 200), strat, agentbus.AgentStrategist)
	_ = rt.PublishArtifact(agentbus.AgentStrategist, start)
	o.cases.Checkpoint(runID, agentbus.AgentStrategist)
	logStage(StageStrategy, "Strategy drafted", t0)
	_ = rt.PublishHandoff(agentbus.AgentStrategist, agentbus.AgentLegal, truncate(strat, 120), []string{stid})

	// --- Legal ---
	t0 = time.Now().UTC()
	o.bus.Publish(agentbus.NewEnvelope(runID, agentbus.KindTask, agentbus.AgentOrchestrator, agentbus.AgentLegal, "legal", "Review comms", []string{stid}))
	rt.RecordTool(agentbus.AgentLegal, rbac.ToolLLMInfer, "legal")
	legView := casefile.HandoffSummary(o.cases.ScopedView(runID, agentbus.AgentLegal), 1500)
	if o.metrics != nil {
		o.metrics.LLMCalls.Add(1)
	}
	legal, err := o.llm.Complete(llmCtx, "Legal counsel.", legView+"\nSummarize legal risk and compliance.")
	if err != nil {
		res.Error = err.Error()
		res.Status = "failed"
		return finish(o, &res, emit)
	}
	res.LegalReview = legal
	lid, lart := o.cases.Put(runID, casefile.SlotLegal, truncate(legal, 200), legal, agentbus.AgentLegal)
	_ = rt.PublishArtifact(agentbus.AgentLegal, lart)
	o.cases.Checkpoint(runID, agentbus.AgentLegal)
	logStage(StageLegal, "Legal review done", t0)
	_ = rt.PublishHandoff(agentbus.AgentLegal, agentbus.AgentGateway, "ready for approval", []string{lid})

	// --- Approval gateway ---
	t0 = time.Now().UTC()
	proposed := fmt.Sprintf("Execute playbook risk=%d: %s", sent.RiskScore, sent.Recommended)
	approvalID := runID + "-action"
	rt.RecordTool(agentbus.AgentGateway, rbac.ToolRequestApproval, proposed)
	o.bus.Publish(agentbus.NewEnvelope(runID, agentbus.KindTask, agentbus.AgentOrchestrator, agentbus.AgentGateway, "approve", proposed, []string{lid}))
	dec := o.gate.Request(ctx, approvalID, proposed)
	res.Approval = dec
	_, aart := o.cases.Put(runID, casefile.SlotApproval, fmt.Sprintf("approved=%v by %s", dec.Approved, dec.By), dec, agentbus.AgentGateway)
	_ = rt.PublishArtifact(agentbus.AgentGateway, aart)
	rt.PublishDecision(agentbus.AgentGateway, agentbus.AgentAction, "approval", fmt.Sprintf("%v via %s", dec.Approved, dec.By))
	logStage(StageApproval, fmt.Sprintf("Approval %v (%s)", dec.Approved, dec.By), t0)

	if !dec.Approved {
		res.ActionStub = "No action (rejected)"
		res.Status = "completed"
		return finish(o, &res, emit)
	}

	// --- Action (RBAC + stub dispatch) ---
	t0 = time.Now().UTC()
	rt.RecordTool(agentbus.AgentAction, rbac.ToolExternalDispatch, "dispatch stub")
	if err := rbacAllow(agentbus.AgentAction, rbac.ToolExternalDispatch); err != nil {
		res.Error = err.Error()
		res.Status = "failed"
		logStage(StageAction, res.Error, t0)
		return finish(o, &res, emit)
	}
	res.ActionStub = fmt.Sprintf("[STUB] dispatch tech=%q pr=%q legal=%q", truncate(diag, 60), truncate(strat, 60), truncate(legal, 60))
	_, actArt := o.cases.Put(runID, casefile.SlotAction, res.ActionStub, res.ActionStub, agentbus.AgentAction)
	_ = rt.PublishArtifact(agentbus.AgentAction, actArt)
	o.cases.Checkpoint(runID, agentbus.AgentAction)
	logStage(StageAction, res.ActionStub, t0)
	_ = rt.PublishHandoff(agentbus.AgentAction, agentbus.AgentFeedback, "measure sentiment", []string{actArt.ID})

	// --- Feedback ---
	t0 = time.Now().UTC()
	if o.metrics != nil {
		o.metrics.LLMCalls.Add(1)
	}
	fb, _ := o.llm.Complete(llmCtx, "Sentiment sentinel.", "Actions: "+res.ActionStub+"\nExpected short-term sentiment shift?")
	res.Feedback = fb
	_, fbArt := o.cases.Put(runID, casefile.SlotFeedback, truncate(fb, 200), fb, agentbus.AgentFeedback)
	_ = rt.PublishArtifact(agentbus.AgentFeedback, fbArt)
	logStage(StageFeedback, truncate(fb, 120), t0)

	// --- Status ---
	t0 = time.Now().UTC()
	resolved := sent.RiskScore < 40
	res.CrisisResolved = resolved
	if resolved {
		res.StatusCheck = "Below threshold; resolved this run."
	} else {
		res.StatusCheck = "Still active; would loop in multi-cycle mode."
	}
	_, stArt := o.cases.Put(runID, casefile.SlotStatus, res.StatusCheck, res.StatusCheck, agentbus.AgentStatus)
	_ = rt.PublishArtifact(agentbus.AgentStatus, stArt)
	o.cases.Checkpoint(runID, agentbus.AgentStatus)
	logStage(StageStatus, res.StatusCheck, t0)
	_ = rt.PublishHandoff(agentbus.AgentStatus, agentbus.AgentPostMortem, res.StatusCheck, []string{stArt.ID})

	// --- Post-mortem ---
	t0 = time.Now().UTC()
	if o.metrics != nil {
		o.metrics.LLMCalls.Add(1)
	}
	pm, _ := o.llm.Complete(llmCtx, "Post-mortem analyst.", "Summary: "+sent.Summary+"\nThree lessons learned.")
	res.PostMortem = pm
	_, pmArt := o.cases.Put(runID, casefile.SlotPostMortem, truncate(pm, 200), pm, agentbus.AgentPostMortem)
	_ = rt.PublishArtifact(agentbus.AgentPostMortem, pmArt)
	o.cases.Checkpoint(runID, agentbus.AgentPostMortem)
	logStage(StagePostMortem, "Post-mortem complete", t0)

	res.Status = "completed"
	return finish(o, &res, emit)
}

func finish(o *Orchestrator, res *RunResult, emit func()) RunResult {
	res.FinishedAt = time.Now().UTC()
	res.AgentMessages = o.bus.History(res.ID)
	res.AuditLog = o.audit.List(res.ID)
	if o.metrics != nil {
		o.metrics.PipelineRuns.Add(1)
		if res.Error != "" || res.Status == "failed" {
			o.metrics.PipelineErrors.Add(1)
		}
	}
	emit()
	return *res
}

func rbacAllow(agent agentbus.AgentID, tool rbac.Tool) error {
	if !rbac.Allow(agent, tool) {
		return fmt.Errorf("rbac: agent %s denied tool %s", agent, tool)
	}
	return nil
}
