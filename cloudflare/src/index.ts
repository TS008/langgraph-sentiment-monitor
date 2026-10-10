import { DurableObject } from "cloudflare:workers";

const PORT = 8080;
const INACTIVITY_TIMEOUT_MS = 15 * 60 * 1000;

export interface Env {
  AEGIS: DurableObjectNamespace<AegisContainer>;
  OPENCODE_GO_API_KEY?: string;
  AEGIS_LLM_API_KEY?: string;
  AEGIS_LLM_BASE_URL?: string;
  AEGIS_LLM_MODEL?: string;
  AEGIS_NEWSNOW_BASE_URL?: string;
  AEGIS_NEWSNOW_DISABLE?: string;
  AEGIS_NEWSNOW_IDS?: string;
  AEGIS_RSS_FEEDS?: string;
}

/**
 * Routes HTTP traffic to the AEGIS Go container (aegis serve).
 */
export class AegisContainer extends DurableObject<Env> {
  private starting: Promise<void> | undefined;

  async fetch(request: Request): Promise<Response> {
    this.starting ??= this.startAndWaitForPort().finally(() => {
      this.starting = undefined;
    });
    await this.starting;

    const url = new URL(request.url);
    url.protocol = "http:";
    url.host = "container";
    const forwarded = new Request(url, request);
    forwarded.headers.delete("host");
    return this.ctx.container!.getTcpPort(PORT).fetch(forwarded);
  }

  private containerEnv(): Record<string, string> {
    const e = this.env;
    const out: Record<string, string> = {
      PORT: String(PORT),
      AEGIS_HTTP_ADDR: `:${PORT}`,
      AEGIS_QUEUE_MODE: "memory",
    };
    const secret = e.OPENCODE_GO_API_KEY || e.AEGIS_LLM_API_KEY;
    if (secret) out.OPENCODE_GO_API_KEY = secret;
    if (e.AEGIS_LLM_BASE_URL) out.AEGIS_LLM_BASE_URL = e.AEGIS_LLM_BASE_URL;
    if (e.AEGIS_LLM_MODEL) out.AEGIS_LLM_MODEL = e.AEGIS_LLM_MODEL;
    if (e.AEGIS_NEWSNOW_BASE_URL) out.AEGIS_NEWSNOW_BASE_URL = e.AEGIS_NEWSNOW_BASE_URL;
    if (e.AEGIS_NEWSNOW_DISABLE) out.AEGIS_NEWSNOW_DISABLE = e.AEGIS_NEWSNOW_DISABLE;
    if (e.AEGIS_NEWSNOW_IDS) out.AEGIS_NEWSNOW_IDS = e.AEGIS_NEWSNOW_IDS;
    if (e.AEGIS_RSS_FEEDS) out.AEGIS_RSS_FEEDS = e.AEGIS_RSS_FEEDS;
    return out;
  }

  private async startAndWaitForPort(): Promise<void> {
    const container = this.ctx.container!;
    if (!container.running) {
      container.start({
        image: container.images.base,
        instance: "lite",
        enableInternet: true,
        env: this.containerEnv(),
      });
    }
    await container.setInactivityTimeout(INACTIVITY_TIMEOUT_MS);

    const port = container.getTcpPort(PORT);
    for (let attempt = 0; attempt < 120; attempt++) {
      try {
        const response = await port.fetch("http://container/healthz", {
          signal: AbortSignal.timeout(2000),
        });
        await response.body?.cancel();
        if (response.ok) return;
      } catch {
        // not ready yet
      }
      await scheduler.wait(500);
    }
    throw new Error("AEGIS container did not become ready on /healthz");
  }
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const id = env.AEGIS.idFromName("primary");
    const stub = env.AEGIS.get(id);
    try {
      return await stub.fetch(request);
    } catch (error) {
      console.error("AEGIS container proxy error:", error);
      return new Response(
        "AEGIS container unavailable. Check Worker logs and container provisioning (first deploy may take several minutes).",
        { status: 502 },
      );
    }
  },
};
