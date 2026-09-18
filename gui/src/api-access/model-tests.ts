/**
 * Benes dashboard source. The live model probe, as the API tab tracks it.
 *
 * A probe result belongs to the chip that started it: one model over one
 * inbound wire. Keying the store by both is what keeps a Chat Completions
 * success from being drawn next to a Messages failure, and the union below
 * makes the states mutually exclusive — a result that never arrived is idle,
 * and a failure always carries a reason.
 */

import type { ApiEndpointInfo } from "./endpoints.ts";

/** The inbound wires this proxy accepts, one per client protocol family. */
export type GatewayInboundProtocol = "responses" | "chat" | "messages";

/** Provider families whose upstream also serves Anthropic Messages. */
const MESSAGES_FAMILIES = ["anthropic", "claude"] as const;

function servesAnthropicMessages(provider: string): boolean {
  const family = provider.trim().toLowerCase();
  return MESSAGES_FAMILIES.some(known => family === known) || family.startsWith("anthropic-");
}

/**
 * The wires a probe may run for one catalogue row.
 *
 * The provider identity alone decides this: a model the server files under an
 * Anthropic-compatible family takes all three wires, and every other provider
 * — including an OpenAI-compatible aggregator that happens to serve a model
 * named `claude-*` — takes Responses and Chat Completions only. Probing a wire
 * a provider does not expose would only report a failure nobody can act on.
 */
export function modelTestProtocols(model: { provider: string }): GatewayInboundProtocol[] {
  const wires: GatewayInboundProtocol[] = ["responses", "chat"];
  if (servesAnthropicMessages(model.provider)) wires.push("messages");
  return wires;
}

export type ModelProbe =
  | { status: "idle" }
  | { status: "testing" }
  | { status: "ok" }
  | { status: "error"; detail: string };

/** Per model id, per protocol. */
export type ModelProbeResults = Record<string, Partial<Record<GatewayInboundProtocol, ModelProbe>>>;

export const IDLE_MODEL_PROBE: ModelProbe = { status: "idle" };

/** The probe for one model × protocol chip, idle when nothing has run yet. */
export function modelProbe(
  results: ModelProbeResults,
  modelId: string,
  protocol: GatewayInboundProtocol,
): ModelProbe {
  return results[modelId]?.[protocol] ?? IDLE_MODEL_PROBE;
}

/** Longest probe detail the dashboard keeps for a tooltip or toast. */
const PROBE_DETAIL_MAX = 240;

const CREDENTIAL_SHAPE = /\bbenes_[A-Za-z0-9_-]{6,}/g;
const BEARER_SHAPE = /(\b(?:bearer|x-benes-api-key|x-api-key)\b["':\s=]{0,4})[A-Za-z0-9._-]{6,}/gi;

/**
 * Strip anything credential-shaped out of an upstream error before it reaches a
 * tooltip or a toast.
 *
 * The probe already proves the request was refused; repeating whatever the
 * upstream echoed back adds nothing but risk. A proxy in the path can return
 * the header it rejected, so the detail is filtered rather than trusted:
 * Benes-shaped keys and explicit bearer/header values are replaced, and the
 * remainder is bounded so a chatty error cannot dominate the surface.
 */
export function redactProbeDetail(raw: string): string {
  return raw
    .replace(CREDENTIAL_SHAPE, "[key redacted]")
    .replace(BEARER_SHAPE, "$1[key redacted]")
    .slice(0, PROBE_DETAIL_MAX);
}

/** The only way a failed probe is constructed, so nothing can bypass the filter. */
export function probeFailure(detail: string): ModelProbe {
  return { status: "error", detail: redactProbeDetail(detail) };
}

/** The one user turn every probe sends. */
function pingTurn(): { role: string; content: string } {
  return { role: "user", content: "ping" };
}

/**
 * What a probe sends, per inbound wire.
 *
 * The three requests ask the same question — one token from the committed model
 * — of three different protocols, so the bodies differ in shape and not in
 * intent. Each body is written once here rather than rebuilt at the call site,
 * which is what keeps a probe from drifting away from the wire it names.
 */
const PROBE_BODY: Record<GatewayInboundProtocol, (modelId: string) => unknown> = {
  responses: modelId => ({ model: modelId, input: "ping", stream: false }),
  chat: modelId => ({ model: modelId, messages: [pingTurn()], max_tokens: 1, stream: false }),
  messages: modelId => ({ model: modelId, max_tokens: 1, messages: [pingTurn()] }),
};

/** The route each wire is reached by, as the listener reported it. */
const PROBE_URL: Record<GatewayInboundProtocol, (endpoints: ApiEndpointInfo) => string> = {
  responses: endpoints => endpoints.responses,
  chat: endpoints => endpoints.chatCompletions,
  messages: endpoints => endpoints.messages,
};

/** The URL and body one probe sends to the proxy's own listener. */
export function modelTestRequest(
  protocol: GatewayInboundProtocol,
  modelId: string,
  endpoints: ApiEndpointInfo,
): { url: string; body: unknown } {
  return { url: PROBE_URL[protocol](endpoints), body: PROBE_BODY[protocol](modelId) };
}

/** Longest gateway message the dashboard keeps; matches the probe detail cap. */
const MESSAGE_MAX = 240;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function readErrorText(value: unknown): string | null {
  if (typeof value !== "string") return null;
  const trimmed = value.trim();
  return trimmed === "" ? null : trimmed.slice(0, MESSAGE_MAX);
}

/**
 * Where an OpenAI-shaped error keeps its sentence, in the order the proxy's
 * upstreams use them. The first non-empty one wins.
 */
const ERROR_PATHS: ReadonlyArray<(body: Record<string, unknown>) => unknown> = [
  body => body.message,
  body => (isRecord(body.error) ? body.error.message : null),
  body => body.detail,
];

/** True for a body that is markup or JSON rather than a sentence. */
function looksEncoded(trimmed: string): boolean {
  return trimmed.startsWith("{") || trimmed.startsWith("<");
}

/**
 * The sentence to show for a refused probe.
 *
 * An upstream that answers with a plain sentence is quoted as-is; anything that
 * looks like JSON or markup is read for its message and otherwise reduced to the
 * status code, because echoing a body the reader cannot act on is worse than
 * naming what happened.
 */
export function modelTestErrorMessage(raw: string, status: number): string {
  const trimmed = raw.trim();
  if (!looksEncoded(trimmed)) {
    return trimmed === "" ? `HTTP ${status}` : trimmed.slice(0, MESSAGE_MAX);
  }
  try {
    const body = JSON.parse(trimmed) as unknown;
    if (isRecord(body)) {
      for (const read of ERROR_PATHS) {
        const message = readErrorText(read(body));
        if (message !== null) return message;
      }
    }
  } catch {
    // Not JSON after all: the status code is the honest answer.
  }
  return `HTTP ${status}`;
}

