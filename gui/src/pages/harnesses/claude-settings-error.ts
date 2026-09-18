/** Extract the exact management-API refusal for Harnesses toasts. */

export function managementErrorMessage(body: unknown, fallback: string): string {
  const fallbackText = fallback.trim() ? fallback : "request failed";
  if (typeof body === "string" && body.trim()) return body.trim();
  if (!body || typeof body !== "object") return fallbackText;
  const rec = body as { error?: unknown; message?: unknown };
  if (typeof rec.error === "string" && rec.error.trim()) return rec.error.trim();
  if (rec.error && typeof rec.error === "object") {
    const nested = rec.error as { message?: unknown };
    if (typeof nested.message === "string" && nested.message.trim()) return nested.message.trim();
  }
  if (typeof rec.message === "string" && rec.message.trim()) return rec.message.trim();
  return fallbackText;
}
