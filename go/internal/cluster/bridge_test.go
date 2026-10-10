package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/approval"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/events"
)

// newPair returns two bridges on separate connections to one Redis, standing in
// for the API process and a worker process.
func newPair(t *testing.T) (api, worker *Bridge) {
	t.Helper()
	mr := miniredis.RunT(t)
	mk := func() *Bridge {
		b, err := NewWithClient(redis.NewClient(&redis.Options{Addr: mr.Addr()}), nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { b.Close() })
		return b
	}
	return mk(), mk()
}

func TestWorkerEventsReachAPIHub(t *testing.T) {
	apiBridge, workerBridge := newPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	apiHub, workerHub := events.NewHub(), events.NewHub()
	if err := apiBridge.RelayToHub(ctx, apiHub); err != nil {
		t.Fatal(err)
	}
	workerBridge.ForwardHub(ctx, workerHub)

	ch, unsub := apiHub.Subscribe()
	defer unsub()
	workerHub.Publish(events.Event{Type: "stage", RunID: "r1", Stage: "sentiment_analysis"})

	select {
	case msg := <-ch:
		if string(msg) != `{"type":"stage","run_id":"r1","stage":"sentiment_analysis"}` {
			t.Fatalf("msg=%s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker event never reached API hub")
	}
}

func TestApprovalFromAPIResolvesWorkerGate(t *testing.T) {
	apiBridge, workerBridge := newPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gate := approval.NewGate(10 * time.Second)
	if err := workerBridge.AttachGate(ctx, gate); err != nil {
		t.Fatal(err)
	}

	if ok, err := apiBridge.Resolve(ctx, "r1-action", true, ""); err != nil || ok {
		t.Fatalf("nothing pending yet: ok=%v err=%v", ok, err)
	}

	got := make(chan approval.Decision, 1)
	go func() { got <- gate.Request(ctx, "r1-action", "deploy fix") }()

	var ok bool
	deadline := time.Now().Add(2 * time.Second)
	for !ok && time.Now().Before(deadline) {
		var err error
		if ok, err = apiBridge.Resolve(ctx, "r1-action", false, "too risky"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !ok {
		t.Fatal("pending approval never advertised")
	}
	if again, _ := apiBridge.Resolve(ctx, "r1-action", true, ""); again {
		t.Fatal("second resolve must lose")
	}

	select {
	case d := <-got:
		if d.Approved || d.By != "human" || d.Note != "too risky" {
			t.Fatalf("decision=%+v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker gate never received the decision")
	}
}

func TestTimedOutApprovalIsNoLongerResolvable(t *testing.T) {
	apiBridge, workerBridge := newPair(t)
	ctx := context.Background()
	gate := approval.NewGate(20 * time.Millisecond)
	if err := workerBridge.AttachGate(ctx, gate); err != nil {
		t.Fatal(err)
	}
	if d := gate.Request(ctx, "r2-action", "x"); d.By != "auto-timeout" {
		t.Fatalf("decision=%+v", d)
	}
	if ok, _ := apiBridge.Resolve(ctx, "r2-action", true, ""); ok {
		t.Fatal("timed-out request should not be resolvable")
	}
}
