package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/api"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/approval"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/config"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/events"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/queue"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/store"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/telemetry"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		runServe(os.Args[2:])
	case "run":
		runOnce(os.Args[2:])
	case "worker":
		runWorker(os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "AEGIS Go monitor — commands:\n")
	fmt.Fprintf(os.Stderr, "  aegis serve              HTTP API + commander UI\n")
	fmt.Fprintf(os.Stderr, "  aegis run [--url URL]    Run pipeline once (stdout JSON)\n")
	fmt.Fprintf(os.Stderr, "  aegis worker             Asynq worker (requires Redis)\n")
}

func newApp(cfg config.Config) (*api.Server, queue.Enqueuer, *events.Hub, *telemetry.Metrics) {
	st := store.NewMemoryRunStore(cfg.MaxRetainedRuns)
	gate := approval.NewGate(cfg.ApprovalTimeout)
	hub := events.NewHub()
	metrics := telemetry.NewMetrics()
	static, _ := fs.Sub(api.StaticFiles(), "static")
	srv := api.NewServer(cfg, st, gate, nil, hub, metrics, slog.Default(), static)

	q, err := queue.NewEnqueuer(cfg, func(ctx context.Context, p queue.RunPayload) error {
		return srv.ProcessQueuePayload(ctx, p)
	})
	if err != nil {
		log.Fatal(err)
	}
	srv.SetQueue(q)
	return srv, q, hub, metrics
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "", "listen address (overrides AEGIS_HTTP_ADDR)")
	_ = fs.Parse(args)

	cfg := config.Load()
	if *addr != "" {
		cfg.HTTPAddr = *addr
	} else if p := os.Getenv("PORT"); p != "" {
		cfg.HTTPAddr = ":" + p
	}

	srv, q, _, metrics := newApp(cfg)
	if c, ok := q.(*queue.AsynqQueue); ok {
		defer c.Close()
	}

	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("aegis_listen", "addr", cfg.HTTPAddr, "llm", cfg.LLMEnabled(), "queue", q.Mode(), "model", cfg.LLMModel)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	_ = metrics
}

func runWorker(args []string) {
	_ = flag.NewFlagSet("worker", flag.ExitOnError)
	cfg := config.Load()
	if !cfg.UseAsynq() {
		log.Fatal("set AEGIS_QUEUE_MODE=asynq to run worker")
	}
	srv, _, _, _ := newApp(cfg)
	server, mux, err := queue.NewWorkerServer(cfg, srv.ProcessQueuePayload)
	if err != nil {
		log.Fatal(err)
	}
	slog.Info("aegis_worker_start", "redis", cfg.RedisURL)
	if err := server.Run(mux); err != nil {
		log.Fatal(err)
	}
}

func runOnce(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	url := fs.String("url", "", "optional HTML page to crawl in addition to RSS")
	js := fs.Bool("js", false, "use chromedp for HTML URL")
	_ = fs.Parse(args)

	cfg := config.Load()
	srv, _, _, _ := newApp(cfg)

	runTimeout := cfg.RunTimeout
	if runTimeout <= 0 {
		runTimeout = 15 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	res := srv.RunOnce(ctx, *url, *js || cfg.UseJSRender)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		log.Fatal(err)
	}
	if res.Error != "" {
		os.Exit(1)
	}
}
