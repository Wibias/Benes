/** Benes dashboard client for the Go proxy (`internal/server`). */
// Explicit extensions: this policy module is covered directly by
// `gui/scripts/models-routing-combos-policy.test.ts`, which runs it under Node ESM.
import { modelVisible, type ProviderModelMap } from "../model-visibility.ts";
import type { ProviderModelGroup } from "../models-groups.ts";
import {
  CAP_OPTIONS,
  CAP_OPTION_SET,
  NATIVE_CAP_OPTIONS,
  NATIVE_CAP_OPTION_SET,
  NATIVE_GPT56_DEFAULT_WINDOW,
  PAGE,
  type ModelRow,
} from "./models-shared.ts";

/** The window a routing ladder should show when the rows themselves advertise none. */
export function widestAdvertisedWindow(rows: ModelRow[]): number | undefined {
  let widest: number | undefined;
  for (const row of rows) {
    if (typeof row.contextWindow !== "number" || row.contextWindow <= 0) continue;
    if (widest === undefined || row.contextWindow > widest) widest = row.contextWindow;
  }
  return widest;
}

/**
 * The context window the provider card offers.
 *
 * A dialled-in cap wins, because it is what the next enable will use. The native GPT-5.6 group
 * falls back to its own default; every other group offers the widest window its rows advertise,
 * and only then the board default.
 */
export function providerCapDisplayValue(input: {
  capOn: boolean;
  providerCap: number;
  nativeProviderGroup: boolean;
  rows: ModelRow[];
}): number {
  if (input.capOn) return input.providerCap;
  if (input.nativeProviderGroup) return NATIVE_GPT56_DEFAULT_WINDOW;
  return widestAdvertisedWindow(input.rows) ?? input.providerCap;
}

/** The card's own page of rows: what it lists, and how much is still folded away. */
export type ProviderCardRows = {
  /** Rows the card lists, enabled ones first. */
  shown: ModelRow[];
  /** Rows that survive the card's text filter, before paging. */
  matched: number;
  /** Of those, the ones behind "show more". */
  remaining: number;
};

/**
 * The provider card's row policy.
 *
 * The text match is the card's own: the catalogue has already decided whether the *provider*
 * answers the query, so this only asks whether a row id contains it. Enabled models float to
 * the top so they stay findable in long lists; the sort keeps the listener's order inside each
 * partition, and paging happens after it.
 */
export function providerCardRows(input: {
  rows: ModelRow[];
  query: string;
  pageSize: number;
  isHidden: (model: ModelRow) => boolean;
}): ProviderCardRows {
  const needle = input.query.trim().toLowerCase();
  const matched: ModelRow[] = [];
  for (const model of input.rows) {
    if (needle && !model.id.toLowerCase().includes(needle)) continue;
    matched.push(model);
  }
  const ordered = matched.toSorted((a, b) => Number(input.isHidden(a)) - Number(input.isHidden(b)));
  const shown = ordered.slice(0, input.pageSize);
  return { shown, matched: matched.length, remaining: matched.length - shown.length };
}

/**
 * Everything the provider card renders: the group's own facts, the visibility predicate it
 * shares with the catalogue, the context-window ladder it offers, and the rows it lists.
 */
export function modelsProviderCardState(
  group: ProviderModelGroup<ModelRow>,
  input: {
    collapsed: ReadonlySet<string>;
    selectedModelMap: ProviderModelMap;
    disabled: ReadonlySet<string>;
    contextCaps: Record<string, number>;
    contextCapValue: number;
    search: Record<string, string>;
    limit: Record<string, number>;
  },
) {
  const { provider, rows, nativeProviderGroup, liveModels, discovery } = group;
  // Final visibility, not just the disable flag: a model reaches Codex only when the provider
  // allowlist admits it AND it is not disabled. Reading `disabled` alone makes the switches
  // disagree with what the picker offers.
  const isVisible = (model: ModelRow) => modelVisible(
    input.selectedModelMap,
    provider,
    model.id,
    model.native === true,
    input.disabled.has(model.namespaced),
  );
  const capOn = input.contextCaps[provider] !== undefined;
  const providerCap = input.contextCaps[provider] ?? input.contextCapValue;
  const page = providerCardRows({
    rows,
    query: input.search[provider] ?? "",
    pageSize: input.limit[provider] ?? PAGE,
    isHidden: model => !isVisible(model),
  });
  const hasRows = rows.length > 0;
  // An empty provider has nothing to send: both bulk buttons stay inert rather than PUTting an
  // empty target list, which the management API rejects.
  const on = rows.filter(isVisible).length;

  return {
    provider,
    rows,
    nativeProviderGroup,
    liveModels,
    discovery,
    isCollapsed: input.collapsed.has(provider),
    isVisible,
    activeCount: on,
    capOn,
    providerCap,
    capDisplayValue: providerCapDisplayValue({
      capOn,
      providerCap,
      nativeProviderGroup,
      rows,
    }),
    capOptions: nativeProviderGroup ? NATIVE_CAP_OPTIONS : CAP_OPTIONS,
    capOptionSet: nativeProviderGroup ? NATIVE_CAP_OPTION_SET : CAP_OPTION_SET,
    discoveryFailure: liveModels && discovery?.status === "failed" ? discovery : undefined,
    visible: page.shown,
    remaining: page.remaining,
    shown: input.limit[provider] ?? PAGE,
    hasRows,
    allOn: !hasRows || on === rows.length,
    allOff: !hasRows || on === 0,
  };
}
