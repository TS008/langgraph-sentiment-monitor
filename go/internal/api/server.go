package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/agentbus"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/approval"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/config"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/events"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/netguard"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/pipeline"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/queue"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/store"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/telemetry"
)

const (
	maxRunRequestBytes      = 64 << 10
	maxApprovalRequestBytes = 16 << 10
	defaultSSEHeartbeat     = 20 * time.Second
	defaultRunTimeout       = 15 * time.Minute
	apiRequestTimeout       = 30 * time.Second
)

// Server exposes HTTP API and commander UI.
type Server struct {
	cfg       config.Config
	store     store.RunStore
	runner    *pipeline.Runner
	gate      *approval.Gate
	approvals ApprovalResolver
	queue     queue.Enqueuer
	hub       *events.Hub
	metrics   *telemetry.Metrics
	logger    *slog.Logger
	static    fs.FS

	fetchPolicy  netguard.Policy
	runSem       chan struct{}
	sseHeartbeat time.Duration
	streamsDone  chan struct{}
	closeStreams sync.Once
	bgCtx        context.Context
	bgCancel     context.CancelFunc
	bgWG         sync.WaitGroup
}

// Option customizes a Server.
type Option func(*serverOptions)

type serverOptions struct {
	deps         *pipeline.OrchestratorDeps
	sseHeartbeat time.Duration
	approvals    ApprovalResolver
}

// ApprovalResolver applies a human decision to a pending approval request.
// It reports false when no such request is pending.
type ApprovalResolver interface {
	Resolve(ctx context.Context, id string, approved bool, note string) (bool, error)
}

// WithApprovalResolver routes approve/reject calls elsewhere (e.g. to the asynq
// worker holding the request). Defaults to the server's own gate.
func WithApprovalResolver(r ApprovalResolver) Option {
	return func(o *serverOptions) { o.approvals = r }
}

// WithRunnerDeps injects pipeline collaborators (tests, custom sources).
func WithRunnerDeps(deps *pipeline.OrchestratorDeps) Option {
	return func(o *serverOptions) { o.deps = deps }
}

// WithSSEHeartbeat overrides the SSE keep-alive interval.
func WithSSEHeartbeat(d time.Duration) Option {
	return func(o *serverOptions) { o.sseHeartbeat = d }
}

func NewServer(
	cfg config.Config,
	st store.RunStore,
	gate *approval.Gate,
	q queue.Enqueuer,
	hub *events.Hub,
	metrics *telemetry.Metrics,
	logger *slog.Logger,
	static fs.FS,
	opts ...Option,
) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	var o serverOptions
	for _, opt := range opts {
		opt(&o)
	}
	if o.sseHeartbeat <= 0 {
		o.sseHeartbeat = defaultSSEHeartbeat
	}
	if o.approvals == nil {
		o.approvals = gate
	}
	maxRuns := cfg.MaxConcurrentRuns
	if maxRuns < 1 {
		maxRuns = 1
	}
	policy, _ := cfg.FetchPolicy()
	bgCtx, bgCancel := context.WithCancel(context.Background())
	srv := &Server{
		fetchPolicy:  policy,
		cfg:          cfg,
		store:        st,
		gate:         gate,
		approvals:    o.approvals,
		queue:        q,
		hub:          hub,
		metrics:      metrics,
		logger:       logger,
		runner:       pipeline.NewRunnerWithDeps(cfg, gate, metrics, o.deps),
		static:       static,
		runSem:       make(chan struct{}, maxRuns),
		sseHeartbeat: o.sseHeartbeat,
		streamsDone:  make(chan struct{}),
		bgCtx:        bgCtx,
		bgCancel:     bgCancel,
	}
	srv.wireAgentBus()
	return srv
}

func (s *Server) wireAgentBus() {
	s.runner.Bus().OnPublish(func(env agentbus.Envelope) {
		s.hub.Publish(events.Event{
			Type:    "agent_message",
			RunID:   env.RunID,
			Stage:   string(env.Kind) + ":" + string(env.From),
			Message: env.Subject + " → " + string(env.To) + ": " + env.Body,
		})
	})
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(telemetry.SlogMiddleware(s.logger, s.metrics))

	// Long-lived: SSE streams indefinitely; run creation waits on a detached run.
	r.Get("/commander/events", s.handleSSE)
	r.Post("/api/v1/runs", s.handleCreateRun)

	r.Group(func(r chi.Router) {
		r.Use(middleware.Timeout(apiRequestTimeout))
		if s.static != nil {
			r.Get("/", s.handleCommander)
			r.Get("/commander", s.handleCommander)
			r.Get("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(s.static))).ServeHTTP)
		}
		r.Get("/health", s.handleHealth)
		r.Get("/healthz", s.handleHealth)
		r.Handle("/metrics", s.metrics.Handler())
		r.Get("/api/v1/runs/{id}", s.handleGetRun)
		r.Get("/api/v1/runs", s.handleListRuns)
		r.Post("/api/v1/approvals/{id}/approve", s.handleApprove)
		r.Post("/api/v1/approvals/{id}/reject", s.handleReject)
	})

	return r
}

// CloseStreams ends open SSE connections; register with http.Server.RegisterOnShutdown.
func (s *Server) CloseStreams() {
	s.closeStreams.Do(func() { close(s.streamsDone) })
}

// Shutdown waits for in-flight runs until ctx expires, then cancels them.
func (s *Server) Shutdown(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.bgWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		s.bgCancel()
		return nil
	case <-ctx.Done():
		s.bgCancel()
		<-done
		return ctx.Err()
	}
}

func (s *Server) handleCommander(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(s.static, "commander.html")
	if err != nil {
		http.Error(w, "ui not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, unsub := s.hub.Subscribe()
	defer unsub()

	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	// Proxies (Cloudflare included) drop idle streams; comment frames keep them open.
	heartbeat := time.NewTicker(s.sseHeartbeat)
	defer heartbeat.Stop()

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case <-s.streamsDone:
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", msg); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "ok",
		"llm_enabled": s.cfg.LLMEnabled(),
		"llm_model":   s.cfg.LLMModel,
		"queue_mode":  s.queueMode(),
		"time":        time.Now().UTC(),
	})
}

func (s *Server) queueMode() string {
	if s.queue == nil {
		return "memory"
	}
	return s.queue.Mode()
}

type createRunRequest struct {
	HTMLURL  string `json:"html_url"`
	JSRender bool   `json:"js_render"`
	Async    bool   `json:"async"`
}

func (s *Server) handleCreateRun(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRunRequestBytes)
	var req createRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.HTMLURL != "" {
		if _, err := s.fetchPolicy.ValidateURL(r.Context(), req.HTMLURL); err != nil {
			http.Error(w, "html_url rejected: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	runID := pipeline.NewRunID()
	payload := queue.RunPayload{
		RunID:    runID,
		HTMLURL:  req.HTMLURL,
		JSRender: req.JSRender,
	}

	if s.queueMode() == "asynq" {
		// Record "queued" before enqueueing: a fast worker's final result must not be
		// overwritten by a late "queued" write.
		queued := pipeline.RunResult{ID: runID, Status: "queued", StartedAt: time.Now().UTC()}
		s.store.Put(queued)
		if err := s.queue.Enqueue(r.Context(), payload); err != nil {
			queued.Status, queued.Error, queued.FinishedAt = "failed", "enqueue: "+err.Error(), time.Now().UTC()
			s.store.Put(queued)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.hub.Publish(events.Event{Type: "run_started", RunID: runID})
		writeJSON(w, http.StatusAccepted, map[string]string{"id": runID, "status": "queued"})
		return
	}

	release, ok := s.tryAcquireRun()
	if !ok {
		w.Header().Set("Retry-After", "5")
		http.Error(w, "too many concurrent runs", http.StatusTooManyRequests)
		return
	}
	s.store.Put(pipeline.RunResult{ID: runID, Status: "pending", StartedAt: time.Now().UTC()})
	s.hub.Publish(events.Event{Type: "run_started", RunID: runID})

	done := s.startRun(payload, release)
	if req.Async {
		writeJSON(w, http.StatusAccepted, map[string]string{"id": runID, "status": "running"})
		return
	}

	select {
	case result := <-done:
		status := http.StatusOK
		if result.Error != "" {
			status = http.StatusAccepted
		}
		writeJSON(w, status, result)
	case <-r.Context().Done():
		// Client went away; the run continues and its result stays retrievable via GET.
	}
}

func (s *Server) tryAcquireRun() (release func(), ok bool) {
	select {
	case s.runSem <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-s.runSem }) }, true
	default:
		return nil, false
	}
}

// startRun executes detached from the HTTP request so client disconnects and
// handler timeouts don't cancel a run; AEGIS_RUN_TIMEOUT_SEC bounds it instead.
func (s *Server) startRun(p queue.RunPayload, release func()) <-chan pipeline.RunResult {
	done := make(chan pipeline.RunResult, 1)
	timeout := s.cfg.RunTimeout
	if timeout <= 0 {
		timeout = defaultRunTimeout
	}
	s.bgWG.Add(1)
	go func() {
		defer s.bgWG.Done()
		defer release()
		ctx, cancel := context.WithTimeout(s.bgCtx, timeout)
		defer cancel()
		done <- s.executeRun(ctx, p)
	}()
	return done
}

func (s *Server) executeRun(ctx context.Context, p queue.RunPayload) pipeline.RunResult {
	onProgress := func(res pipeline.RunResult) {
		s.store.Put(res)
		if len(res.Stages) > 0 {
			last := res.Stages[len(res.Stages)-1]
			s.hub.Publish(events.Event{
				Type:    "stage",
				RunID:   res.ID,
				Stage:   last.Stage,
				Message: last.Message,
			})
		}
		if len(res.AuditLog) > 0 {
			last := res.AuditLog[len(res.AuditLog)-1]
			s.hub.Publish(events.Event{
				Type:    "audit",
				RunID:   res.ID,
				Stage:   string(last.Tool),
				Message: fmt.Sprintf("%s allowed=%v %s", last.Agent, last.Allowed, last.Detail),
			})
		}
	}
	opts := pipeline.RunOptions{
		RunID:    p.RunID,
		HTMLURL:  p.HTMLURL,
		JSRender: p.JSRender,
	}
	result := s.runner.Run(ctx, opts, onProgress)
	s.store.Put(result)
	s.hub.Publish(events.Event{Type: "run_finished", RunID: result.ID, Message: result.Status})
	return result
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	res, ok := s.store.Get(id)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

const (
	defaultListLimit = 50
	maxListLimit     = 500
)

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	limit := defaultListLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		limit = min(n, maxListLimit)
	}
	writeJSON(w, http.StatusOK, s.store.List(limit))
}

func decodeApprovalNote(w http.ResponseWriter, r *http.Request) string {
	var body struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, maxApprovalRequestBytes)).Decode(&body)
	return body.Note
}

func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	s.resolveApproval(w, r, true)
}

func (s *Server) handleReject(w http.ResponseWriter, r *http.Request) {
	s.resolveApproval(w, r, false)
}

func (s *Server) resolveApproval(w http.ResponseWriter, r *http.Request, approved bool) {
	id := chi.URLParam(r, "id")
	ok, err := s.approvals.Resolve(r.Context(), id, approved, decodeApprovalNote(w, r))
	if err != nil {
		http.Error(w, "approval relay failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	if !ok {
		http.Error(w, "no pending approval", http.StatusNotFound)
		return
	}
	status := "rejected"
	if approved {
		status = "approved"
	}
	s.hub.Publish(events.Event{Type: "approval", RunID: id, Message: status})
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// RunOnce is used by CLI without HTTP.
func (s *Server) RunOnce(ctx context.Context, htmlURL string, js bool) pipeline.RunResult {
	return s.executeRun(ctx, queue.RunPayload{
		RunID:    pipeline.NewRunID(),
		HTMLURL:  htmlURL,
		JSRender: js,
	})
}

// ProcessQueuePayload is the worker entrypoint.
func (s *Server) ProcessQueuePayload(ctx context.Context, p queue.RunPayload) error {
	s.executeRun(ctx, p)
	return nil
}
