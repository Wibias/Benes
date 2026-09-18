/**
 * Benes dashboard source. The data-plane URLs the API tab prints.
 *
 * The listener reports one canonical value — the Responses endpoint under the
 * `/v1` prefix of whatever host and port it was reached on. Every other surface
 * hangs off that same prefix, so the base is recovered once and the rest fall
 * out of one route table instead of five hand-written strings that can drift.
 */

/** The surfaces besides Responses, which is the one the listener reports. */
export type ApiSurface = "chatCompletions" | "messages" | "models";

/**
 * Every surface the proxy serves, in the order the dashboard prints them.
 *
 * Responses leads because it is the route the listener reports; the rest follow
 * it. A board lists the surfaces from here rather than naming them again, so a
 * route that moves in this table moves everywhere it is printed.
 */
export const API_SURFACES = ["responses", "chatCompletions", "messages", "models"] as const;

export interface ApiEndpointInfo extends Record<ApiSurface, string> {
  baseUrl: string;
  responses: string;
}

/** Path under the base URL for each derived surface. */
const SURFACE_ROUTE: Record<ApiSurface, string> = {
  chatCompletions: "chat/completions",
  messages: "messages",
  models: "models",
};

function surfacesUnder(baseUrl: string): Record<ApiSurface, string> {
  return {
    chatCompletions: `${baseUrl}/${SURFACE_ROUTE.chatCompletions}`,
    messages: `${baseUrl}/${SURFACE_ROUTE.messages}`,
    models: `${baseUrl}/${SURFACE_ROUTE.models}`,
  };
}

/** Loopback base, used before the listener's own answer has arrived. */
const LOOPBACK_BASE = "http://127.0.0.1:23100/v1";

export const DEFAULT_ENDPOINTS: ApiEndpointInfo = {
  baseUrl: LOOPBACK_BASE,
  responses: `${LOOPBACK_BASE}/responses`,
  ...surfacesUnder(LOOPBACK_BASE),
};

/** Route the reported endpoint is expected to be. */
const V1_RESPONSES = "/v1/responses";

/** Everything before `/v1/responses`, or `null` when the value is not that route. */
function v1Prefix(value: string): string | null {
  if (value.endsWith(V1_RESPONSES)) return value.slice(0, -V1_RESPONSES.length);
  const trailing = `${V1_RESPONSES}/`;
  if (value.endsWith(trailing)) return value.slice(0, -trailing.length);
  return null;
}

/**
 * Derive every surface from the endpoint the listener reported.
 *
 * A canonical `/v1/responses` (with or without its trailing slash) yields the
 * prefix it sits under plus `/v1`. Anything else is treated as a URL the
 * operator configured, so only a trailing `/responses` is trimmed and the
 * custom base is otherwise left exactly as given — normalising it further would
 * silently rewrite a base the listener never reported.
 */
export function deriveApiEndpoints(endpoint: string): ApiEndpointInfo {
  const responses = endpoint === "" ? DEFAULT_ENDPOINTS.responses : endpoint;
  const prefix = v1Prefix(responses);
  const baseUrl = prefix === null ? responses.replace(/\/responses\/?$/, "") : `${prefix}/v1`;
  return { baseUrl, responses, ...surfacesUnder(baseUrl) };
}
