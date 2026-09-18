import assert from "node:assert/strict";
import test from "node:test";

test("maskEmailAddress censors the local part and keeps the domain", async () => {
  const { maskEmailAddress } = await import("../src/lib/privacy.ts");
  assert.equal(maskEmailAddress("ada@example.com"), "a***@example.com");
  assert.equal(maskEmailAddress("JK@Example.COM"), "J***@Example.COM");
  assert.equal(maskEmailAddress("Codex App login"), null);
  assert.equal(maskEmailAddress("account-…12ab"), null);
  assert.equal(maskEmailAddress(""), null);
});

test("formatAccessQuotaReset uses remaining duration, not a calendar date", async () => {
  const { formatAccessQuotaReset } = await import("../src/provider-workspace/quota-presentation.ts");
  const t = (key, vars = {}) => {
    if (key === "quota.resetsInCompact") return `Resets in ${vars.wait}`;
    if (key === "quota.resetsRelativeMinutes") return `Resets in ${vars.n} min`;
    return key;
  };
  const now = Date.parse("2026-08-30T02:00:00Z");
  const inFiveDaysTwelveHours = now + (((5 * 24) + 12) * 60 * 60 * 1000);
  assert.equal(formatAccessQuotaReset(inFiveDaysTwelveHours, t, now), "Resets in 5d 12h");
  assert.equal(formatAccessQuotaReset(now + 40 * 60 * 1000, t, now), "Resets in 40 min");
});

test("remainingQuotaPercent is ChatGPT remaining, not WHAM used", async () => {
  const { remainingQuotaPercent, accountQuotaFromCodexAccounts } = await import("../src/codex-quota-utils.ts");
  assert.equal(remainingQuotaPercent(18), 82);
  assert.equal(remainingQuotaPercent(15), 85);
  assert.equal(remainingQuotaPercent(0), 100);
  assert.equal(remainingQuotaPercent(100), 0);

  const quota = accountQuotaFromCodexAccounts({
    shortPercent: 15,
    shortResetAt: 1,
    weeklyPercent: 18,
    weeklyResetAt: 2,
    updatedAt: 3,
  });
  assert.equal(quota?.fiveHourPercent, 15);
  assert.equal(quota?.fiveHourResetAt, 1);
  assert.equal(quota?.weeklyPercent, 18);
  assert.equal(remainingQuotaPercent(quota?.fiveHourPercent ?? 0), 85);
  assert.equal(remainingQuotaPercent(quota?.weeklyPercent ?? 0), 82);
});

test("accessWeeklyQuotaCell is weekly remaining plus reset, not a stacked 5h line", async () => {
  const { accessWeeklyQuotaCell } = await import("../src/codex-quota-utils.ts");
  assert.deepEqual(
    accessWeeklyQuotaCell({ weeklyPercent: 21, weeklyResetAt: 99, fiveHourPercent: 32, fiveHourResetAt: 1 }),
    { remainingPercent: 79, resetAt: 99 },
  );
  assert.deepEqual(accessWeeklyQuotaCell({ weeklyPercent: 21 }), { remainingPercent: 79 });
  assert.equal(accessWeeklyQuotaCell({ fiveHourPercent: 32, fiveHourResetAt: 1 }), null);
  assert.equal(accessWeeklyQuotaCell(null), null);
});

test("formatElapsedSince spells out minutes", async () => {
  const { formatElapsedSince } = await import("../src/relative-clock.ts");
  const now = Date.parse("2026-08-30T02:00:00Z");
  assert.equal(formatElapsedSince(now - 5 * 60_000, undefined, now), "5 min ago");
});
