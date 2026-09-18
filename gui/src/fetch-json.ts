/**
 * Decode JSON from the local management API.
 *
 * Callers must inspect HTTP status before treating a body as a success payload.
 * `fetch` resolves on 4xx/5xx, so these helpers are the status gate.
 */

type SuccessBody<T> =
  | { kind: "empty" }
  | { kind: "value"; value: T };

async function readSuccessBody<T>(response: Response): Promise<SuccessBody<T>> {
  if (response.status === 204) return { kind: "empty" };
  if (typeof response.text !== "function") {
    return { kind: "value", value: await response.json() as T };
  }
  const raw = await response.text();
  if (!raw.trim()) return { kind: "empty" };
  return { kind: "value", value: JSON.parse(raw) as T };
}

function managementCopy(body: unknown, fallback: string): string {
  if (!body || typeof body !== "object") return fallback;
  const record = body as { error?: unknown; message?: unknown };
  if (typeof record.error === "string" && record.error) return record.error;
  if (typeof record.message === "string" && record.message) return record.message;
  return fallback;
}

function asSuccessValue<T>(body: SuccessBody<T>): T | undefined {
  return body.kind === "empty" ? undefined : body.value;
}

export async function readJsonOrThrow<T>(
  response: Response,
  fallbackMessage = `HTTP ${response.status}`,
): Promise<T | undefined> {
  if (!response.ok) {
    let message;
    try {
      message = managementCopy(await response.json(), fallbackMessage);
    } catch {
      message = fallbackMessage;
    }
    throw new Error(message);
  }
  return asSuccessValue(await readSuccessBody<T>(response));
}

export async function readJsonIfOk<T>(response: Response): Promise<T | null | undefined> {
  if (!response.ok) return null;
  try {
    return asSuccessValue(await readSuccessBody<T>(response));
  } catch {
    return null;
  }
}

/**
 * The same read as `readJsonOrThrow`, for callers whose contract has no empty success: a
 * missing body is a failure rather than a value-less success, so it throws the fallback copy.
 */
export async function readRequiredJson<T>(response: Response, fallbackMessage?: string): Promise<T> {
  const body = await readJsonOrThrow<T>(response, fallbackMessage);
  if (body === undefined) throw new Error(fallbackMessage ?? "empty response");
  return body;
}

/**
 * UI-facing copy for a failed management response.
 * Prefers a non-empty trimmed `error` string; otherwise the caller fallback.
 * Unlike `readJsonOrThrow`, this does not consult `message`.
 */
export async function readManagementError(response: Response, fallback: string): Promise<string> {
  try {
    const body = await response.json() as { error?: unknown };
    if (typeof body.error === "string") {
      const trimmed = body.error.trim();
      if (trimmed) return trimmed;
    }
  } catch {
    return fallback;
  }
  return fallback;
}

/**
 * Whether a failed read belongs to a generation its caller already walked away from.
 *
 * An aborted signal — or a fetch that surfaces the abort as an `AbortError` — must propagate
 * rather than be answered as a live empty or error row, because the resource that asked for
 * the read has already moved on. Every poll that keeps a stale-while-revalidate row needs the
 * same answer here, so it is decided once instead of at each call site.
 */
export function readAbandoned(signal: AbortSignal, failure: unknown): boolean {
  if (signal.aborted) return true;
  return failure instanceof Error && failure.name === "AbortError";
}
