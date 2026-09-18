/**
 * Benes dashboard source. The export clients the API workspace lists, and the
 * `/api/client-config` envelope the listener serves for each one.
 *
 * This list is browser-safe on purpose. The authority is `ClientIDs` in
 * `internal/export/export.go`, which also resolves home-directory destinations
 * and therefore pulls `node:os`/`node:path`; importing it here would drag the
 * filesystem into the bundle. `export-clients-contract.test.ts` reads the Go
 * source and fails when the two lists drift, so the sync is checked rather than
 * merely asked for in a comment.
 *
 * A mark is listed only where the client publishes one. A client without an
 * entry renders as its name alone — never another product's logo, and never a
 * provider mark that happens to share the string `opencode`.
 */
import type { TKey } from "../i18n/shared";

interface ExportClientSpec {
  readonly id: string;
  readonly labelKey: TKey;
  /** Public path of the client's own published mark, when it has one. */
  readonly mark?: string;
}

export const EXPORT_CLIENTS = [
  { id: "opencode", labelKey: "api.clientConfig.clientOpencode", mark: "/provider-icons/opencode.svg" },
  { id: "pi", labelKey: "api.clientConfig.clientPi", mark: "/provider-icons/pi.svg" },
  { id: "prime", labelKey: "api.clientConfig.clientPrime" },
  { id: "omp", labelKey: "api.clientConfig.clientOmp" },
  { id: "hermes", labelKey: "api.clientConfig.clientHermes" },
  { id: "openclaw", labelKey: "api.clientConfig.clientOpenclaw" },
  { id: "kimi", labelKey: "api.clientConfig.clientKimi" },
  { id: "gajae", labelKey: "api.clientConfig.clientGajae" },
  { id: "dsh", labelKey: "api.clientConfig.clientDsh" },
  { id: "mcode", labelKey: "api.clientConfig.clientMcode" },
] as const satisfies readonly ExportClientSpec[];

export type ExportClientId = (typeof EXPORT_CLIENTS)[number]["id"];

export const EXPORT_CLIENT_IDS: readonly ExportClientId[] = EXPORT_CLIENTS.map(entry => entry.id);

export function exportClientSpec(id: ExportClientId): ExportClientSpec {
  const found: ExportClientSpec | undefined = EXPORT_CLIENTS.find(entry => entry.id === id);
  if (!found) throw new Error(`unknown export client: ${id}`);
  return found;
}

export function exportClientLabelKey(id: ExportClientId): TKey {
  return exportClientSpec(id).labelKey;
}

/**
 * The `/api/client-config` 200 envelope.
 *
 * `filename`, `mediaType`, and `text` are the listener's, not the dashboard's.
 * `text` is the generated config byte-for-byte: four of these clients do not
 * read JSON, so the panel previews, copies, and downloads those exact bytes and
 * never re-serializes `config`.
 */
export interface ClientConfigEnvelope {
  client: ExportClientId;
  filename: string;
  destination: string;
  apiKeyEnv: string;
  exportHint: string;
  modelCount: number;
  modelsWithoutLimits: number;
  format: string;
  mediaType: string;
  text: string;
  config: unknown;
}

/**
 * What the detail view is allowed to move: a preview, a copy, and a download
 * all take these three fields and nothing else.
 *
 * `text` is the listener's generated config in the client's own format. `config`
 * is the same document as data, and re-serializing it would emit JSON for the
 * clients that read TOML, YAML, or JSON5 — a file that does not load. Filename
 * and media type travel with the bytes for the same reason: the server named
 * the file and chose the type.
 */
export interface ClientConfigTransfer {
  text: string;
  filename: string;
  mediaType: string;
}

export function clientConfigTransfer(envelope: ClientConfigEnvelope): ClientConfigTransfer {
  return { text: envelope.text, filename: envelope.filename, mediaType: envelope.mediaType };
}

function readText(value: unknown): string | null {
  return typeof value === "string" ? value : null;
}

function readCount(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

/**
 * Read the envelope, or `null` when it cannot be shown as-is.
 *
 * The client id comes from the request rather than the payload — the row knows
 * which client it asked for. Every field the detail view prints is required,
 * because a missing filename or media type would silently change what a
 * download writes; a rejected envelope becomes the row's error state.
 */
export function parseClientConfigEnvelope(
  input: unknown,
  client: ExportClientId,
): ClientConfigEnvelope | null {
  if (input === null || typeof input !== "object") return null;
  const wire = input as Record<string, unknown>;
  const filename = readText(wire.filename);
  const destination = readText(wire.destination);
  const apiKeyEnv = readText(wire.apiKeyEnv);
  const exportHint = readText(wire.exportHint);
  const format = readText(wire.format);
  const mediaType = readText(wire.mediaType);
  const text = readText(wire.text);
  const modelCount = readCount(wire.modelCount);
  const modelsWithoutLimits = readCount(wire.modelsWithoutLimits);
  if (
    filename === null || filename === ""
    || destination === null
    || apiKeyEnv === null
    || exportHint === null
    || format === null
    || mediaType === null
    || text === null
    || modelCount === null
    || modelsWithoutLimits === null
  ) {
    return null;
  }
  return {
    client,
    filename,
    destination,
    apiKeyEnv,
    exportHint,
    modelCount,
    modelsWithoutLimits,
    format,
    mediaType,
    text,
    config: wire.config,
  };
}
