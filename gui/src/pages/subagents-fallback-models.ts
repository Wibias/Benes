/** Held fallback-model list transport for Subagents. */

import { readJsonOrThrow } from "../fetch-json.ts";

const FALLBACK_ROUTE = "/api/subagent-model-fallback";

type FallbackEnvelope = { models?: string[] };

function fallbackUrl(apiBase: string): string {
  return `${apiBase}${FALLBACK_ROUTE}`;
}

function extractModelIds(envelope: FallbackEnvelope | null | undefined, backup: string[]): string[] {
  if (!envelope) return backup;
  return Array.isArray(envelope.models) ? envelope.models : backup;
}

export async function loadSubagentFallbacks(
  apiBase: string,
  failMessage: string,
  signal?: AbortSignal,
): Promise<string[]> {
  const response = await fetch(fallbackUrl(apiBase), { signal });
  const envelope = await readJsonOrThrow<FallbackEnvelope>(response, failMessage);
  return extractModelIds(envelope, []);
}

export async function saveSubagentFallbacks(
  apiBase: string,
  models: string[],
  failMessage: string,
): Promise<string[]> {
  const response = await fetch(fallbackUrl(apiBase), {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ models }),
  });
  const envelope = await readJsonOrThrow<FallbackEnvelope>(response, failMessage);
  return extractModelIds(envelope, models);
}
