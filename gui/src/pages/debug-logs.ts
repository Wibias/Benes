/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { DebugLogEntry, LogStream } from "./debug-shared";

export function debugLogsPath(apiBase: string, stream: LogStream): string {
  if (stream === "provider") return `${apiBase}/api/debug/logs`;
  if (stream === "usage") return `${apiBase}/api/debug/usage-logs`;
  return `${apiBase}/api/debug/injection-logs`;
}

export function mergeDebugLogEntries(
  previous: DebugLogEntry[],
  next: DebugLogEntry[],
  initial: boolean,
): DebugLogEntry[] {
  return (initial ? next : [...previous, ...next]).slice(-2000);
}

export function debugLogsQuery(initial: boolean, after: number): string {
  const params = new URLSearchParams({ limit: "500" });
  if (!initial && after > 0) params.set("after", String(after));
  return params.toString();
}
