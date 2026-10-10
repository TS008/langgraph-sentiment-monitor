# Deploy AEGIS on Cloudflare Containers

This packages **`aegis serve`** (Go API + commander UI + ingest) behind a Worker on **Cloudflare Containers**. Public URL shape:

`https://aegis-monitor.<YOUR_SUBDOMAIN>.workers.dev`

## Plan requirement (Containers API)

**Workers Paid** (or another plan that includes Containers) is required. Containers are not available on the free Workers plan. After upgrading, confirm in the dashboard that **Workers & Pages → your Worker → Containers** is usable and that `npx wrangler containers list` succeeds once you are logged in locally or via CI.

## Recommended: Workers Builds (no laptop Docker)

Use [Workers Builds](https://developers.cloudflare.com/workers/ci-cd/builds/) so Cloudflare builds the Go image with Docker during deploy. You do **not** need Docker on your laptop.

### Connect the repository

1. Cloudflare dashboard → **Workers & Pages** → **Create** → **Import a repository** (or add Builds to an existing Worker).
2. Git provider: **GitHub** → authorize → select **`TS008/langgraph-sentiment-monitor`**.
3. **Root directory**: `cloudflare` (this folder; `wrangler.jsonc` and `package.json` live here).
4. **Production branch**: `main` (or your release branch, e.g. `cursor/go-highperf-93ac` while iterating).
5. **Build command** (optional but recommended for reproducible installs):

   ```bash
   npm ci --legacy-peer-deps
   ```

6. **Deploy command** (production branch — required for Containers):

   ```bash
   npx wrangler deploy
   ```

   On the **production branch only**, `wrangler deploy` builds `../Dockerfile.aegis` (context = repo root), pushes the image, and rolls out container instances. Non-production branch builds default to `npx wrangler versions upload`, which **does not** update container images; keep preview branches on `versions upload` or only merge to production when you want a full container rollout.

7. **Environment variables / secrets** (dashboard → Worker → **Settings** → **Variables and Secrets**):

   | Secret | Purpose |
   |--------|---------|
   | `OPENCODE_GO_API_KEY` | Required for live LLM |
   | `AEGIS_NEWSNOW_BASE_URL` | Optional external NewsNow |
   | `AEGIS_NEWSNOW_DISABLE` | Optional; image defaults to disabling NewsNow |

8. Push to the production branch. First deploy often takes **several minutes** (image build + rollout). Then open:

   - Commander UI: `https://aegis-monitor.<subdomain>.workers.dev/commander`
   - Health: `https://aegis-monitor.<subdomain>.workers.dev/healthz`

Workers that use Durable Objects (including Containers) do not get branch preview URLs; rely on production deploy or Version History after `versions upload`.

References: [Builds configuration](https://developers.cloudflare.com/workers/ci-cd/builds/configuration/), [Deploy Containers](https://developers.cloudflare.com/containers/deploy/).

## Local / CLI deploy (Docker on your machine)

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/) running locally (required for `wrangler deploy` image build)
- [Node.js](https://nodejs.org/) 20+
- Cloudflare account with **Workers Paid** and `npx wrangler login`

### Deploy

From **`cloudflare/`** (build context for the image is the repo root via `build_context` in `wrangler.jsonc`):

```bash
cd cloudflare
npm ci --legacy-peer-deps
npx wrangler secret put OPENCODE_GO_API_KEY   # required for live LLM
# optional:
# npx wrangler secret put AEGIS_NEWSNOW_BASE_URL
# npx wrangler secret put AEGIS_NEWSNOW_DISABLE  # set to "true" if no NewsNow

npx wrangler deploy
```

First deploy builds `../Dockerfile.aegis` (linux/amd64) and pushes to Cloudflare's registry. **Wait several minutes** after the first deploy before expecting container routes to succeed.

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

- `wrangler containers list` — instance status (requires login)
- Worker responds but `/commander` 502 — container still provisioning or failed health check on `/healthz`
- Build fails — run deploy from `cloudflare/` with Docker running (CLI) or use Workers Builds; Dockerfile path is `../Dockerfile.aegis`, context `..`
- Deploy fails **Docker is missing** — use Workers Builds on the production branch, or install/start Docker for CLI deploy
- `wrangler deploy` config errors — use `build_context` (not `context`) under `containers[].images.*` for Wrangler 4.x
