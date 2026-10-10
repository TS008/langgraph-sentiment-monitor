# Go refactor (`refactor/go-highperf`)

High-concurrency rewrite of AEGIS crisis / sentiment monitoring. Legacy Python + LangGraph remains at the repo root (see `legacy-python/README.md`).

## Library survey (GitHub)

| Area | Candidates considered | Choice | Rationale |
|------|----------------------|--------|-----------|
| HTTP/HTML crawl | [gocolly/colly](https://github.com/gocolly/colly), [chromedp](https://github.com/chromedp/chromedp), [go-rod/rod](https://github.com/go-rod/rod) | **colly v2** | Mature (~25k stars), async + per-domain limits, robots.txt; good for RSS-linked HTML. JS-heavy sites deferred (chromedp/rod documented for later). |
| RSS | [mmcdole/gofeed](https://github.com/mmcdole/gofeed), stdlib XML | **gofeed** | Battle-tested feed parser, simple API. |
| HTTP API | stdlib, [go-chi/chi](https://github.com/go-chi/chi), echo, fiber | **chi v5** | Small, composable middleware, idiomatic on stdlib `net/http`. |
| Rate limits / backpressure | `golang.org/x/time/rate`, custom semaphores | **x/time/rate** + worker sem | Token bucket for crawl politeness; bounded goroutines for RSS fan-out. |
| Job queue (future) | [hibiken/asynq](https://github.com/hibiken/asynq), NATS, Redis streams | **in-process pipeline (MVP)** | End-to-end proof without Redis; asynq recommended for durable multi-worker deploy. |
| LLM / agents | [sashabaranov/go-openai](https://github.com/sashabaranov/go-openai), [tmc/langchaingo](https://github.com/tmc/langchaingo), [cohesion-org/deepseek-go](https://github.com/cohesion-org/deepseek-go) | **go-openai** | OpenAI-compatible `BaseURL` works with DeepSeek; minimal deps. LangGraph-style graphs deferred; explicit pipeline stages preserve product flow. |
| Config | viper, env | **godotenv + env** | Matches existing `.env` pattern; `.env.example` for placeholders. |

### Auth-walled sources (later)

Twitter/X, private Slack, paid firehoses: document OAuth and stream consumers as separate ingest adapters; MVP uses public RSS + optional single HTML URL.

## Architecture (MVP)

```
RSS/HTML ingest (crawl) → sentiment/risk (LLM or heuristic)
  → technical diagnosis (LLM) → PR strategy (LLM)
  → approval gate (timeout auto-approve stub) → action coordinator stub
```

Stages map to legacy agents: `sentiment_sentinel`, `technical_diagnostician`, `pr_strategist`, `decision_gateway`, `action_coordinator`.

## Run the MVP

Prerequisites: Go 1.23+ (required by colly v2.2)

```bash
cd go
cp ../.env.example ../.env   # optional: set DEEPSEEK_API_KEY
go mod tidy
go run ./cmd/aegis run        # one-shot pipeline; prints JSON
go run ./cmd/aegis serve      # HTTP on :8080 (or AEGIS_HTTP_ADDR)
```

### HTTP API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Liveness + whether LLM is configured |
| POST | `/api/v1/runs` | Trigger full pipeline (`{"html_url":"https://..."}` optional) |
| GET | `/api/v1/runs/{id}` | Fetch run result |
| GET | `/api/v1/runs` | List runs (in-memory) |
| POST | `/api/v1/approvals/{id}/approve` | Human approve pending gate (`id` = `{runId}-action`) |
| POST | `/api/v1/approvals/{id}/reject` | Human reject |

Example:

```bash
curl -s localhost:8080/health | jq .
curl -s -X POST localhost:8080/api/v1/runs -d '{}' | jq .
```

Without `DEEPSEEK_API_KEY`, the pipeline still completes using heuristic sentiment scoring and stub LLM text.

## Remaining gaps

- Persistent queue (asynq/Redis) and horizontal workers
- Browser rendering for JS SPAs (chromedp/rod)
- Real commander Web UI / WebSocket (replace Python Socket.IO)
- Social API connectors (X, Weibo, etc.) with OAuth
- Structured PR JSON + legal review stages from legacy graph
- Observability (OpenTelemetry), metrics on crawl/LLM latency
- Integration tests with recorded HTTP fixtures
