import assert from "node:assert/strict";
import test from "node:test";

import { clampNumberDraft } from "../src/clamp-draft.ts";
import { COPY_FEEDBACK_VISIBLE_MS, createAttemptSequencer, feedbackOutcome, settledCopy } from "../src/copy-feedback.ts";
import {
  featureFlagEnabled,
  featureFlagFailureMessage,
  featureFlagSavedMessageKey,
  featureFlagWriteAccepted,
} from "../src/codex-feature-flags.ts";
import { oauthTosDialogPlan } from "../src/oauth-tos-dialog.ts";
import { formatBytes } from "../src/format-bytes.ts";
import { formatTokens } from "../src/format-tokens.ts";
import { formatUptime } from "../src/formatUptime.ts";
import { SECTION_TAB_SCROLL_LOCK_MS, sectionAnchorId, sectionAnchorPrefix } from "../src/section-anchors.ts";
import { catalogValue } from "../src/i18n/catalogs.ts";
import type { TFn } from "../src/i18n/shared.ts";
import {
  CAP_OPTION_SET,
  CAP_OPTIONS,
  CUSTOM_OPTION,
  NATIVE_CAP_OPTION_SET,
  NATIVE_CAP_OPTIONS,
  NATIVE_GPT56_DEFAULT_WINDOW,
  PAGE,
  REASONING_EFFORT_LEVELS,
  THREAD_OPTION_SET,
  collectDisabledNamespaced,
  discoveryFailureLabel,
  fmtK,
  readCollapsedProviders,
  writeCollapsedProviders,
  type DiscoveryFailure,
} from "../src/pages/models-shared.ts";

test("clampNumberDraft starts an unreadable draft at the low bound", () => {
  assert.equal(clampNumberDraft("", 1, 1, 100), "2");
  assert.equal(clampNumberDraft("   ", 1, 1, 100), "2");
  assert.equal(clampNumberDraft("abc", -1, 5, 10), "5");
});

test("clampNumberDraft clamps both ends", () => {
  assert.equal(clampNumberDraft("100", 1, 1, 100), "100");
  assert.equal(clampNumberDraft("1", -1, 1, 100), "1");
  assert.equal(clampNumberDraft("-5", -1, 0, 10), "0");
  assert.equal(clampNumberDraft("9999", 0, 0, 10_000), "9999");
});

test("clampNumberDraft keeps one decimal only for fractional steps", () => {
  assert.equal(clampNumberDraft("2", 1, 0, 10), "3");
  assert.equal(clampNumberDraft("2.5", 0.1, 0, 10, 0.1), "2.6");
  assert.equal(clampNumberDraft("0.2", 0.1, 0, 10, 0.1), "0.3");
  assert.equal(clampNumberDraft("2.04", 0.1, 0, 10, 0.1), "2.1");
});

test("formatBytes keeps sub-KiB counts exact and scales above", () => {
  assert.equal(formatBytes(0, "en"), "0 B");
  assert.equal(formatBytes(1023, "en"), "1023 B");
  assert.equal(formatBytes(1024, "en"), "1 KiB");
  assert.equal(formatBytes(1536, "en"), "1.5 KiB");
  assert.equal(formatBytes(1024 * 1024, "en"), "1 MiB");
});

test("formatBytes stops at TiB and groups larger counts", () => {
  assert.equal(formatBytes(1024 ** 4, "en"), "1 TiB");
  assert.equal(formatBytes(1024 ** 5, "en"), "1,024 TiB");
});

test("formatTokens steps by thousands for western locales", () => {
  assert.equal(formatTokens(0, "en"), "0");
  assert.equal(formatTokens(-5, "en"), "-5");
  assert.equal(formatTokens(9999, "en"), "9999");
  assert.equal(formatTokens(10_000, "en"), "10K");
  assert.equal(formatTokens(15_000, "en"), "15K");
  assert.equal(formatTokens(1_500_000, "en"), "1.5M");
  assert.equal(formatTokens(1_000_000_000, "en"), "1B");
  assert.equal(formatTokens(1_000_000_000_000, "en"), "1T");
});

test("formatTokens steps by the myriad for CJK locales", () => {
  assert.equal(formatTokens(9999, "ko"), "9999");
  assert.equal(formatTokens(10_000, "ko"), "1만");
  assert.equal(formatTokens(12_345, "ko"), "1.2만");
  assert.equal(formatTokens(100_000_000, "zh"), "1亿");
  assert.equal(formatTokens(100_000_000, "zh-TW"), "1億");
  assert.equal(formatTokens(10_000_000_000_000_000, "zh"), "1京");
});

test("formatUptime reports unknown and sub-minute ages", () => {
  assert.equal(formatUptime(Number.NaN, "en"), "\u2014");
  assert.equal(formatUptime(Number.POSITIVE_INFINITY, "en"), "\u2014");
  assert.equal(formatUptime(0, "en"), "0" + catalogValue("en", "uptime.second"));
  assert.equal(formatUptime(-30, "en"), "0" + catalogValue("en", "uptime.second"));
  assert.equal(formatUptime(299, "en"), "299" + catalogValue("en", "uptime.second"));
});

test("formatUptime switches units and keeps the second component", () => {
  const hour = catalogValue("en", "uptime.hour");
  const minute = catalogValue("en", "uptime.minute");
  const day = catalogValue("en", "uptime.day");
  assert.equal(formatUptime(300, "en"), "5" + minute);
  assert.equal(formatUptime(3599, "en"), "59" + minute);
  assert.equal(formatUptime(3600, "en"), "1" + hour);
  assert.equal(formatUptime(3660, "en"), "1" + hour + " 1" + minute);
  assert.equal(formatUptime(86_400, "en"), "1" + day);
  assert.equal(formatUptime(90_000, "en"), "1" + day + " 1" + hour);
});

test("section anchors keep the scope-section-id shape", () => {
  assert.equal(sectionAnchorId("api", "keys"), "api-section-keys");
  assert.equal(sectionAnchorPrefix("api"), "api-section-");
  assert.equal(sectionAnchorId("api", "keys").slice(sectionAnchorPrefix("api").length), "keys");
  assert.equal(SECTION_TAB_SCROLL_LOCK_MS, 1200);
});
/** Storage double that also reports the key it was last read or written with. */
function memoryStorage(initial?: string) {
  let value = initial ?? null;
  let lastKey: string | null = null;
  return {
    getItem(key: string): string | null {
      lastKey = key;
      return value;
    },
    setItem(key: string, next: string): void {
      lastKey = key;
      value = next;
    },
    lastKey(): string | null {
      return lastKey;
    },
  };
}

test("clipboard feedback carries the scope it settled for", () => {
  assert.equal(COPY_FEEDBACK_VISIBLE_MS, 2500);
  assert.deepEqual(settledCopy("openai", true), { scope: "openai", outcome: "copied" });
  assert.deepEqual(settledCopy("openai", false), { scope: "openai", outcome: "unavailable" });
  const settled = settledCopy("openai", true);
  assert.equal(feedbackOutcome(settled, "openai"), "copied");
  assert.equal(feedbackOutcome(settled, "anthropic"), null);
  assert.equal(feedbackOutcome(null, "openai"), null);
});

test("only the newest clipboard attempt may publish", () => {
  const attempts = createAttemptSequencer();
  const first = attempts.begin();
  const second = attempts.begin();
  assert.equal(attempts.isNewest(first), false);
  assert.equal(attempts.isNewest(second), true);
  const third = attempts.begin();
  assert.equal(attempts.isNewest(second), false);
  assert.equal(attempts.isNewest(third), true);
});

test("oauthTosDialogPlan answers only for providers the warning covers", () => {
  assert.equal(oauthTosDialogPlan("openai"), null);
  assert.equal(oauthTosDialogPlan(""), null);
  assert.deepEqual(oauthTosDialogPlan(" Anthropic "), {
    level: "high",
    usesProviderBody: true,
    offersApiKeyPath: true,
  });
  assert.deepEqual(oauthTosDialogPlan("google-antigravity"), {
    level: "high",
    usesProviderBody: false,
    offersApiKeyPath: true,
  });
  assert.deepEqual(oauthTosDialogPlan("github-copilot"), {
    level: "elevated",
    usesProviderBody: false,
    offersApiKeyPath: false,
  });
  assert.deepEqual(oauthTosDialogPlan("cursor"), {
    level: "elevated",
    usesProviderBody: false,
    offersApiKeyPath: false,
  });
});

test("feature-flag responses are read literally", () => {
  assert.equal(featureFlagEnabled({ enabled: true }), true);
  assert.equal(featureFlagEnabled({}), false);
  assert.equal(featureFlagEnabled({ enabled: "true" }), false);
  assert.equal(featureFlagWriteAccepted({ ok: true }), true);
  assert.equal(featureFlagWriteAccepted({}), false);
  assert.equal(featureFlagSavedMessageKey({ changed: true }), "codexAuth.requestUserInputUpdatedRestart");
  assert.equal(featureFlagSavedMessageKey({ changed: false }), "codexAuth.requestUserInputUpdated");
});

test("a failed feature-flag write keeps transport words but not a bare status", () => {
  const keyOnly: TFn = key => key;
  assert.equal(
    featureFlagFailureMessage(keyOnly, new Error("dial tcp 127.0.0.1:23100: connectex: connection refused")),
    "dial tcp 127.0.0.1:23100: connectex: connection refused",
  );
  assert.equal(featureFlagFailureMessage(keyOnly, new Error("HTTP 502")), "codexAuth.requestUserInputUpdateFailed");
  assert.equal(featureFlagFailureMessage(keyOnly, new Error("")), "codexAuth.requestUserInputUpdateFailed");
  assert.equal(featureFlagFailureMessage(keyOnly, "boom"), "codexAuth.requestUserInputUpdateFailed");
});

test("fmtK compacts whole thousands and stops at the unit it can name", () => {
  assert.equal(fmtK(0), "0");
  assert.equal(fmtK(-1), "-1");
  assert.equal(fmtK(Number.POSITIVE_INFINITY), "Infinity");
  assert.equal(fmtK(12_345), (12_345).toLocaleString());
  assert.equal(fmtK(350_000), "350k");
  assert.equal(fmtK(999_000), "999k");
  assert.equal(fmtK(1_000_000), "1M");
  assert.equal(fmtK(1_050_000), "1.05M");
});

test("catalog ladders keep the values the listeners advertise", () => {
  assert.equal(CAP_OPTIONS.length, 18);
  assert.equal(CAP_OPTIONS.at(0), 100_000);
  assert.equal(CAP_OPTIONS.at(-1), 950_000);
  assert.equal(CAP_OPTION_SET.has(400_000), true);
  assert.equal(CAP_OPTION_SET.has(425_000), false);
  assert.equal(NATIVE_GPT56_DEFAULT_WINDOW, 1_050_000);
  assert.deepEqual(NATIVE_CAP_OPTIONS, [272_000, 400_000, 922_000, 1_050_000]);
  assert.equal(NATIVE_CAP_OPTION_SET.has(272_000), true);
  assert.equal(NATIVE_CAP_OPTION_SET.has(950_000), false);
  assert.equal(THREAD_OPTION_SET.has(1000), true);
  assert.equal(THREAD_OPTION_SET.has(512), false);
  assert.equal(PAGE, 60);
  assert.equal(CUSTOM_OPTION, "custom");
  const effortLevels: readonly string[] = REASONING_EFFORT_LEVELS;
  assert.equal(effortLevels.includes("xhigh"), true);
  assert.equal(effortLevels.includes("ultra"), false);
});

test("collectDisabledNamespaced keys off the disabled flag only", () => {
  const rows = [
    { provider: "openai", id: "gpt-5", namespaced: "openai/gpt-5", disabled: true },
    { provider: "openai", id: "gpt-5-mini", namespaced: "openai/gpt-5-mini", disabled: false },
  ];
  assert.deepEqual([...collectDisabledNamespaced(rows)], ["openai/gpt-5"]);
  assert.deepEqual([...collectDisabledNamespaced([])], []);
});

test("discoveryFailureLabel interpolates a status only when the failure has one", () => {
  const seen: string[] = [];
  const t: TFn = (key, vars) => {
    seen.push(vars ? `status=${String(vars.status)}` : "no vars");
    return key;
  };
  assert.equal(
    discoveryFailureLabel(t, { status: "failed", reason: "http", httpStatus: 503 }),
    "models.discoveryFailedHttp",
  );
  assert.equal(seen.at(-1), "status=503");
  assert.equal(discoveryFailureLabel(t, { status: "failed", reason: "network" }), "models.discoveryFailedNetwork");
  assert.equal(seen.at(-1), "no vars");
  const futureReason = { status: "failed", reason: "future" } as unknown as DiscoveryFailure;
  assert.equal(discoveryFailureLabel(t, futureReason), "models.discoveryFailedGeneric");
});

test("the collapsed-provider preference round-trips under one storage key", () => {
  const store = memoryStorage();
  assert.equal(readCollapsedProviders(store), null);
  writeCollapsedProviders(new Set(["openai", "anthropic"]), store);
  assert.equal(store.lastKey(), "benes-models-collapsed:v2");
  assert.deepEqual([...(readCollapsedProviders(store) ?? [])], ["openai", "anthropic"]);
  writeCollapsedProviders(new Set(), store);
  assert.deepEqual([...(readCollapsedProviders(store) ?? ["unexpected"])], []);
});

test("an unreadable collapse preference means no preference", () => {
  assert.equal(readCollapsedProviders(memoryStorage("{")), null);
  assert.equal(readCollapsedProviders(memoryStorage('{"openai":true}')), null);
  assert.deepEqual([...(readCollapsedProviders(memoryStorage('["openai",7,null]')) ?? [])], ["openai"]);
  // No localStorage at all (tests, sandboxed frames) reads as "no preference" and must not throw.
  assert.equal(readCollapsedProviders(), null);
  writeCollapsedProviders(new Set(["openai"]));
});

test("a storage that refuses to read or write leaves the Models board alone", () => {
  const refusing = {
    getItem(): string | null {
      throw new Error("SecurityError: storage denied");
    },
    setItem(): void {
      throw new Error("QuotaExceededError");
    },
  };
  // A denied read is the same answer as an empty storage: the board applies its default.
  assert.equal(readCollapsedProviders(refusing), null);
  // A refused write (quota, private mode) is a lost convenience, never a broken board.
  assert.doesNotThrow(() => writeCollapsedProviders(new Set(["openai"]), refusing));
});
