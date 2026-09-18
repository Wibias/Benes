/**
 * The Dashboard's wire contract: the payload shapes the Go listener answers with on the
 * endpoints this board reads.
 *
 * Every declaration here is a shape the listener publishes for the path named in its
 * comment, or a request body that path accepts. Nothing here fetches, decodes, or routes:
 * reads live in `dashboard-core-poll`, response decoding in `fetch-json`, model projections
 * in `dashboard-model-views`, the sidecar patch merge in `dashboard-sidecar-merge`, and the
 * `#dashboard/...` section vocabulary in `dashboard-tab-nav`.
 */
import type { UsageResponse } from "./usage-contract.ts";

/**
 * `GET /healthz` — a liveness probe.
 *
 * The listener answers `{"ok":true}` and nothing else: no version, status, or uptime. The
 * version the Overview footer shows is the running bundle's own build version.
 */
export interface HealthData { ok: boolean }

/** One row of `GET /api/providers`: provider identity plus stored-credential presence. */
export interface ProviderCredentialRow { name: string; hasApiKey: boolean }

/**
 * The per-provider slice of `GET /api/config` that the Dashboard renders. The listener
 * publishes public fields only; secrets never cross this boundary.
 */
export interface PublicProviderConfig {
  adapter?: string;
  baseUrl?: string;
  defaultModel?: string;
}

/** One row of `GET /api/models`. */
export interface ModelInfo { provider: string; id: string; namespaced: string }

/**
 * `GET /api/settings` — the fields this board reads. Startup protection is not part of it;
 * `GET /api/startup-health` is that state's only owner.
 */
export interface SettingsData {
  codexAutoStart: boolean;
  port: number;
  hostname: string;
}

/**
 * The 30-day usage row the Overview reads. The listener answers `/api/usage` with the whole
 * usage response and the card renders its summary, so the summary comes from the usage
 * contract that owns those fields rather than a second, narrower copy of the same three.
 */
export type UsageSummary30d = Pick<UsageResponse, "summary">;
