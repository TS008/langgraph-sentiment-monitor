# Deploy AEGIS on Cloudflare Containers

This packages **`aegis serve`** (Go API + commander UI + ingest) behind a Worker on **Cloudflare Containers**. Public URL shape:

`https://aegis-monitor.<YOUR_SUBDOMAIN>.workers.dev`

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/) running locally (required for `wrangler deploy` image build)
- [Node.js](https://nodejs.org/) 20+
- Cloudflare account with **Workers Paid** (Containers) and `wrangler login`

## Deploy

From the **repository root** (so `Dockerfile.aegis` can `COPY go/`):

```bash
cd cloudflare
npm install
npx wrangler secret put OPENCODE_GO_API_KEY   # required for live LLM
# optional:
# npx wrangler secret put AEGIS_NEWSNOW_BASE_URL
# npx wrangler secret put AEGIS_NEWSNOW_DISABLE  # set to "true" if no NewsNow

npx wrangler deploy
```

First deploy builds `../Dockerfile.aegis` (linux/amd64) and pushes to Cloudflare's registry. **Wait several minutes** after the first deploy before expecting container routes to succeed.

Open:

- Commander UI: `https://<worker-host>/commander`
- Health: `https://<worker-host>/healthz`

## Secrets & env

| Secret / var | Purpose |
|--------------|---------|
| `OPENCODE_GO_API_KEY` | OpenCode Go LLM (also accepts `AEGIS_LLM_API_KEY` in container if set as secret) |
| `AEGIS_LLM_BASE_URL` | Optional override (default in Go: OpenCode Go) |
| `AEGIS_LLM_MODEL` | Optional (default `mimo-v2.6-flash`) |
| `AEGIS_RSS_FEEDS` | Comma-separated RSS URLs |
| `AEGIS_NEWSNOW_BASE_URL` | External/self-hosted NewsNow (no sidecar in this image) |
| `AEGIS_NEWSNOW_DISABLE` | `true` to skip NewsNow (default in Docker image) |

Never commit API keys. The Worker passes secrets into the container at start time only.

## NewsNow on Cloudflare

The container image sets **`AEGIS_NEWSNOW_DISABLE=true`** by default (no localhost NewsNow). Options:

1. **RSS + HN** only (zero extra services)
2. Set secret **`AEGIS_NEWSNOW_BASE_URL`** to a self-hosted NewsNow (`docker compose up newsnow` elsewhere) or any reachable NewsNow-compatible API
3. Run a separate NewsNow container on another host/VPS and point the secret at it

## Local image test (no Cloudflare)

```bash
docker build -f Dockerfile.aegis -t aegis:local .
docker run --rm -p 8080:8080 -e OPENCODE_GO_API_KEY=... aegis:local
curl -s localhost:8080/healthz
```

## Chromium / JS crawl

Default image is **lean** (no Chromium). For chromedp builds see `deploy/aegis/Dockerfile.chromium` (heavier; not the default Cloudflare image).

## Troubleshooting

- `wrangler containers list` — instance status
- Worker responds but `/commander` 502 — container still provisioning or failed health check on `/healthz`
- Build fails — run deploy from `cloudflare/` with Docker running; build context is repo root (`context: ".."` in `wrangler.jsonc`)
