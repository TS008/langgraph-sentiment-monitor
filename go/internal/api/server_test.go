package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/approval"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/config"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/crawl"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/events"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/llm"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/pipeline"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/social"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/store"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/telemetry"
)

// gatedCompletor blocks sentiment analysis until release is closed (or ctx ends).
type gatedCompletor struct {
	release chan struct{}
	stub    *llm.StubCompletor
}

func (g *gatedCompletor) AnalyzeSignals(ctx context.Context, corpus string) (llm.SentimentResult, error) {
	select {
	case <-g.release:
		return g.stub.AnalyzeSignals(ctx, corpus)
	case <-ctx.Done():
		return llm.SentimentResult{}, ctx.Err()
	}
}

func (g *gatedCompletor) Complete(ctx context.Context, system, user string) (string, error) {
	return g.stub.Complete(ctx, system, user)
}

func newTestServer(t *testing.T, maxRuns int, opts ...Option) (*Server, *gatedCompletor) {
	t.Helper()
	cfg := config.Config{
		ApprovalTimeout:   time.Millisecond,
		LLMTimeout:        10 * time.Second,
		RunTimeout:        30 * time.Second,
		MaxConcurrentRuns: maxRuns,
	}
	gc := &gatedCompletor{release: make(chan struct{}), stub: llm.NewStubCompletor()}
	reg := &social.Registry{Sources: []social.Source{&social.FixtureSource{Signals: []crawl.Signal{{Source: "f", Title: "outage"}}}}}
	opts = append(opts, WithRunnerDeps(&pipeline.OrchestratorDeps{Completor: gc, Social: reg}))
	srv := NewServer(cfg, store.NewMemoryRunStore(50), approval.NewGate(cfg.ApprovalTimeout), nil,
		events.NewHub(), telemetry.NewMetrics(), slog.New(slog.NewTextHandler(io.Discard, nil)), nil, opts...)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return srv, gc
}

func postRun(t *testing.T, h http.Handler, body string) (*httptest.ResponseRecorder, map[string]interface{}) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/runs", strings.NewReader(body)))
	var out map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func waitStatus(t *testing.T, srv *Server, id, want string) pipeline.RunResult {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if res, ok := srv.store.Get(id); ok && res.Status == want {
			return res
		}
		time.Sleep(10 * time.Millisecond)
	}
	res, _ := srv.store.Get(id)
	t.Fatalf("run %s status=%q, want %q", id, res.Status, want)
	return res
}

func TestAsyncRunReturnsBeforeCompletion(t *testing.T) {
	srv, gc := newTestServer(t, 2)
	h := srv.Router()

	start := time.Now()
	rec, out := postRun(t, h, `{"async":true}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	if time.Since(start) > time.Second {
		t.Fatal("async POST should not wait for the run")
	}
	id := out["id"].(string)
	if res, _ := srv.store.Get(id); res.Status == "completed" {
		t.Fatal("run should still be in progress")
	}
	close(gc.release)
	waitStatus(t, srv, id, "completed")
}

func TestConcurrentRunLimitReturns429(t *testing.T) {
	srv, gc := newTestServer(t, 1)
	h := srv.Router()

	rec, out := postRun(t, h, `{"async":true}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("first run: code=%d", rec.Code)
	}
	rec, _ = postRun(t, h, `{"async":true}`)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second run: code=%d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("429 should set Retry-After")
	}

	close(gc.release)
	waitStatus(t, srv, out["id"].(string), "completed")
	deadline := time.Now().Add(2 * time.Second)
	for {
		rec, _ = postRun(t, h, `{"async":true}`)
		if rec.Code == http.StatusAccepted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("slot not released: code=%d", rec.Code)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSyncRunSurvivesClientDisconnect(t *testing.T) {
	srv, gc := newTestServer(t, 2)
	h := srv.Router()

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", strings.NewReader(`{}`)).WithContext(ctx)
	rec := httptest.NewRecorder()
	handlerDone := make(chan struct{})
	go func() {
		h.ServeHTTP(rec, req)
		close(handlerDone)
	}()

	var id string
	deadline := time.Now().Add(2 * time.Second)
	for id == "" && time.Now().Before(deadline) {
		if runs := srv.store.List(1); len(runs) == 1 {
			id = runs[0].ID
		}
		time.Sleep(5 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("run was not registered")
	}
	cancel()
	<-handlerDone

	close(gc.release)
	waitStatus(t, srv, id, "completed")
}

func TestCreateRunRejectsOversizedBody(t *testing.T) {
	srv, _ := newTestServer(t, 1)
	body := `{"html_url":"` + strings.Repeat("a", maxRunRequestBytes) + `"}`
	rec, _ := postRun(t, srv.Router(), body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d, want 400", rec.Code)
	}
	if srv.store.Len() != 0 {
		t.Fatal("rejected request must not create a run")
	}
}

func TestCreateRunRejectsInternalHTMLURL(t *testing.T) {
	srv, _ := newTestServer(t, 1)
	for _, u := range []string{"http://169.254.169.254/latest/meta-data/", "http://127.0.0.1:8080/", "file:///etc/passwd"} {
		rec, _ := postRun(t, srv.Router(), `{"async":true,"html_url":"`+u+`"}`)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "html_url rejected") {
			t.Fatalf("%s: code=%d body=%s", u, rec.Code, rec.Body)
		}
	}
	if srv.store.Len() != 0 {
		t.Fatal("rejected URLs must not create runs")
	}
}

func TestSSESendsHeartbeat(t *testing.T) {
	srv, _ := newTestServer(t, 1, WithSSEHeartbeat(20*time.Millisecond))
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/commander/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if sc.Text() == ": ping" {
			return
		}
	}
	t.Fatal("no heartbeat received")
}

func TestShutdownCancelsRunsAfterDeadline(t *testing.T) {
	srv, _ := newTestServer(t, 1)
	_, out := postRun(t, srv.Router(), `{"async":true}`)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := srv.Shutdown(ctx); err == nil {
		t.Fatal("expected deadline error while a run is blocked")
	}
	res, _ := srv.store.Get(out["id"].(string))
	if res.Status != "failed" {
		t.Fatalf("cancelled run status=%q, want failed", res.Status)
	}
}
