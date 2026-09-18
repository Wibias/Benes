/**
 * End-to-end contract for the dashboard's hash router.
 *
 * The existing suites pin the Routing/Usage/Sessions/Codex-Auth families they own. This
 * one pins the parts they do not: the passive-versus-deliberate split in `hash-routing.ts`,
 * the query delegation to the page-local validators, the complete legacy rewrite table,
 * and the membership rule every canonical hash has to satisfy.
 */
import assert from "node:assert/strict";
import test from "node:test";

import {
  hashBelongsToPage,
  readPageFromHash,
  resolveAppHashChange,
  type Page,
} from "../src/app-routing.ts";
import { navigateHash, normalizeHashPath, parseHashRoute, replaceHash } from "../src/hash-routing.ts";

interface HistoryCall {
  state: unknown;
  url: string;
}

/** Minimal stand-in for the browser globals `hash-routing.ts` touches. */
function fakeWindow(hash: string) {
  const replaced: HistoryCall[] = [];
  const pushed: HistoryCall[] = [];
  const win = {
    location: { hash, pathname: "/", search: "?keep=1" },
    history: {
      state: { entry: "current" },
      replaceState(state: unknown, _title: string, url: string) {
        replaced.push({ state, url });
        win.location.hash = url.slice(url.indexOf("#"));
      },
      pushState(state: unknown, _title: string, url: string) {
        pushed.push({ state, url });
      },
    },
  };
  return { win: win as unknown as Window, replaced, pushed };
}

test("normalizeHashPath strips one leading marker and leaves the rest alone", () => {
  assert.equal(normalizeHashPath("#logs"), "logs");
  assert.equal(normalizeHashPath("#/logs"), "logs");
  assert.equal(normalizeHashPath("logs"), "logs");
  assert.equal(normalizeHashPath("#"), "");
  assert.equal(normalizeHashPath("#/"), "");
  assert.equal(normalizeHashPath(""), "");
  // Only one marker is consumed; a later marker is page content.
  assert.equal(normalizeHashPath("##logs"), "#logs");
  assert.equal(normalizeHashPath("logs#!/usr/bin"), "logs#!/usr/bin");
});

test("parseHashRoute splits on the first question mark only", () => {
  const routed = parseHashRoute("#logs?sessionId=ses_123");
  assert.equal(routed.path, "logs");
  assert.equal(routed.query.get("sessionId"), "ses_123");

  const noQuery = parseHashRoute("storage/cleanup");
  assert.equal(noQuery.path, "storage/cleanup");
  assert.equal([...noQuery.query.keys()].length, 0);

  const doubled = parseHashRoute("logs?a=1?b=2");
  assert.equal(doubled.path, "logs");
  assert.equal(doubled.query.get("a"), "1?b=2");
  assert.equal(doubled.query.get("b"), null);
});

test("parseHashRoute defers to the platform query grammar", () => {
  assert.equal(parseHashRoute("sessions?policy=a+b").query.get("policy"), "a b");
  assert.equal(parseHashRoute("sessions?policy=a%20b").query.get("policy"), "a b");
  assert.deepEqual(parseHashRoute("logs?sessionId=1&sessionId=2").query.getAll("sessionId"), ["1", "2"]);
  assert.equal(parseHashRoute("logs?sessionId").query.get("sessionId"), "");
});

test("replaceHash rewrites the current entry and keeps the path and search", () => {
  const { win, replaced, pushed } = fakeWindow("#old");
  replaceHash("logs?sessionId=ses_1", win);
  assert.deepEqual(replaced, [{ state: { entry: "current" }, url: "/?keep=1#logs?sessionId=ses_1" }]);
  assert.deepEqual(pushed, []);
});

test("replaceHash is a no-op for the hash already in the address bar", () => {
  for (const current of ["#logs", "logs"]) {
    const { win, replaced } = fakeWindow(current);
    replaceHash("logs", win);
    assert.deepEqual(replaced, [], current);
  }
  const marked = fakeWindow("#/logs");
  replaceHash("#logs", marked.win);
  assert.deepEqual(marked.replaced, []);
});

test("replaceHash accepts either marker spelling and never pushes", () => {
  const { win, replaced } = fakeWindow("");
  replaceHash("#/routing/combos", win);
  assert.deepEqual(replaced, [{ state: { entry: "current" }, url: "/?keep=1#routing/combos" }]);
});

test("navigateHash assigns the location hash so the browser pushes an entry", () => {
  const { win, replaced, pushed } = fakeWindow("");
  navigateHash("models", win);
  assert.equal(win.location.hash, "models");
  assert.deepEqual(replaced, []);
  assert.deepEqual(pushed, []);
});

test("navigateHash leaves an unchanged target where it is", () => {
  const { win } = fakeWindow("#models");
  navigateHash("models", win);
  assert.equal(win.location.hash, "#models");
});

test("navigateHash normalizes the marker before assigning", () => {
  const { win } = fakeWindow("");
  navigateHash("#/usage", win);
  assert.equal(win.location.hash, "usage");
});

const CANONICAL_HASHES: ReadonlyMap<Page, readonly string[]> = new Map([
  ["dashboard", ["dashboard", "dashboard/providers", "dashboard/models"]],
  ["startup", ["startup"]],
  ["routing", ["routing", "routing/combos", "routing/evaluation", "routing/analytics", "routing/compatibility"]],
  ["providers", ["providers", "providers/anthropic/access", "providers/openai/access"]],
  ["models", ["models"]],
  ["subagents", ["subagents"]],
  ["sessions", ["sessions", "sessions?policy=primary", "sessions?combo=fast"]],
  ["logs", ["logs", "logs/debug", "logs?sessionId=ses_1"]],
  ["usage", ["usage", "usage/breakdown", "usage/breakdown/providers", "usage/breakdown/accounts", "usage/coverage"]],
  ["tasks", ["tasks"]],
  ["storage", ["storage", "storage/cleanup", "storage/quarantine"]],
  ["api", ["api", "api/clients", "api/endpoints", "api/models", "api/examples"]],
  ["integrations", ["integrations"]],
  ["harnesses", ["harnesses", "harnesses/claude", "harnesses/grok", "harnesses/claude-desktop"]],
]);

test("every canonical hash resolves to its own page without a rewrite", () => {
  for (const [page, hashes] of CANONICAL_HASHES) {
    for (const hash of hashes) {
      assert.equal(hashBelongsToPage(hash, page), true, `${hash} belongs to ${page}`);
      // `integrations` is a transition alias: its hashes resolve through to the pages
      // that inherited the surface, so only the final owners round-trip exactly.
      if (page === "integrations") {
        assert.equal(readPageFromHash(`#${hash}`), "harnesses", `#${hash}`);
        continue;
      }
      assert.equal(readPageFromHash(`#${hash}`), page, `#${hash}`);
      assert.deepEqual(resolveAppHashChange(hash), { page, replaceTo: null }, hash);
    }
  }
});

test("a page does not own a sibling's canonical hash", () => {
  assert.equal(hashBelongsToPage("dashboard", "models"), false);
  assert.equal(hashBelongsToPage("dashboard/providers", "providers"), false);
  assert.equal(hashBelongsToPage("routing/combos", "startup"), false);
  assert.equal(hashBelongsToPage("api/models", "models"), false);
  assert.equal(hashBelongsToPage("harnesses", "integrations"), false);
  assert.equal(hashBelongsToPage("integrations", "harnesses"), false);
  assert.equal(hashBelongsToPage("storage/cleanup", "logs"), false);
});

test("an unknown root resolves to the dashboard", () => {
  for (const hash of ["nope", "nope/deep", ""]) {
    assert.equal(readPageFromHash(`#${hash}`), "dashboard", hash);
  }
  assert.deepEqual(resolveAppHashChange("nope/deep"), { page: "dashboard", replaceTo: "dashboard" });
});

test("an unowned subpath normalizes to its page's canonical root", () => {
  const cases: ReadonlyArray<readonly [string, Page]> = [
    ["routing/nope", "routing"],
    ["dashboard/nope", "dashboard"],
    ["models/zzz", "models"],
    ["subagents/zzz", "subagents"],
    ["tasks/zzz", "tasks"],
    ["api/zzz", "api"],
    ["api/models/extra", "api"],
    ["usage/zzz", "usage"],
  ];
  for (const [hash, page] of cases) {
    assert.equal(readPageFromHash(`#${hash}`), page, hash);
    assert.deepEqual(resolveAppHashChange(hash), { page, replaceTo: page }, hash);
  }
});

test("query state outside the owning page's grammar is not silently dropped", () => {
  assert.equal(hashBelongsToPage("logs?sessionId=ses_1", "logs"), true);
  assert.equal(hashBelongsToPage("logs?other=1", "logs"), false);
  assert.equal(hashBelongsToPage("logs/debug?sessionId=ses_1", "logs"), false);
  assert.deepEqual(resolveAppHashChange("logs?other=1"), { page: "logs", replaceTo: "logs" });

  assert.equal(hashBelongsToPage("sessions?policy=", "sessions"), false);
  assert.equal(hashBelongsToPage("sessions?policy=a&combo=b", "sessions"), false);
  assert.deepEqual(resolveAppHashChange("sessions?policy="), { page: "sessions", replaceTo: "sessions" });

  assert.equal(hashBelongsToPage("storage/quarantine?x=1", "storage"), false);
  assert.equal(hashBelongsToPage("dashboard/providers?x=1", "dashboard"), false);
  assert.equal(hashBelongsToPage("routing/combos?x=1", "routing"), false);
});

test("provider Access hashes are delegated to the provider workspace reader", () => {
  assert.equal(hashBelongsToPage("providers/anthropic/access", "providers"), true);
  assert.equal(hashBelongsToPage("providers/anthropic/overview", "providers"), false);
  assert.equal(hashBelongsToPage("providers/anthropic/access/extra", "providers"), false);
  assert.deepEqual(resolveAppHashChange("providers/anthropic/overview"), { page: "providers", replaceTo: "providers" });
});

test("retired roots resolve to their canonical owner on a cold load", () => {
  const cold: ReadonlyArray<readonly [string, Page]> = [
    ["#debug", "logs"],
    ["#debug/tail", "logs"],
    ["#codex-auth", "providers"],
    ["#codex-auth/accounts", "providers"],
    ["#combos", "routing"],
    ["#combos/free", "routing"],
    ["#evaluation", "routing"],
    ["#analytics", "routing"],
    ["#lab", "routing"],
    ["#startup/compatibility", "routing"],
    ["#startup/routing", "routing"],
    ["#startup/combos", "routing"],
    ["#models/combos", "routing"],
    ["#models/compatibility", "routing"],
    ["#integrations", "harnesses"],
    ["#integrations/claude/desktop", "harnesses"],
    ["#integrations/grok", "harnesses"],
    ["#integrations/keys", "api"],
    ["#integrations/keys/abc", "api"],
    ["#claude", "harnesses"],
    ["#grok", "harnesses"],
    ["#providers/workspace", "providers"],
  ];
  for (const [hash, page] of cold) {
    assert.equal(readPageFromHash(hash), page, hash);
  }
});

const LEGACY_REWRITES: ReadonlyArray<readonly [string, Page, string]> = [
  ["debug", "logs", "logs/debug"],
  ["debug/tail", "logs", "logs/debug"],
  ["codex-auth", "providers", "providers/openai/access"],
  ["codex-auth/", "providers", "providers/openai/access"],
  ["codex-auth/accounts", "providers", "providers/openai/access"],
  ["startup/routing", "routing", "routing"],
  ["startup/routing/deep", "routing", "routing"],
  ["startup/compatibility", "routing", "routing/compatibility"],
  ["startup/combos", "routing", "routing/combos"],
  ["startup/evaluation", "routing", "routing/evaluation"],
  ["startup/analytics", "routing", "routing/analytics"],
  ["models/routing", "routing", "routing"],
  ["models/compatibility", "routing", "routing/compatibility"],
  ["models/combos", "routing", "routing/combos"],
  ["lab", "routing", "routing/compatibility"],
  ["lab/verdicts", "routing", "routing/compatibility"],
  ["combos", "routing", "routing/combos"],
  ["combos/free", "routing", "routing/combos"],
  ["evaluation", "routing", "routing/evaluation"],
  ["evaluation/foo", "routing", "routing/evaluation"],
  ["analytics", "routing", "routing/analytics"],
  ["analytics/foo", "routing", "routing/analytics"],
  ["api/keys", "api", "api"],
  ["integrations/keys", "api", "api"],
  ["integrations/keys/abc", "api", "api"],
  ["integrations", "harnesses", "harnesses"],
  ["integrations/", "harnesses", "harnesses"],
  ["integrations/claude/desktop", "harnesses", "harnesses/claude-desktop"],
  ["integrations/claude", "harnesses", "harnesses/claude"],
  ["integrations/grok", "harnesses", "harnesses/grok"],
  ["claude", "harnesses", "harnesses/claude"],
  ["grok", "harnesses", "harnesses/grok"],
  ["providers/workspace", "providers", "providers"],
];

test("every retired bookmark passively rewrites to its canonical hash", () => {
  for (const [legacy, page, replaceTo] of LEGACY_REWRITES) {
    assert.deepEqual(resolveAppHashChange(legacy), { page, replaceTo }, legacy);
  }
});

test("a rewritten target is itself canonical", () => {
  for (const [, page, replaceTo] of LEGACY_REWRITES) {
    assert.equal(hashBelongsToPage(replaceTo, page), true, `${replaceTo} must belong to ${page}`);
    assert.deepEqual(resolveAppHashChange(replaceTo), { page, replaceTo: null }, replaceTo);
  }
});

test("the resolver treats a leading marker as literal content, not as a prefix", () => {
  // Production always feeds this function `normalizeHashPath(location.hash)`. Called with
  // the raw marker no legacy prefix matches, so the owning page's root is what comes back;
  // normalizing first is what reaches the surface rewrite.
  assert.deepEqual(resolveAppHashChange("#combos"), { page: "routing", replaceTo: "routing" });
  assert.deepEqual(resolveAppHashChange("#models/combos"), { page: "routing", replaceTo: "routing" });
  assert.deepEqual(
    resolveAppHashChange(normalizeHashPath("#combos")),
    { page: "routing", replaceTo: "routing/combos" },
  );
  assert.deepEqual(
    resolveAppHashChange(normalizeHashPath("#models/combos")),
    { page: "routing", replaceTo: "routing/combos" },
  );
});
