// Package cluster connects the API process and asynq workers over Redis: worker
// SSE events are relayed to the API, and approvals clicked in the API reach the
// worker whose gate is waiting.
package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/approval"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/events"
)

const (
	channelEvents     = "aegis:events"
	channelApprovals  = "aegis:approvals"
	approvalKeyPrefix = "aegis:approval:"
	// approvalKeySlack keeps the pending marker a little past the gate timeout so
	// a crashed worker's markers still expire.
	approvalKeySlack = 30 * time.Second
	opTimeout        = 3 * time.Second
	forwardBuffer    = 256
)

type Bridge struct {
	rdb    *redis.Client
	logger *slog.Logger
}

// New connects to Redis and verifies it is reachable.
func New(redisURL string, logger *slog.Logger) (*Bridge, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("redis url: %w", err)
	}
	return NewWithClient(redis.NewClient(opt), logger)
}

func NewWithClient(rdb *redis.Client, logger *slog.Logger) (*Bridge, error) {
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return &Bridge{rdb: rdb, logger: logger}, nil
}

func (b *Bridge) Client() *redis.Client { return b.rdb }

func (b *Bridge) Close() error { return b.rdb.Close() }

// ForwardHub publishes every event from a worker's hub to Redis. Publishing runs
// on a background goroutine so the pipeline never waits on Redis; if Redis falls
// behind, events are dropped (SSE is best-effort, results live in the run store).
func (b *Bridge) ForwardHub(ctx context.Context, hub *events.Hub) {
	queue := make(chan []byte, forwardBuffer)
	hub.SetForwarder(func(data []byte) {
		select {
		case queue <- data:
		default:
		}
	})
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case data := <-queue:
				pctx, cancel := context.WithTimeout(ctx, opTimeout)
				if err := b.rdb.Publish(pctx, channelEvents, data).Err(); err != nil {
					b.logger.Warn("cluster_forward_event", "err", err)
				}
				cancel()
			}
		}
	}()
}

// RelayToHub delivers events published by workers to the API's local SSE clients.
// It returns once the subscription is active.
func (b *Bridge) RelayToHub(ctx context.Context, hub *events.Hub) error {
	sub := b.rdb.Subscribe(ctx, channelEvents)
	if _, err := sub.Receive(ctx); err != nil {
		sub.Close()
		return fmt.Errorf("subscribe %s: %w", channelEvents, err)
	}
	go func() {
		defer sub.Close()
		ch := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				hub.Broadcast([]byte(msg.Payload))
			}
		}
	}()
	return nil
}

type approvalMsg struct {
	ID       string `json:"id"`
	Approved bool   `json:"approved"`
	Note     string `json:"note"`
}

// AttachGate advertises a worker gate's pending requests in Redis and applies
// decisions published by Resolve. It returns once the subscription is active.
func (b *Bridge) AttachGate(ctx context.Context, gate *approval.Gate) error {
	gate.SetHooks(
		func(id string, timeout time.Duration) {
			c, cancel := context.WithTimeout(ctx, opTimeout)
			defer cancel()
			if err := b.rdb.Set(c, approvalKeyPrefix+id, "1", timeout+approvalKeySlack).Err(); err != nil {
				b.logger.Warn("cluster_approval_pending", "id", id, "err", err)
			}
		},
		func(id string) {
			c, cancel := context.WithTimeout(context.WithoutCancel(ctx), opTimeout)
			defer cancel()
			b.rdb.Del(c, approvalKeyPrefix+id)
		},
	)

	sub := b.rdb.Subscribe(ctx, channelApprovals)
	if _, err := sub.Receive(ctx); err != nil {
		sub.Close()
		return fmt.Errorf("subscribe %s: %w", channelApprovals, err)
	}
	go func() {
		defer sub.Close()
		ch := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var m approvalMsg
				if err := json.Unmarshal([]byte(msg.Payload), &m); err != nil {
					continue
				}
				// Every worker receives the message; only the one holding the request acts.
				_, _ = gate.Resolve(ctx, m.ID, m.Approved, m.Note)
			}
		}
	}()
	return nil
}

// Resolve is called by the API process. Deleting the pending marker is atomic, so
// exactly one caller wins even across API replicas; losers get false (404).
func (b *Bridge) Resolve(ctx context.Context, id string, approved bool, note string) (bool, error) {
	n, err := b.rdb.Del(ctx, approvalKeyPrefix+id).Result()
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	payload, _ := json.Marshal(approvalMsg{ID: id, Approved: approved, Note: note})
	if err := b.rdb.Publish(ctx, channelApprovals, payload).Err(); err != nil {
		return false, err
	}
	return true, nil
}
