/**
 * Focused contracts for the independently reauthored Codex auth/account modules.
 *
 * These cover observable behavior and transitions, not source shape: selection-order
 * normalization, the OpenAI account-mode read, the mutation-completion projection,
 * OAuth ToS risk levels, auto-switch threshold planning, and OAuth health mapping.
 */
import assert from "node:assert/strict";
import test from "node:test";

import type { TFn } from "../src/i18n/shared.ts";
import {
  ACCOUNT_PRIORITY_PRESETS,
  accountPriorityLabel,
  accountPriorityPresetKey,
  DEFAULT_ACCOUNT_PRIORITY,
  formatAccountPriority,
  isAccountPriorityPreset,
  normalizeAccountPriority,
} from "../src/account-priority.ts";
import { codexAccountModeState } from "../src/codex-multi-state.ts";
import { codexAccountMutationCompletion } from "../src/codex-account-mutation.ts";
import { oauthTosRisk, oauthTosRiskBodyKey, oauthTosRiskTitleKey } from "../src/oauth-tos-risk.ts";
import {
  DEFAULT_AUTO_SWITCH_THRESHOLD,
  autoSwitchThresholdReadDisposition,
  extractAutoSwitchThresholdPayload,
  nextAutoSwitchThreshold,
  normalizeAutoSwitchThreshold,
  parseEnabledAutoSwitchThreshold,
  planAutoSwitchToggleWrite,
  putAutoSwitchThreshold,
} from "../src/codex-auto-switch.ts";
import {
  accountNeedsReauth,
  formatOAuthHealthLabel,
  formatOAuthHealthSummary,
  oauthHealthBadgeClass,
  oauthHealthBadgeTone,
  oauthHealthLabelKey,
  oauthHealthShowsDoctor,
  oauthHealthShowsReauth,
} from "../src/oauth-health-display.ts";
import { creditDaysRemaining } from "../src/intl-formatters.ts";
import { feedbackOutcome, settledCopy } from "../src/copy-feedback.ts";
import {
  ACCOUNT_POOL_RESET_ORDERS,
  ACCOUNT_POOL_STRATEGIES,
  DEFAULT_ACCOUNT_POOL_RESET_ORDER,
  DEFAULT_ACCOUNT_POOL_STRATEGY,
  DEFAULT_ACCOUNT_POOL_STICKY_LIMIT,
  normalizeAccountPoolResetOrder,
  normalizeAccountPoolStickyLimit,
  normalizeAccountPoolStrategy,
  parseAccountPoolStickyLimitDraft,
  putCodexPoolStrategy,
} from "../src/account-pool-strategy.ts";
import { isThirtyDayOnlyPlan, normalizeQuotaForPlan } from "../src/codex-quota-utils.ts";
import {
  accountPickerReducer,
  initialAccountPickerState,
  type AccountPickerEvent,
  type AccountPickerState,
} from "../src/lib/codex-account-picker-policy.ts";

function fakeT(key: string, vars?: Record<string, unknown>): string {
  return vars ? `${key}(${JSON.stringify(vars)})` : key;
}
const translate = fakeT as unknown as TFn;

test("account priority presets stay in highest-first order", () => {
  assert.deepEqual([...ACCOUNT_PRIORITY_PRESETS], [2, 1, 0, -1, -2]);
});

test("account priority normalization rejects non-integers and out-of-range values", () => {
  assert.equal(normalizeAccountPriority(2), 2);
  assert.equal(normalizeAccountPriority(-100), -100);
  assert.equal(normalizeAccountPriority(100), 100);
  assert.equal(normalizeAccountPriority("2"), DEFAULT_ACCOUNT_PRIORITY);
  assert.equal(normalizeAccountPriority(1.5), DEFAULT_ACCOUNT_PRIORITY);
  assert.equal(normalizeAccountPriority(101), DEFAULT_ACCOUNT_PRIORITY);
  assert.equal(normalizeAccountPriority(-101), DEFAULT_ACCOUNT_PRIORITY);
  assert.equal(normalizeAccountPriority(undefined), DEFAULT_ACCOUNT_PRIORITY);
});

test("account priority formatting renders a signed hyphen", () => {
  assert.equal(formatAccountPriority(2), "+2");
  assert.equal(formatAccountPriority(0), "0");
  assert.equal(formatAccountPriority(-1), "-1");
  assert.equal(formatAccountPriority("nonsense"), "0");
});

test("account priority preset lookup distinguishes presets from custom values", () => {
  assert.equal(isAccountPriorityPreset(2), true);
  assert.equal(isAccountPriorityPreset(7), false);
  assert.equal(accountPriorityPresetKey(-2), "accountPool.priorityLast");
  assert.equal(accountPriorityPresetKey(7), null);
});

test("account priority labels name the preset or fall back to Custom", () => {
  assert.equal(
    accountPriorityLabel(translate, 1),
    'accountPool.priorityOption({"name":"accountPool.priorityEarlier","value":"+1"})',
  );
  assert.equal(
    accountPriorityLabel(translate, 7),
    'accountPool.priorityOption({"name":"accountPool.priorityCustom","value":"+7"})',
  );
});

test("codex account mode reads only a live OpenAI provider row", () => {
  assert.equal(codexAccountModeState(null), "absent");
  assert.equal(codexAccountModeState("config"), "absent");
  assert.equal(codexAccountModeState({ providers: {} }), "absent");
  assert.equal(codexAccountModeState({ providers: { openai: [] } }), "absent");
  assert.equal(codexAccountModeState({ providers: { openai: {} } }), "pool");
  assert.equal(codexAccountModeState({ providers: { openai: { codexAccountMode: "pool" } } }), "pool");
  assert.equal(codexAccountModeState({ providers: { openai: { codexAccountMode: "direct" } } }), "direct");
  assert.equal(codexAccountModeState({ providers: { openai: { disabled: true, codexAccountMode: "direct" } } }), "disabled");
  assert.equal(codexAccountModeState({ providers: { openai: { codexAccountMode: "other" } } }), "absent");
});

test("mutation completion reads only an own boolean property", () => {
  assert.deepEqual(codexAccountMutationCompletion({ catalogRefreshPending: true }), { catalogRefreshPending: true });
  assert.deepEqual(codexAccountMutationCompletion({ catalogRefreshPending: false }), { catalogRefreshPending: false });
  assert.deepEqual(codexAccountMutationCompletion({ catalogRefreshPending: "yes" }), { catalogRefreshPending: false });
  assert.deepEqual(codexAccountMutationCompletion(null), { catalogRefreshPending: false });
  assert.deepEqual(codexAccountMutationCompletion([1, 2]), { catalogRefreshPending: false });
  const inherited = Object.create({ catalogRefreshPending: true }) as object;
  assert.deepEqual(codexAccountMutationCompletion(inherited), { catalogRefreshPending: false });
});

test("oauth ToS risk is a provider-id lookup with matching copy keys", () => {
  assert.equal(oauthTosRisk(" Anthropic "), "high");
  assert.equal(oauthTosRisk("google-antigravity"), "high");
  assert.equal(oauthTosRisk("github-copilot"), "elevated");
  assert.equal(oauthTosRisk("cursor"), "elevated");
  assert.equal(oauthTosRisk("openai"), null);
  assert.equal(oauthTosRiskTitleKey("high"), "oauthTos.highTitle");
  assert.equal(oauthTosRiskTitleKey("elevated"), "oauthTos.elevatedTitle");
  assert.equal(oauthTosRiskBodyKey("high"), "oauthTos.highBody");
  assert.equal(oauthTosRiskBodyKey("elevated"), "oauthTos.elevatedBody");
});

test("auto-switch threshold normalization clamps to 0-100 or the default", () => {
  assert.equal(normalizeAutoSwitchThreshold(0), 0);
  assert.equal(normalizeAutoSwitchThreshold(100), 100);
  assert.equal(normalizeAutoSwitchThreshold(-1), DEFAULT_AUTO_SWITCH_THRESHOLD);
  assert.equal(normalizeAutoSwitchThreshold(101), DEFAULT_AUTO_SWITCH_THRESHOLD);
  assert.equal(normalizeAutoSwitchThreshold(33.3), DEFAULT_AUTO_SWITCH_THRESHOLD);
  assert.equal(normalizeAutoSwitchThreshold("50"), DEFAULT_AUTO_SWITCH_THRESHOLD);
});

test("auto-switch draft parsing accepts only enabled integers", () => {
  assert.equal(parseEnabledAutoSwitchThreshold(" 42 "), 42);
  assert.equal(parseEnabledAutoSwitchThreshold("1"), 1);
  assert.equal(parseEnabledAutoSwitchThreshold("100"), 100);
  assert.equal(parseEnabledAutoSwitchThreshold("0"), null);
  assert.equal(parseEnabledAutoSwitchThreshold("101"), null);
  assert.equal(parseEnabledAutoSwitchThreshold("-5"), null);
  assert.equal(parseEnabledAutoSwitchThreshold("4 2"), null);
});

test("auto-switch restore value keeps the last enabled threshold", () => {
  assert.equal(nextAutoSwitchThreshold(40, 90), 0);
  assert.equal(nextAutoSwitchThreshold(0, 90), 90);
  assert.equal(nextAutoSwitchThreshold(0, 0), DEFAULT_AUTO_SWITCH_THRESHOLD);
  assert.equal(nextAutoSwitchThreshold(0, 250), DEFAULT_AUTO_SWITCH_THRESHOLD);
});

test("auto-switch read disposition drops superseded reads and defers busy ones", () => {
  assert.equal(autoSwitchThresholdReadDisposition(false, false, 3, 4), "ignore");
  assert.equal(autoSwitchThresholdReadDisposition(true, false, 4, 4), "defer");
  assert.equal(autoSwitchThresholdReadDisposition(false, true, 4, 4), "defer");
  assert.equal(autoSwitchThresholdReadDisposition(false, false, 4, 4), "apply");
});

test("auto-switch payload extraction unwraps only a full /active body", () => {
  assert.equal(extractAutoSwitchThresholdPayload({ autoSwitchThreshold: 25 }), 25);
  assert.equal(extractAutoSwitchThresholdPayload(25), 25);
  assert.equal(extractAutoSwitchThresholdPayload(null), null);
  assert.deepEqual(extractAutoSwitchThresholdPayload([1]), [1]);
});

test("auto-switch toggle plan is a single write in both directions", () => {
  assert.deepEqual(planAutoSwitchToggleWrite(0, "50", 90), { threshold: 90, lastEnabled: 90 });
  assert.deepEqual(planAutoSwitchToggleWrite(40, "55", 90), { threshold: 0, lastEnabled: 55 });
  assert.deepEqual(planAutoSwitchToggleWrite(40, "bogus", 90), { threshold: 0, lastEnabled: 90 });
});

test("auto-switch PUT writes the threshold and reports transport failure separately", async () => {
  const calls: Array<Record<string, string>> = [];
  const ok = await putAutoSwitchThreshold("http://x", 42, async (url, init) => {
    calls.push({ url, body: String(init.body) });
    return new Response(null, { status: 200 });
  });
  assert.equal(ok, true);
  assert.equal(calls[0]?.url, "http://x/api/codex-auth/auto-switch");
  assert.equal(calls[0]?.body, JSON.stringify({ threshold: 42 }));

  const rejected = await putAutoSwitchThreshold("http://x", 42, async () => new Response(null, { status: 500 }));
  assert.equal(rejected, false);
  const thrown = await putAutoSwitchThreshold("http://x", 42, async () => { throw new Error("down"); });
  assert.equal(thrown, false);
  const invalid = await putAutoSwitchThreshold("http://x", 101, async () => new Response(null, { status: 200 }));
  assert.equal(invalid, false);
});

test("oauth health tone and class map status to a badge", () => {
  assert.equal(oauthHealthBadgeTone("healthy"), "ok");
  assert.equal(oauthHealthBadgeTone("cooldown"), "muted");
  assert.equal(oauthHealthBadgeTone("warning"), "warn");
  assert.equal(oauthHealthBadgeTone("reauth_required"), "warn");
  assert.equal(oauthHealthBadgeTone(undefined), "muted");
  assert.equal(oauthHealthBadgeClass("healthy"), "badge badge-green");
  assert.equal(oauthHealthBadgeClass("warning"), "badge badge-amber");
  assert.equal(oauthHealthBadgeClass(undefined), "badge badge-muted");
});

test("oauth health reauth and doctor gates follow the canonical projection", () => {
  assert.equal(oauthHealthShowsReauth("reauth_required"), true);
  assert.equal(oauthHealthShowsReauth("cooldown"), false);
  assert.equal(oauthHealthShowsDoctor("warning"), true);
  assert.equal(oauthHealthShowsDoctor("healthy"), false);
  assert.equal(accountNeedsReauth(null), false);
  assert.equal(accountNeedsReauth({ needsReauth: true }), true);
  assert.equal(accountNeedsReauth({ health: { status: "reauth_required" } }), true);
  assert.equal(accountNeedsReauth({ health: { status: "cooldown" } }), false);
});

test("oauth health label keys cover cooldown, reauth, and warning reasons", () => {
  assert.equal(oauthHealthLabelKey(undefined), null);
  assert.equal(oauthHealthLabelKey({ status: "healthy" }), null);
  assert.equal(oauthHealthLabelKey({ status: "cooldown", reason: "rate_limit" }), "pws.healthLabel.rateLimited");
  assert.equal(oauthHealthLabelKey({ status: "cooldown", reason: "quota" }), "pws.healthLabel.quotaLimited");
  assert.equal(oauthHealthLabelKey({ status: "reauth_required", reason: "refresh_failed" }), "pws.healthLabel.refreshFailed");
  assert.equal(oauthHealthLabelKey({ status: "reauth_required" }), "pws.healthLabel.reauthRequired");
  assert.equal(oauthHealthLabelKey({ status: "warning", reason: "refresh_conflict" }), "pws.healthLabel.credentialConflict");
  assert.equal(oauthHealthLabelKey({ status: "warning", reason: "metadata_mismatch" }), "pws.healthLabel.metadataMismatch");
  assert.equal(oauthHealthLabelKey({ status: "warning", reason: "stale_credentials" }), "pws.healthLabel.refreshFailed");
  assert.equal(oauthHealthLabelKey({ status: "warning", reason: "unknown" }), "pws.healthLabel.reauthRequired");
  assert.equal(formatOAuthHealthLabel(translate, { status: "healthy" }), null);
  assert.equal(formatOAuthHealthLabel(translate, { status: "cooldown", reason: "rate_limit" }), "pws.healthLabel.rateLimited");
});

test("oauth health summary reports nothing for a healthy account", () => {
  assert.equal(formatOAuthHealthSummary(translate, "openai", "a@b", undefined), null);
  assert.equal(formatOAuthHealthSummary(translate, "openai", "a@b", { status: "healthy" }), null);
  const cooldown = formatOAuthHealthSummary(translate, "openai", "__main__", { status: "cooldown", reason: "rate_limit" });
  assert.ok(cooldown?.startsWith("pws.healthSummary.rateLimited("));
  const reauth = formatOAuthHealthSummary(translate, "openai", "a@b", { status: "reauth_required" });
  assert.ok(reauth?.startsWith("pws.healthSummary.reauthRequired("));
  const conflict = formatOAuthHealthSummary(translate, "openai", "a@b", { status: "warning", reason: "refresh_conflict" });
  assert.ok(conflict?.startsWith("pws.healthSummary.credentialConflict("));
  const stale = formatOAuthHealthSummary(translate, "openai", "a@b", { status: "warning" });
  assert.ok(stale?.startsWith("pws.healthSummary.staleCredentials("));
});

test("credit day count floors in the past and rounds up in the future", () => {
  assert.equal(creditDaysRemaining(new Date(Date.now() - 86_400_000).toISOString()), 0);
  assert.equal(creditDaysRemaining(new Date(Date.now() + 2 * 86_400_000).toISOString()), 2);
  assert.equal(creditDaysRemaining(new Date(Date.now() + 90 * 60_000).toISOString()), 1);
});

test("copy feedback scope tag travels with the outcome", () => {
  assert.equal(feedbackOutcome(null, "a"), null);
  assert.equal(feedbackOutcome(settledCopy("a", true), "a"), "copied");
  assert.equal(feedbackOutcome(settledCopy("a", false), "a"), "unavailable");
  assert.equal(feedbackOutcome(settledCopy("a", true), "b"), null);
});

/** The pool rotation value domains, which had no test consumer before this cluster. */
test("pool rotation values fall back to the shipped defaults for anything unknown", () => {
  assert.deepEqual([...ACCOUNT_POOL_STRATEGIES], ["quota", "round-robin", "fill-first", "reset-window"]);
  assert.deepEqual([...ACCOUNT_POOL_RESET_ORDERS], ["soonest", "latest"]);
  assert.equal(normalizeAccountPoolStrategy("fill-first"), "fill-first");
  assert.equal(normalizeAccountPoolStrategy("weighted"), DEFAULT_ACCOUNT_POOL_STRATEGY);
  assert.equal(normalizeAccountPoolStrategy(undefined), DEFAULT_ACCOUNT_POOL_STRATEGY);
  assert.equal(normalizeAccountPoolResetOrder("latest"), "latest");
  assert.equal(normalizeAccountPoolResetOrder(null), DEFAULT_ACCOUNT_POOL_RESET_ORDER);
});

test("sticky limits are whole numbers inside 1-100, and drafts must be plain digits", () => {
  assert.equal(normalizeAccountPoolStickyLimit(1), 1);
  assert.equal(normalizeAccountPoolStickyLimit(100), 100);
  assert.equal(normalizeAccountPoolStickyLimit(0), DEFAULT_ACCOUNT_POOL_STICKY_LIMIT);
  assert.equal(normalizeAccountPoolStickyLimit(101), DEFAULT_ACCOUNT_POOL_STICKY_LIMIT);
  assert.equal(normalizeAccountPoolStickyLimit(2.5), DEFAULT_ACCOUNT_POOL_STICKY_LIMIT);
  assert.equal(normalizeAccountPoolStickyLimit("3"), DEFAULT_ACCOUNT_POOL_STICKY_LIMIT);
  assert.equal(parseAccountPoolStickyLimitDraft(" 7 "), 7);
  assert.equal(parseAccountPoolStickyLimitDraft("1"), 1);
  assert.equal(parseAccountPoolStickyLimitDraft("100"), 100);
  assert.equal(parseAccountPoolStickyLimitDraft("0"), null);
  assert.equal(parseAccountPoolStickyLimitDraft("101"), null);
  assert.equal(parseAccountPoolStickyLimitDraft("-4"), null);
  assert.equal(parseAccountPoolStickyLimitDraft("2.5"), null);
  assert.equal(parseAccountPoolStickyLimitDraft(""), null);
});

test("the pool-strategy PUT sends only the fields the operator moved", async () => {
  const calls: Array<{ url: string; method?: string; body?: string }> = [];
  const stored = await putCodexPoolStrategy("http://x", { strategy: "reset-window" }, async (url, init) => {
    calls.push({ url, method: init.method, body: String(init.body) });
    return new Response(JSON.stringify({ accountPoolStrategy: "reset-window" }), { status: 200 });
  });
  assert.equal(calls[0]?.url, "http://x/api/codex-auth/pool-strategy");
  assert.equal(calls[0]?.method, "PUT");
  assert.deepEqual(JSON.parse(String(calls[0]?.body)), { strategy: "reset-window" });
  // The listener answered with one field, so the other two stay at what was asked for.
  assert.deepEqual(stored, {
    ok: true,
    strategy: "reset-window",
    stickyLimit: DEFAULT_ACCOUNT_POOL_STICKY_LIMIT,
    resetOrder: DEFAULT_ACCOUNT_POOL_RESET_ORDER,
  });
});

test("an empty pool-strategy patch and every failed write report the same refusal", async () => {
  let sent = 0;
  const counted = async () => { sent += 1; return new Response(null, { status: 200 }); };
  assert.deepEqual(await putCodexPoolStrategy("http://x", {}, counted), { ok: false });
  assert.equal(sent, 0);
  assert.deepEqual(
    await putCodexPoolStrategy("http://x", { stickyLimit: 4 }, async () => new Response(null, { status: 500 })),
    { ok: false },
  );
  assert.deepEqual(
    await putCodexPoolStrategy("http://x", { stickyLimit: 4 }, async () => { throw new Error("down"); }),
    { ok: false },
  );
});

test("a 30-day plan keeps only the monthly window", () => {
  assert.equal(isThirtyDayOnlyPlan(" Go "), true);
  assert.equal(isThirtyDayOnlyPlan("free"), true);
  assert.equal(isThirtyDayOnlyPlan("plus"), false);
  assert.equal(isThirtyDayOnlyPlan(null), false);
  assert.deepEqual(
    normalizeQuotaForPlan({
      fiveHourPercent: 10,
      fiveHourResetAt: 1,
      weeklyPercent: 20,
      weeklyResetAt: 2,
      monthlyPercent: 30,
      monthlyResetAt: 3,
      resetCredits: 4,
      updatedAt: 5,
    }, "go"),
    { updatedAt: 5, monthlyPercent: 30, monthlyResetAt: 3, resetCredits: 4 },
  );
  const rolling = { fiveHourPercent: 10, updatedAt: 5 };
  assert.equal(normalizeQuotaForPlan(rolling, "plus"), rolling);
  assert.equal(normalizeQuotaForPlan(null, "go"), null);
});

/**
 * The account-picker card's transitions. They used to live inside the component, where the
 * read/save ordering could only be observed through a rendered dashboard.
 */
function pickerRun(start: AccountPickerState, ...events: AccountPickerEvent[]): AccountPickerState {
  return events.reduce(accountPickerReducer, start);
}

test("an account-picker read that started before a save cannot undo it", () => {
  const read = pickerRun(initialAccountPickerState(), { kind: "read-began", generation: 1 });
  const saving = pickerRun(read, { kind: "save-began", generation: 2, enabled: true });
  assert.equal(saving.saving, true);
  assert.equal(saving.enabled, true);
  // The GET issued before the save lands late; it is dropped rather than applied.
  assert.equal(pickerRun(saving, { kind: "read-arrived", generation: 1, enabled: false }), saving);
  assert.equal(pickerRun(saving, { kind: "read-failed", generation: 1 }), saving);
});

test("a second account-picker save cannot start while one is in flight", () => {
  const saving = pickerRun(initialAccountPickerState(), { kind: "save-began", generation: 1, enabled: true });
  assert.equal(pickerRun(saving, { kind: "save-began", generation: 2, enabled: false }), saving);
});

test("only a cold account-picker read failure offers the retry", () => {
  const cold = pickerRun(
    initialAccountPickerState(),
    { kind: "read-began", generation: 1 },
    { kind: "read-failed", generation: 1 },
  );
  assert.equal(cold.loadError, true);
  assert.equal(cold.hydrated, false);

  const hydrated = pickerRun(
    initialAccountPickerState(),
    { kind: "read-began", generation: 1 },
    { kind: "read-arrived", generation: 1, enabled: true },
  );
  assert.equal(hydrated.hydrated, true);
  assert.equal(hydrated.loadError, false);
  const laterMiss = pickerRun(hydrated, { kind: "read-began", generation: 2 }, { kind: "read-failed", generation: 2 });
  assert.equal(laterMiss.loadError, false);
  assert.equal(laterMiss.enabled, true);
});

test("a refused account-picker save restores the value the listener still holds", () => {
  const ready = pickerRun(
    initialAccountPickerState(),
    { kind: "read-began", generation: 1 },
    { kind: "read-arrived", generation: 1, enabled: false },
  );
  const saving = pickerRun(ready, { kind: "save-began", generation: 2, enabled: true });
  assert.equal(saving.enabled, true);
  const refused = pickerRun(saving, { kind: "save-failed" });
  assert.equal(refused.enabled, false);
  assert.equal(refused.saving, false);
  assert.equal(refused.feedback, "update-failed");
});

test("an accepted account-picker save names its own feedback", () => {
  const ready = pickerRun(
    initialAccountPickerState(),
    { kind: "read-began", generation: 1 },
    { kind: "read-arrived", generation: 1, enabled: false },
  );
  const saving = pickerRun(ready, { kind: "save-began", generation: 2, enabled: true });
  const pending = pickerRun(saving, { kind: "save-arrived", enabled: true, catalogRefreshPending: true });
  assert.equal(pending.enabled, true);
  assert.equal(pending.saving, false);
  assert.equal(pending.feedback, "refresh-pending");

  const settled = pickerRun(saving, { kind: "save-arrived", enabled: true, catalogRefreshPending: false });
  assert.equal(settled.feedback, "updated");
});
