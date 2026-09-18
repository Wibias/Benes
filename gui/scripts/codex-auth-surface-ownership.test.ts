/**
 * #236 correction: the standalone Codex Auth page is retired, and Providers → OpenAI →
 * Access owns the whole account surface. These assertions pin the routing contract, the
 * single-owner topology, and the survival of every behaviour the standalone page held.
 */
import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import { VALID_PAGES, hashBelongsToPage, readPageFromHash, resolveAppHashChange } from "../src/app-routing.ts";
import { PAGE_TKEY } from "../src/app-shell.ts";
import {
  CODEX_ACCOUNT_PROVIDER,
  OPENAI_ACCOUNT_ACCESS_HASH,
  nextProviderAccessTarget,
  providerAccessHash,
  readProviderAccessHash,
} from "../src/provider-workspace/provider-access-hash.ts";
import {
  accountsFocusAdjustment,
  scopedAccountsFocusToken,
} from "../src/provider-workspace/detail-tabs.ts";
import { nextDetailTabIndex } from "../src/provider-workspace/connection-test.ts";
import { accessDescriptorPolicy } from "../src/provider-workspace/access-policy.ts";
import {
  accessTableScrolls,
  credentialSelectionDisplay,
  credentialSelectionFacts,
} from "../src/provider-workspace/access-presentation.ts";

const guiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const source = (relativePath: string) => readFileSync(path.join(guiRoot, relativePath), "utf8");
const exists = (relativePath: string) => existsSync(path.join(guiRoot, relativePath));

function guiSources(): Array<{ path: string; source: string }> {
  const srcRoot = path.join(guiRoot, "src");
  return (readdirSync(srcRoot, { recursive: true }) as string[])
    .map(entry => entry.replace(/\\/g, "/"))
    .filter(entry => /\.[cm]?tsx?$/.test(entry))
    .map(entry => ({ path: `src/${entry}`, source: readFileSync(path.join(srcRoot, entry), "utf8") }));
}

function filesMatching(needle: string): string[] {
  return guiSources().filter(file => file.source.includes(needle)).map(file => file.path).sort();
}

test("codex-auth is no longer a first-class page", () => {
  assert.equal(VALID_PAGES.has("codex-auth" as never), false);
  assert.equal(Object.prototype.hasOwnProperty.call(PAGE_TKEY, "codex-auth"), false);
  assert.equal(readPageFromHash("#codex-auth"), "providers");
  assert.equal(readPageFromHash("#codex-auth/pool"), "providers");
});

test("legacy #codex-auth bookmarks passively land on Providers -> OpenAI -> Access", () => {
  for (const legacy of ["codex-auth", "codex-auth/", "codex-auth/accounts"]) {
    assert.deepEqual(
      resolveAppHashChange(legacy),
      { page: "providers", replaceTo: OPENAI_ACCOUNT_ACCESS_HASH },
      legacy,
    );
  }
  assert.equal(OPENAI_ACCOUNT_ACCESS_HASH, providerAccessHash(CODEX_ACCOUNT_PROVIDER));
  assert.equal(OPENAI_ACCOUNT_ACCESS_HASH, "providers/openai/access");
  assert.equal(hashBelongsToPage(OPENAI_ACCOUNT_ACCESS_HASH, "providers"), true);
  assert.deepEqual(resolveAppHashChange(OPENAI_ACCOUNT_ACCESS_HASH), { page: "providers", replaceTo: null });
});

test("the canonical deep link reads exactly one provider name", () => {
  assert.equal(readProviderAccessHash("providers/openai/access"), "openai");
  assert.equal(readProviderAccessHash("#providers/anthropic/access"), "anthropic");
  for (const other of [
    "providers",
    "providers/openai",
    "providers/openai/overview",
    "providers/openai/access/extra",
    "providers//access",
    "providers/openai/access?refresh=1",
    "codex-auth",
  ]) {
    assert.equal(readProviderAccessHash(other), null, other);
  }
  // An unknown provider still parses: the workspace decides what a name it does not have
  // means, and must not crash resolving it.
  assert.equal(readProviderAccessHash("providers/retired-name/access"), "retired-name");
  assert.deepEqual(
    resolveAppHashChange("providers/retired-name/access"),
    { page: "providers", replaceTo: null },
  );
});

test("a deep link is consumed once and replays only after the hash moves away", () => {
  const target = "providers/openai/access";
  // Cold load and refresh both read the hash once.
  const cold = nextProviderAccessTarget({ applied: null }, target);
  assert.equal(cold.provider, "openai");
  // A repeat read of the same hash — a re-render, or a config reload — must not re-reveal.
  const repeated = nextProviderAccessTarget(cold.state, target);
  assert.equal(repeated.provider, null);
  // Back: the hash moves away, which re-arms the deep link.
  const away = nextProviderAccessTarget(repeated.state, "providers");
  assert.deepEqual(away.state, { applied: null });
  assert.equal(away.provider, null);
  // Forward: the same deep link applies again.
  assert.equal(nextProviderAccessTarget(away.state, target).provider, "openai");
});

test("a cold legacy bookmark still reveals the OpenAI Access tab", () => {
  // The router rewrites `#codex-auth` with replaceState, which emits no hashchange, and the
  // workspace reads the hash before that rewrite lands. The reader therefore resolves the
  // retired page itself, and the rewrite must not read as a second deep link.
  const cold = nextProviderAccessTarget({ applied: null }, "codex-auth");
  assert.equal(cold.provider, "openai");
  assert.deepEqual(cold.state, { applied: OPENAI_ACCOUNT_ACCESS_HASH });
  assert.equal(nextProviderAccessTarget(cold.state, "codex-auth/pool").provider, null);
  assert.equal(nextProviderAccessTarget(cold.state, OPENAI_ACCOUNT_ACCESS_HASH).provider, null);
  // Leaving and returning still applies it.
  const away = nextProviderAccessTarget(cold.state, "providers");
  assert.deepEqual(away.state, { applied: null });
  assert.equal(nextProviderAccessTarget(away.state, OPENAI_ACCOUNT_ACCESS_HASH).provider, "openai");
});

test("the OpenAI account reveal still selects the Access tab for that provider only", () => {
  // `reveal-accounts` bumps a token; useDetailTabs turns a changed token into the Access tab.
  assert.deepEqual(accountsFocusAdjustment(0, 0), null);
  assert.deepEqual(accountsFocusAdjustment(1, 0), { seen: 1, tab: "access" });
  assert.deepEqual(accountsFocusAdjustment(2, 1), { seen: 2, tab: "access" });
  assert.equal(scopedAccountsFocusToken("openai", "openai", 4), 4);
  assert.equal(scopedAccountsFocusToken("anthropic", "openai", 4), 0);
  assert.equal(scopedAccountsFocusToken(null, "openai", 4), 0);
});

test("Providers supplies the one shared Codex account controller", () => {
  const providers = source("src/pages/Providers.tsx");
  assert.equal(providers.match(/useCodexAccountPool\(/g)?.length, 1);
  assert.match(providers, /codexController: codexPool/);
  assert.match(providers, /type: "reveal-accounts"/);
  assert.match(providers, /useProviderAccessDeepLink\(\)/);
});

test("the embedded Codex account pool owns no policy stack of its own", () => {
  const pool = source("src/components/CodexAccountPool.tsx");
  for (const forbidden of [
    "useCodexAutoSwitch",
    "usePoolRotation",
    "CodexPoolStrategySetting",
    "CodexAuthAdvancedSettings",
    "api/codex-auth/active",
    "api/codex-auth/auto-switch",
    "api/codex-auth/pool-strategy",
  ]) {
    assert.equal(pool.includes(forbidden), false, `CodexAccountPool must not own ${forbidden}`);
  }
});

test("the Access page is the only auto-switch and rotation owner", () => {
  assert.deepEqual(filesMatching("useCodexAutoSwitch(apiBase"), [
    "src/components/provider-workspace/ProviderAccess.tsx",
  ]);
  assert.deepEqual(filesMatching("<CodexPoolStrategySetting"), [
    "src/components/provider-workspace/ProviderAccess.tsx",
  ]);
  assert.deepEqual(filesMatching("<CodexAutoSwitchSetting"), [
    "src/components/provider-workspace/ProviderAccess.tsx",
  ]);
  // The Access page subscribes to the pool's reads instead of opening its own /active read.
  const access = source("src/components/provider-workspace/ProviderAccess.tsx");
  assert.equal(access.includes("subscribeLoadObserver"), true);
  assert.equal(access.includes("readLastThreshold"), true);
  assert.equal(access.includes("readLastActive"), true);
  assert.match(access, /codexController \?\? \{\}/);
  assert.equal(access.includes("/api/codex-auth/active"), false);
});

test("no standalone CodexAuth consumer remains", () => {
  for (const retired of [
    "src/pages/CodexAuth.tsx",
    "src/codex-auth-page-state.ts",
    "src/components/CodexAuthAdvancedSettings.tsx",
    "src/components/codex-account-pool-cards.tsx",
    "src/components/codex-account-pool-card.tsx",
    "src/components/codex-account-pool-main-card.tsx",
    "src/components/codex-account-main-card-sections.tsx",
    "src/components/AccountPriorityControl.tsx",
    "src/components/AccountPriorityBadge.tsx",
    "scripts/codex-auth-page-state.test.ts",
  ]) {
    assert.equal(exists(retired), false, `${retired} should be gone`);
  }
  assert.deepEqual(filesMatching("CodexAccountPoolStandaloneSurface"), []);
  assert.deepEqual(filesMatching("pages/CodexAuth\""), []);
  assert.equal(source("src/app-page.tsx").includes("codex-auth"), false);
});

test("the OAuth / API-key lane keeps its tab keyboard contract", () => {
  assert.equal(nextDetailTabIndex("ArrowRight", 0, 2), 1);
  assert.equal(nextDetailTabIndex("ArrowRight", 1, 2), 0);
  assert.equal(nextDetailTabIndex("ArrowLeft", 0, 2), 1);
  assert.equal(nextDetailTabIndex("Home", 1, 2), 0);
  assert.equal(nextDetailTabIndex("End", 0, 2), 1);
  assert.equal(nextDetailTabIndex("Tab", 0, 2), null);
  assert.equal(nextDetailTabIndex("ArrowRight", 0, 0), null);
});

test("the OAuth lane keeps every account action the standalone page offered", () => {
  const lane = source("src/components/provider-workspace/OAuthAccountsTable.tsx");
  for (const action of [
    "onRefresh",
    "onPauseExhausted",
    "onAddAccount",
    "onTogglePause",
    "onPriorityChange",
    "onReauth",
    "onEditAlias",
    "onRemove",
    "onSwitch",
    "onOpenReset",
  ]) {
    assert.equal(lane.includes(action), true, `lane lost ${action}`);
  }
  // Switching and reset-credit redemption used to live only on the standalone cards.
  assert.match(source("src/components/codex-account-pool-surfaces.tsx"), /CodexAccountSwitchModal/);
  assert.match(source("src/components/codex-account-pool-surfaces.tsx"), /CodexAccountResetModal/);
  assert.match(source("src/components/codex-account-pool-surfaces.tsx"), /AddCodexAccountModal/);
});

test("Default access and Credential selection stay on the Access page", () => {
  const access = source("src/components/provider-workspace/ProviderAccess.tsx");
  assert.match(access, /prov\.access\.defaultAccess/);
  assert.match(access, /prov\.access\.credentialSelection/);
  for (const fact of ["prov.access.mode", "prov.access.strategy", "prov.access.autoSwitch", "prov.access.stickyLimit"]) {
    assert.equal(access.includes(fact), true, `Credential selection lost ${fact}`);
  }
  assert.match(access, /editSelection/);
  const configSections = source("src/components/provider-workspace/config-sections.tsx");
  assert.match(configSections, /CodexAccountPickerSetting/);
  assert.match(configSections, /DefaultModeRequestUserInputSetting/);
});

/**
 * The lane's row menu hangs below its row, so an overflow container cuts the actions off.
 * A pool that fits the table's three-row window must not clip them; a longer pool scrolls,
 * which is where the menu can be scrolled into view.
 */
test("the Access table only becomes a scroll window when the pool outgrows it", () => {
  assert.equal(accessTableScrolls(0), false);
  assert.equal(accessTableScrolls(1), false);
  assert.equal(accessTableScrolls(3), false);
  assert.equal(accessTableScrolls(4), true);
  assert.match(
    source("src/components/provider-workspace/OAuthAccountsTable.tsx"),
    /providers-access-table-body--scroll/,
  );
});

/**
 * The retired standalone page rendered the rotation card unconditionally, including for an
 * install that holds one account. One account has nothing to rotate, so the strategy, the
 * auto-switch threshold and the sticky limit have no observable effect there; the canonical
 * Access page keeps the shipped rule and dashes the values out. Two credentials is where
 * the editor opens, which is exactly where those settings start to matter.
 */
test("the Credential selection editor opens only once a credential can rotate", () => {
  const descriptor = accessDescriptorPolicy(
    { name: "openai", codexAccountMode: "pool" },
    { accountProvider: true, localProvider: false, apiLanePresent: false },
  );
  const selection = descriptor.selection;
  assert.ok(selection);

  assert.deepEqual(
    credentialSelectionDisplay(credentialSelectionFacts(selection), "quota", false),
    { mode: "—", strategy: "—", autoSwitch: "—", sticky: "—" },
  );
  assert.equal(
    credentialSelectionDisplay(credentialSelectionFacts(selection), "quota", true).mode,
    "pool",
  );

  const access = source("src/components/provider-workspace/ProviderAccess.tsx");
  assert.match(access, /const showSelection = showCredentialSelection\(access, oauthCount, keys\?\.length \?\? 0\)/);
  assert.match(access, /\{interactive && onUpdateProvider && \(/);
  assert.match(access, /\{editing && interactive && onUpdateProvider && \(/);
});
