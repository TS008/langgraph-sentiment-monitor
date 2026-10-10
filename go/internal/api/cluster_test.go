package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/approval"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/cluster"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/config"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/crawl"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/events"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/llm"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/pipeline"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/queue"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/social"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/store"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/telemetry"
)

// handoffQueue stands in for asynq: Enqueue hands the payload to the worker
// server on a goroutine, as a separate worker process would pick it up.
type handoffQueue struct{ worker *Server }

func (q *handoffQueue) Mode() string { return "asynq" }

func (q *handoffQueue) Enqueue(_ context.Context, p queue.RunPayload) error {
	go func() { _ = q.worker.ProcessQueuePayload(context.Background(), p) }()
	return nil
}

func newBridge(t *testing.T, mr *miniredis.Miniredis) *cluster.Bridge {
	t.Helper()
	b, err := cluster.NewWithClient(redis.NewClient(&redis.Options{Addr: mr.Addr()}), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	return b
}

// TestAsynqModeAPISeesWorkerResults wires an API server and a worker server that
// share nothing but Redis, then drives a run through the API: progress arrives over
// SSE, a human approval clicked in the API reaches the worker's gate, and the
// worker's final result is served by the API.
func TestAsynqModeAPISeesWorkerResults(t *testing.T) {
	mr := miniredis.RunT(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Config{
		QueueMode:         "asynq",
		ApprovalTimeout:   30 * time.Second,
		LLMTimeout:        5 * time.Second,
		MaxConcurrentRuns: 2,
	}

	workerBridge := newBridge(t, mr)
	workerGate := approval.NewGate(cfg.ApprovalTimeout)
	workerHub := events.NewHub()
	if err := workerBridge.AttachGate(ctx, workerGate); err != nil {
		t.Fatal(err)
	}
	workerBridge.ForwardHub(ctx, workerHub)
	reg := &social.Registry{Sources: []social.Source{&social.FixtureSource{Signals: []crawl.Signal{{Source: "f", Title: "outage"}}}}}
	worker := NewServer(cfg, store.NewRedisRunStore(workerBridge.Client(), 50, quiet), workerGate, nil,
		workerHub, telemetry.NewMetrics(), quiet, nil,
		WithRunnerDeps(&pipeline.OrchestratorDeps{Completor: llm.NewStubCompletor(), Social: reg}))

	apiBridge := newBridge(t, mr)
	apiHub := events.NewHub()
	if err := apiBridge.RelayToHub(ctx, apiHub); err != nil {
		t.Fatal(err)
	}
	apiServer := NewServer(cfg, store.NewRedisRunStore(apiBridge.Client(), 50, quiet), approval.NewGate(cfg.ApprovalTimeout),
		&handoffQueue{worker: worker}, apiHub, telemetry.NewMetrics(), quiet, nil, WithApprovalResolver(apiBridge))
	h := apiServer.Router()

	sse, unsub := apiHub.Subscribe()
	defer unsub()

	rec, out := postRun(t, h, `{}`)
	if rec.Code != http.StatusAccepted || out["status"] != "queued" {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	id := out["id"].(string)

	approved := false
	deadline := time.Now().Add(5 * time.Second)
	for !approved && time.Now().Before(deadline) {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/api/v1/approvals/"+id+"-action/approve", strings.NewReader(`{"note":"ship it"}`)))
		approved = r.Code == http.StatusOK
		time.Sleep(10 * time.Millisecond)
	}
	if !approved {
		t.Fatal("approval never became pending via the API")
	}

	var res pipeline.RunResult
	for time.Now().Before(deadline) {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+id, nil))
		_ = json.Unmarshal(r.Body.Bytes(), &res)
		if res.Status == "completed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if res.Status != "completed" {
		t.Fatalf("API never saw the worker's result: status=%q", res.Status)
	}
	if !res.Approval.Approved || res.Approval.By != "human" || res.Approval.Note != "ship it" {
		t.Fatalf("approval=%+v", res.Approval)
	}
	if res.PostMortem == "" || len(res.AgentMessages) == 0 {
		t.Fatal("expected full result from worker")
	}
	if list := apiServer.store.List(5); len(list) != 1 || list[0].ID != id {
		t.Fatalf("list=%+v", list)
	}

	sawStage := false
	for !sawStage {
		select {
		case msg := <-sse:
			sawStage = strings.Contains(string(msg), `"type":"stage"`) && strings.Contains(string(msg), id)
		case <-time.After(2 * time.Second):
			t.Fatal("API SSE clients never received worker stage events")
		}
	}
}

func TestQueuedStatusDoesNotOverwriteFastWorkerResult(t *testing.T) {
	mr := miniredis.RunT(t)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	b := newBridge(t, mr)
	st := store.NewRedisRunStore(b.Client(), 50, quiet)

	// Worker finishes synchronously inside Enqueue: the most adversarial ordering.
	fast := &syncQueue{store: st}
	srv := NewServer(config.Config{QueueMode: "asynq", MaxConcurrentRuns: 1}, st, approval.NewGate(time.Millisecond),
		fast, events.NewHub(), telemetry.NewMetrics(), quiet, nil)
	_, out := postRun(t, srv.Router(), `{}`)
	res, ok := st.Get(out["id"].(string))
	if !ok || res.Status != "completed" {
		t.Fatalf("final result overwritten: %+v", res)
	}
}

type syncQueue struct{ store store.RunStore }

func (q *syncQueue) Mode() string { return "asynq" }
func (q *syncQueue) Enqueue(_ context.Context, p queue.RunPayload) error {
	q.store.Put(pipeline.RunResult{ID: p.RunID, Status: "completed"})
	return nil
}
