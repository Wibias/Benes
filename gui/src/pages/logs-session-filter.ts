/** Benes dashboard client for the Go proxy (`internal/server`). */
/** Diagnostics hash/query bridge for durable Sessions (`GET /api/diagnostics/requests?sessionId=`). */

export function logsSessionIdFromHash(hash: string): string {
  const raw = hash.replace(/^#\/?/, "");
  const q = raw.indexOf("?");
  const path = q < 0 ? raw : raw.slice(0, q);
  if (path !== "logs") return "";
  if (q < 0) return "";
  return new URLSearchParams(raw.slice(q + 1)).get("sessionId")?.trim() ?? "";
}

export function logsHashForSession(sessionId: string): string {
  const params = new URLSearchParams();
  params.set("sessionId", sessionId);
  return ["logs", params.toString()].join("?");
}

export function logsListUrl(apiBase: string, sessionId?: string): string {
  const params = new URLSearchParams({ limit: "200" });
  const id = sessionId?.trim() ?? "";
  if (id) params.set("sessionId", id);
  return `${apiBase}/api/diagnostics/requests?${params.toString()}`;
}

export function logsHashIsAllowed(path: string, query: URLSearchParams): boolean {
  if (path === "logs/debug") return [...query.keys()].length === 0;
  if (path !== "logs") return false;
  for (const key of query.keys()) {
    if (key !== "sessionId") return false;
  }
  return true;
}
