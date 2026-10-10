package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/approval"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/config"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/pipeline"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/store"
)

// Server exposes health and pipeline trigger endpoints.
type Server struct {
	cfg    config.Config
	store  *store.MemoryRunStore
	runner *pipeline.Runner
	gate   *approval.Gate
}

func NewServer(cfg config.Config, st *store.MemoryRunStore, gate *approval.Gate) *Server {
	return &Server{
		cfg:    cfg,
		store:  st,
		gate:   gate,
		runner: pipeline.NewRunner(cfg, gate),
	}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(120 * time.Second))

	r.Get("/health", s.handleHealth)
	r.Post("/api/v1/runs", s.handleCreateRun)
	r.Get("/api/v1/runs/{id}", s.handleGetRun)
	r.Get("/api/v1/runs", s.handleListRuns)
	r.Post("/api/v1/approvals/{id}/approve", s.handleApprove)
	r.Post("/api/v1/approvals/{id}/reject", s.handleReject)

	return r
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":     "ok",
		"llm_enabled": s.cfg.LLMEnabled(),
		"time":       time.Now().UTC(),
	})
}

type createRunRequest struct {
	HTMLURL string `json:"html_url"`
}

func (s *Server) handleCreateRun(w http.ResponseWriter, r *http.Request) {
	var req createRunRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	ctx := r.Context()
	result := s.runner.Run(ctx, req.HTMLURL)
	s.store.Put(result)
	status := http.StatusOK
	if result.Error != "" {
		status = http.StatusAccepted
	}
	writeJSON(w, status, result)
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
	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// RunOnce is used by CLI without HTTP.
func (s *Server) RunOnce(ctx context.Context, htmlURL string) pipeline.RunResult {
	result := s.runner.Run(ctx, htmlURL)
	s.store.Put(result)
	return result
}
