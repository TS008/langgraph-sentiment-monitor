package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/api"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/approval"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/config"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		runServe(os.Args[2:])
	case "run":
		runOnce(os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "AEGIS Go monitor — commands:\n")
	fmt.Fprintf(os.Stderr, "  aegis serve          Start HTTP API\n")
	fmt.Fprintf(os.Stderr, "  aegis run [--url URL]  Run pipeline once (stdout JSON)\n")
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "", "listen address (overrides AEGIS_HTTP_ADDR)")
	_ = fs.Parse(args)

	cfg := config.Load()
	if *addr != "" {
		cfg.HTTPAddr = *addr
	}

	st := store.NewMemoryRunStore()
	gate := approval.NewGate(cfg.ApprovalTimeout)
	srv := api.NewServer(cfg, st, gate)

	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("AEGIS listening on %s (llm=%v)", cfg.HTTPAddr, cfg.LLMEnabled())
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
}

func runOnce(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	url := fs.String("url", "", "optional HTML page to crawl in addition to RSS")
	_ = fs.Parse(args)

	cfg := config.Load()
	st := store.NewMemoryRunStore()
	gate := approval.NewGate(cfg.ApprovalTimeout)
	srv := api.NewServer(cfg, st, gate)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	res := srv.RunOnce(ctx, *url)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		log.Fatal(err)
	}
	if res.Error != "" {
		os.Exit(1)
	}
}
