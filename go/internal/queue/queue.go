package queue

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/config"
)

const TaskRunPipeline = "aegis:run_pipeline"

// RunPayload is serialized for asynq workers.
type RunPayload struct {
	RunID    string `json:"run_id"`
	HTMLURL  string `json:"html_url"`
	JSRender bool   `json:"js_render"`
}

// Enqueuer abstracts in-process vs Redis-backed execution.
type Enqueuer interface {
	Mode() string
	Enqueue(ctx context.Context, p RunPayload) error
}

// MemoryQueue runs jobs synchronously in the caller goroutine (dev/CI default).
type MemoryQueue struct {
	handler func(context.Context, RunPayload) error
}

func NewMemoryQueue(handler func(context.Context, RunPayload) error) *MemoryQueue {
	return &MemoryQueue{handler: handler}
}

func (m *MemoryQueue) Mode() string { return "memory" }

func (m *MemoryQueue) Enqueue(ctx context.Context, p RunPayload) error {
	return m.handler(ctx, p)
}

// AsynqQueue pushes to Redis; requires `aegis worker`.
type AsynqQueue struct {
	client *asynq.Client
}

func NewAsynqQueue(cfg config.Config) (*AsynqQueue, error) {
	opt, err := asynq.ParseRedisURI(cfg.RedisURL)
	if err != nil {
		return nil, err
	}
	return &AsynqQueue{client: asynq.NewClient(opt)}, nil
}

func (q *AsynqQueue) Mode() string { return "asynq" }

func (q *AsynqQueue) Enqueue(ctx context.Context, p RunPayload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	task := asynq.NewTask(TaskRunPipeline, body)
	_, err = q.client.EnqueueContext(ctx, task)
	return err
}

func (q *AsynqQueue) Close() error {
	return q.client.Close()
}

// NewWorkerServer processes asynq tasks.
func NewWorkerServer(cfg config.Config, handler func(context.Context, RunPayload) error) (*asynq.Server, *asynq.ServeMux, error) {
	opt, err := asynq.ParseRedisURI(cfg.RedisURL)
	if err != nil {
		return nil, nil, err
	}
	srv := asynq.NewServer(opt, asynq.Config{Concurrency: 4})
	mux := asynq.NewServeMux()
	mux.HandleFunc(TaskRunPipeline, func(ctx context.Context, t *asynq.Task) error {
		var p RunPayload
		if err := json.Unmarshal(t.Payload(), &p); err != nil {
			return err
		}
		return handler(ctx, p)
	})
	return srv, mux, nil
}

// NewEnqueuer picks backend from config; falls back to memory if Redis unavailable.
func NewEnqueuer(cfg config.Config, memHandler func(context.Context, RunPayload) error) (Enqueuer, error) {
	if !cfg.UseAsynq() {
		return NewMemoryQueue(memHandler), nil
	}
	q, err := NewAsynqQueue(cfg)
	if err != nil {
		return nil, fmt.Errorf("asynq: %w (set AEGIS_QUEUE_MODE=memory to skip Redis)", err)
	}
	return q, nil
}
