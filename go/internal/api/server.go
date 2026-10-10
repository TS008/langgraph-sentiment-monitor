package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/agentbus"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/approval"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/config"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/events"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/pipeline"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/queue"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/store"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/telemetry"
)

// Server exposes HTTP API and commander UI.
type Server struct {
	cfg     config.Config
	store   *store.MemoryRunStore
	runner  *pipeline.Runner
	gate    *approval.Gate
	queue   queue.Enqueuer
	hub     *events.Hub
	metrics *telemetry.Metrics
	logger  *slog.Logger
	static  fs.FS
}

func NewServer(
	cfg config.Config,
	st *store.MemoryRunStore,
	gate *approval.Gate,
	q queue.Enqueuer,
	hub *events.Hub,
	metrics *telemetry.Metrics,
	logger *slog.Logger,
	static fs.FS,
) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	srv := &Server{
		cfg:     cfg,
		store:   st,
		gate:    gate,
		queue:   q,
		hub:     hub,
		metrics: metrics,
		logger:  logger,
		runner:  pipeline.NewRunner(cfg, gate, metrics),
		static:  static,
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
	r.Use(middleware.Timeout(180 * time.Second))

	if s.static != nil {
		r.Get("/", s.handleCommander)
		r.Get("/commander", s.handleCommander)
		r.Get("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(s.static))).ServeHTTP)
	}
	r.Get("/commander/events", s.handleSSE)

	r.Get("/health", s.handleHealth)
	r.Get("/healthz", s.handleHealth)
	r.Handle("/metrics", s.metrics.Handler())
	r.Post("/api/v1/runs", s.handleCreateRun)
	r.Get("/api/v1/runs/{id}", s.handleGetRun)
	r.Get("/api/v1/runs", s.handleListRuns)
	r.Post("/api/v1/approvals/{id}/approve", s.handleApprove)
	r.Post("/api/v1/approvals/{id}/reject", s.handleReject)

	return r
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
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, unsub := s.hub.Subscribe()
	defer unsub()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case msg := <-ch:
			_, _ = fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "ok",
		"llm_enabled": s.cfg.LLMEnabled(),
		"llm_model":   s.cfg.LLMModel,
		"queue_mode":  s.queue.Mode(),
		"time":        time.Now().UTC(),
	})
}

type createRunRequest struct {
	HTMLURL  string `json:"html_url"`
	JSRender bool   `json:"js_render"`
	Async    bool   `json:"async"`
}

func (s *Server) handleCreateRun(w http.ResponseWriter, r *http.Request) {
	var req createRunRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	runID := pipeline.NewRunID()
	payload := queue.RunPayload{
		RunID:    runID,
		HTMLURL:  req.HTMLURL,
		JSRender: req.JSRender,
	}

	pending := pipeline.RunResult{
		ID:        runID,
		Status:    "pending",
		StartedAt: time.Now().UTC(),
	}
	s.store.Put(pending)
	s.hub.Publish(events.Event{Type: "run_started", RunID: runID})

	ctx := r.Context()
	if req.Async || s.queue.Mode() == "asynq" {
		if err := s.queue.Enqueue(ctx, payload); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"id": runID, "status": "queued"})
		return
	}

	result := s.executeRun(ctx, payload)
	status := http.StatusOK
	if result.Error != "" {
		status = http.StatusAccepted
	}
	writeJSON(w, status, result)
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

func (s *Server) handleListRuns(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.store.List())
}

func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct{ Note string `json:"note"` }
	_ = json.NewDecoder(r.Body).Decode(&body)
	if !s.gate.Approve(id, body.Note) {
		http.Error(w, "no pending approval", http.StatusNotFound)
		return
	}
	s.hub.Publish(events.Event{Type: "approval", RunID: id, Message: "approved"})
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

func (s *Server) handleReject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct{ Note string `json:"note"` }
	_ = json.NewDecoder(r.Body).Decode(&body)
	if !s.gate.Reject(id, body.Note) {
		http.Error(w, "no pending approval", http.StatusNotFound)
		return
	}
	s.hub.Publish(events.Event{Type: "approval", RunID: id, Message: "rejected"})
	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
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
