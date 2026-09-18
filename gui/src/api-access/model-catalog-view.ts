/**
 * Benes dashboard source. The Models tab's presentation contract.
 *
 * The page hands the tab six booleans and a row list; every sentence the tab
 * can print is decided here instead of in JSX. That keeps the states the
 * product distinguishes honestly distinct — a first load, a refresh that keeps
 * last-good rows, a failed refresh beside those rows, a cold failure where
 * nothing ever loaded, a catalog that is genuinely empty, and a search that
 * matched nothing — and it means the component never has to re-derive domain
 * behaviour while it renders.
 */
import type { TFn, TKey } from "../i18n/shared.ts";
import {
  modelProbe,
  modelTestProtocols,
  type GatewayInboundProtocol,
  type ModelProbe,
  type ModelProbeResults,
} from "./model-tests.ts";

/**
 * One `/v1/models` row, shaped for the Models tab.
 *
 * `id` is the callable id exactly as the server announced it — the string the
 * copy action writes into a request — so it is never normalized or rebuilt
 * from the provider. `provider` names who owns the row; `native` and `custom`
 * are what the source column distinguishes, and the remaining flags are set by
 * the surface that produced the row rather than by this read.
 */
export interface ExternalModelRow {
  id: string;
  displayName: string;
  provider: string;
  disabled?: boolean;
  native?: boolean;
  custom?: boolean;
}

/** The id a probe or a copy action addresses this row by. */
export function externalModelId(model: ExternalModelRow): string {
  return model.id;
}

/** The `provider/` prefix of a composite id, absent when it has none. */
function providerPrefix(id: string): string | undefined {
  const slash = id.indexOf("/");
  return slash > 0 ? id.slice(0, slash) : undefined;
}

/**
 * Shape one row of the Models tab's `/v1/models` read.
 *
 * A composite id carries its provider in the prefix and that wins; a bare id
 * falls back to the announced `owned_by`, and to OpenAI when the server named
 * no owner at all. Combo aliases publish themselves through `owned_by`, which
 * is why they are not drawn as OpenAI models. A slash anywhere — including a
 * leading one — means the announced id is not a bare provider model.
 */
export function classifyExternalModel(raw: { id: string; owned_by?: string }): ExternalModelRow {
  const prefix = providerPrefix(raw.id);
  const owner = typeof raw.owned_by === "string" ? raw.owned_by.trim() : "";
  const provider = prefix ?? (owner || "openai");
  return {
    id: raw.id,
    displayName: raw.id,
    provider,
    native: !raw.id.includes("/") && provider === "openai",
    custom: provider !== "openai" && provider !== "combo",
  };
}

/** One /v1/models row as the listener announces it. */
interface ModelCatalogRow {
  id: string;
  owned_by?: string;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isModelCatalogRow(row: unknown): row is ModelCatalogRow {
  return isRecord(row) && typeof row.id === "string";
}

/**
 * The catalogue a /v1/models answer carries, or null when the answer is not one.
 *
 * OpenAI-compatible servers publish either a bare array or the same list under
 * a data field; both are read the same way. A payload that is neither — or that
 * holds a row with no callable id — is not a catalogue, so the caller shows its
 * load failure rather than an empty board. Rows are classified here so every
 * reader of the catalogue reads one shape, and sorted by id so two reads of the
 * same listener answer list identically.
 */
export function readModelsCatalog(payload: unknown): ExternalModelRow[] | null {
  const rows = Array.isArray(payload)
    ? payload
    : (isRecord(payload) && Array.isArray(payload.data) ? payload.data : null);
  if (rows === null) return null;
  return rows
    .filter(isModelCatalogRow)
    .map(row => classifyExternalModel(row))
    .sort((left, right) => left.id.localeCompare(right.id));
}

/**
 * The rows a search leaves, in the order the listener published them.
 *
 * A model is findable by the id a request addresses, by what the board prints,
 * and by the provider that owns it, because those are the three things a reader
 * has in front of them. Matching is case-insensitive over a trimmed query.
 */
export function filterCatalogRows(
  rows: readonly ExternalModelRow[],
  query: string,
): ExternalModelRow[] {
  const needle = query.trim().toLowerCase();
  if (needle === "") return [...rows];
  return rows.filter(row =>
    row.id.toLowerCase().includes(needle)
    || row.displayName.toLowerCase().includes(needle)
    || row.provider.toLowerCase().includes(needle),
  );
}

/** Which of the catalogue's four sources a row came from. */
export type ModelSourceKind = "native" | "combo" | "custom" | "provider";

export function modelSourceKind(model: ExternalModelRow): ModelSourceKind {
  if (model.native) return "native";
  if (model.provider === "combo") return "combo";
  return model.custom ? "custom" : "provider";
}

/** The sources the dashboard names itself; a provider row names its provider. */
const SOURCE_LABEL: Record<ModelSourceKind, TKey | null> = {
  native: "api.sourceNative",
  combo: "api.sourceCombo",
  custom: "api.sourceCustom",
  provider: null,
};

export function apiSourceLabel(
  model: ExternalModelRow,
  t: TFn,
  formatProvider: (provider: string, t: TFn) => string,
): string {
  const key = SOURCE_LABEL[modelSourceKind(model)];
  return key === null ? formatProvider(model.provider, t) : t(key);
}

const PROTOCOL_LABEL: Record<GatewayInboundProtocol, TKey> = {
  responses: "api.protocolResponses",
  chat: "api.protocolChatCompletions",
  messages: "api.protocolMessages",
};

export function apiProtocolLabel(protocol: GatewayInboundProtocol, t: TFn): string {
  return t(PROTOCOL_LABEL[protocol]);
}

export type CatalogColumnId = "model" | "source" | "protocols";

export interface CatalogColumn {
  readonly id: CatalogColumnId;
  readonly header: string;
}

/** One probe's announced state. `detail` only exists for a failure. */
export interface ProbeStatusView {
  readonly text: string;
  readonly className: string;
  readonly detail?: string;
}

export interface ProbeChipView {
  readonly protocol: GatewayInboundProtocol;
  readonly label: string;
  readonly disabled: boolean;
  /** Why the probe is off, for the disabled button's tooltip. */
  readonly hint?: string;
  /** Absent while the probe is idle: there is nothing to announce. */
  readonly status?: ProbeStatusView;
}

export interface CatalogRowView {
  /** The row's domain entity, so an action can name what it acts on. */
  readonly model: ExternalModelRow;
  readonly id: string;
  /** Only when it differs from the id; repeating it states nothing. */
  readonly displayName?: string;
  readonly source: string;
  readonly copyLabel: string;
  readonly chips: readonly ProbeChipView[];
}

/** What the catalog area shows. Exactly one of these is the main surface. */
export type CatalogSurface =
  | { readonly kind: "loading"; readonly label: string }
  | { readonly kind: "unavailable" }
  | { readonly kind: "empty"; readonly message: string }
  | { readonly kind: "no-match"; readonly message: string }
  | { readonly kind: "rows"; readonly rows: readonly CatalogRowView[] };

export interface CatalogFailureView {
  readonly message: string;
  readonly retryLabel: string;
}

export interface ModelCatalogView {
  readonly title: string;
  readonly subtitle: string;
  readonly searchPlaceholder: string;
  /** Present while probes are unavailable, independent of the surface. */
  readonly probeNote: string | null;
  readonly failure: CatalogFailureView | null;
  /** Present while a refresh runs underneath last-good rows. */
  readonly refreshing: string | null;
  readonly columns: readonly CatalogColumn[];
  readonly surface: CatalogSurface;
  readonly disabledNote: string | null;
}

/** Raw page state. Nothing here knows how it will be drawn. */
export interface ModelCatalogState {
  readonly models: readonly ExternalModelRow[];
  readonly totalCount: number;
  readonly query: string;
  readonly loading: boolean;
  readonly refreshing: boolean;
  readonly loadFailed: boolean;
  readonly hasData: boolean;
  readonly copiedModelId: string | null;
  readonly probes: ModelProbeResults;
  readonly probesAvailable: boolean;
  readonly sourceLabel: (model: ExternalModelRow) => string;
  readonly protocolLabel: (protocol: GatewayInboundProtocol) => string;
}

const PROBE_STATUS_TEXT = {
  testing: "api.testingModel",
  ok: "api.testSucceeded",
  error: "api.testFailed",
} as const;

/**
 * The note's class per probe state, spelled out rather than assembled from a prefix.
 *
 * `styles-apikeys-workspace.css` styles the three states separately, and the dashboard's
 * stylesheet-integrity test reads class names from source text: a name assembled at runtime
 * looks unstyled there and the sheet's rules look unreachable. Naming each one keeps the
 * stylesheet and its consumer the same statement.
 */
const PROBE_NOTE_CLASS: Record<Exclude<ModelProbe["status"], "idle">, string> = {
  testing: "api-test-note api-test-note--testing",
  ok: "api-test-note api-test-note--ok",
  error: "api-test-note api-test-note--error",
};

function probeStatus(probe: ModelProbe, t: TFn): ProbeStatusView | undefined {
  if (probe.status === "idle") return undefined;
  const className = PROBE_NOTE_CLASS[probe.status];
  const text = t(PROBE_STATUS_TEXT[probe.status]);
  if (probe.status === "error") return { text, className, detail: probe.detail };
  return { text, className };
}

function probeChip(
  state: ModelCatalogState,
  model: ExternalModelRow,
  protocol: GatewayInboundProtocol,
  t: TFn,
): ProbeChipView {
  const probe = modelProbe(state.probes, externalModelId(model), protocol);
  return {
    protocol,
    label: t("api.auth.testProtocol", { protocol: state.protocolLabel(protocol) }),
    // A probe needs the one-time key, and a running probe needs its own button back.
    disabled: probe.status === "testing" || !state.probesAvailable,
    hint: state.probesAvailable ? undefined : t("api.auth.testNeedsFreshKey"),
    status: probeStatus(probe, t),
  };
}

function catalogRow(state: ModelCatalogState, model: ExternalModelRow, t: TFn): CatalogRowView {
  const id = externalModelId(model);
  return {
    model,
    id,
    displayName: model.displayName === model.id ? undefined : model.displayName,
    source: state.sourceLabel(model),
    copyLabel: t(state.copiedModelId === id ? "api.modelCopied" : "api.copyModelId"),
    chips: modelTestProtocols(model).map(protocol => probeChip(state, model, protocol, t)),
  };
}

function catalogSurface(state: ModelCatalogState, t: TFn): CatalogSurface {
  // A first load owns the area; a refresh over last-good rows does not.
  if (state.loading) return { kind: "loading", label: t("api.modelsLoading") };
  // Failed cold: never read as an empty catalog we never managed to fetch.
  if (!state.hasData) return { kind: "unavailable" };
  if (state.models.length > 0) {
    return { kind: "rows", rows: state.models.map(model => catalogRow(state, model, t)) };
  }
  if (state.totalCount === 0) return { kind: "empty", message: t("api.modelsEmpty") };
  return { kind: "no-match", message: t("api.modelsNoMatch", { query: state.query.trim() }) };
}

export function deriveModelCatalogView(state: ModelCatalogState, t: TFn): ModelCatalogView {
  const surface = catalogSurface(state, t);
  const probingOff = !state.probesAvailable;
  return {
    title: t("api.modelsTitle"),
    subtitle: t("api.modelsSubtitle", { count: state.totalCount }),
    searchPlaceholder: t("api.modelsSearch"),
    probeNote: probingOff ? t("api.models.testNeedsKey") : null,
    failure: state.loadFailed
      ? { message: t("api.modelsLoadFailed"), retryLabel: t("common.retry") }
      : null,
    refreshing: state.refreshing && !state.loading ? t("api.modelsLoading") : null,
    columns: [
      { id: "model", header: t("api.colModel") },
      { id: "source", header: t("api.colSource") },
      { id: "protocols", header: t("api.colProtocols") },
    ],
    surface,
    disabledNote: probingOff && surface.kind === "rows" ? t("api.models.testDisabledNote") : null,
  };
}
