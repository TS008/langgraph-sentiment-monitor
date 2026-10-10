# Go refactor (`refactor/go-highperf`)

High-concurrency rewrite of AEGIS crisis / sentiment monitoring. Legacy Python + LangGraph remains at the repo root (see `legacy-python/README.md`).

## Library survey (GitHub)

| Area | Candidates considered | Choice | Rationale |
|------|----------------------|--------|-----------|
| HTTP/HTML crawl | [gocolly/colly](https://github.com/gocolly/colly), [chromedp](https://github.com/chromedp/chromedp), [go-rod/rod](https://github.com/go-rod/rod) | **colly** + **chromedp v0.11** (flag) | Colly/gofeed by default; `AEGIS_CRAWL_JS_RENDER` or `--js` uses chromedp (needs Chromium on the host). |
| RSS | [mmcdole/gofeed](https://github.com/mmcdole/gofeed) | **gofeed** | Public ingest path (HN RSS default). |
| HTTP API + UI | [go-chi/chi](https://github.com/go-chi/chi), htmx | **chi** + **htmx** + SSE | Commander UI at `/commander`; no Socket.IO parity yet. |
| Job queue | [hibiken/asynq](https://github.com/hibiken/asynq), in-process | **asynq** + **memory** default | `AEGIS_QUEUE_MODE=memory` for CI/local; `asynq` + Redis for durable workers. |
| LLM | [go-openai](https://github.com/sashabaranov/go-openai), langchaingo | **go-openai** | OpenAI-compatible client for **OpenCode Go** endpoint. |
| Metrics | prometheus/client_golang, custom text | **custom `/metrics`** | Prometheus text exposition without heavy deps. |
| Logging | log/slog | **slog** JSON | Request id via chi middleware. |

## LLM (OpenCode Go)

| Variable | Purpose |
|----------|---------|
| `OPENCODE_GO_API_KEY` | API key (required for live LLM) |
| `AEGIS_LLM_API_KEY` | Alias for the same key |
| `AEGIS_LLM_BASE_URL` | Default `https://opencode.ai/zen/go/v1` |
| `AEGIS_LLM_MODEL` | Default `mimo-v2.6-flash` |

**OpenCode Go routing (required by the API):** every chat completion request must include:

- Header `x-opencode-session: <uuid>` — AEGIS sets this to the **pipeline run id** and reuses it for all LLM calls in that run (`go/internal/llm`).
- Header `User-Agent: aegis-monitor/1.0` (non-generic; set via custom HTTP transport).

Without these, OpenCode returns `400 MissingSessionID`. No extra env vars are needed; session ids are assigned per run automatically.

Heuristic/offline fallback runs when the key is missing or the API returns 401/403.

## Architecture

```
Social registry (RSS live, X/Weibo stubs)
  → sentiment_sentinel (ingest)
  → sentiment_analysis → technical_diagnostician → pr_strategist → legal_counsel
  → decision_gateway (approve/reject or timeout auto-approve)
  → action_coordinator (stub) → realtime_sentiment_feedback
  → crisis_status_check → post_mortem_analyst
```

Optional **chromedp** fetch for a single HTML URL. Optional **asynq** worker for async runs.

## Run

Prerequisites: Go 1.24+ (see `go/go.mod`; use `GOTOOLCHAIN=auto`)

```bash
cd go
cp ../.env.example ../.env   # set OPENCODE_GO_API_KEY locally only
go mod tidy
go build ./...

go run ./cmd/aegis run              # CLI, no key → heuristic fallback
go run ./cmd/aegis serve            # API + commander UI at http://localhost:8080/commander
```

### Commander UI

Open **http://localhost:8080/commander** — trigger runs, watch SSE stage log, approve/reject (`{runId}-action`).

### Redis + asynq (optional)

```bash
docker compose up -d redis
export AEGIS_QUEUE_MODE=asynq
export AEGIS_REDIS_URL=redis://127.0.0.1:6379
go run ./cmd/aegis serve &
go run ./cmd/aegis worker
```

POST `/api/v1/runs` returns `202` with run id when queue mode is `asynq`. With `memory`, runs execute inline unless you rely on default sync behavior.

### HTTP API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Liveness, model, queue mode |
| GET | `/metrics` | Prometheus text counters |
| GET | `/commander/events` | SSE stage events |
| POST | `/api/v1/runs` | `{"html_url":"","js_render":false,"async":false}` |
| GET | `/api/v1/runs/{id}` | Run result |
| POST | `/api/v1/approvals/{id}/approve` | Human approve |
| POST | `/api/v1/approvals/{id}/reject` | Human reject |

### Social ingest

- **RSS**: configured via `AEGIS_RSS_FEEDS` (implemented).
- **X / Weibo**: stub adapters in `go/internal/social`; enable after OAuth env vars (documented in `.env.example`).

## Remaining follow-ups

- Multi-cycle LangGraph-style loop until commander marks crisis resolved
- Durable run store (Postgres) instead of in-memory
- Full PR JSON templates + department fan-out from legacy Python
- OpenTelemetry traces (beyond slog + `/metrics`)
