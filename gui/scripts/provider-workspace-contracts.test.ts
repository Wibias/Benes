import assert from "node:assert/strict";
import test from "node:test";

import {
  applyAddProviderCommand,
  createAddProviderSession,
} from "../src/components/add-provider-session.ts";
import { addProviderChoosePresetForm } from "../src/lib/add-provider-submit-policy.ts";
import { buildActiveAccountNeedsReauthMap } from "../src/provider-workspace/account-attention.ts";
import {
  listAccountLoginTargets,
  mirrorForwardLoginStatus,
  planAccountLogin,
} from "../src/provider-workspace/provider-account-login.ts";
import {
  applyNamedProviderPatch,
  decodeProviderMutationCopy,
  deleteNamedProvider,
  setNamedProviderDefault,
  setNamedProviderDisabled,
} from "../src/provider-workspace/provider-mutations.ts";
import {
  applyActiveAccountReauth,
  binProviderStatus,
  buildProviderWorkspace,
  hideRedundantChatGptForwardProviders,
  isAccountProvider,
  isFreeProvider,
  providerBoardGroup,
  providerSupportsLiveModelDiscovery,
  providerTier,
  sortWorkspaceItems,
  type WorkspaceProvider,
} from "../src/provider-workspace/catalog.ts";
import { isLocalProvider, providerKind } from "../src/provider-workspace/catalog.ts";
import {
  maxQuotaUtilisation,
  projectQuotaSurface,
  quotaFillRatio,
  quotaIsSpent,
  soonestExhaustedResetAt,
} from "../src/provider-workspace/quota-presentation.ts";
import {
  countAvailableModels,
  decodeModelSelection,
} from "../src/provider-workspace/model-selection.ts";
import { formatElapsedSince } from "../src/relative-clock.ts";
import { formatAccessQuotaReset } from "../src/provider-workspace/quota-presentation.ts";
import {
  encodeProviderCreate,
  encodeProviderDraft,
  type ProviderPayloadForm,
} from "../src/provider-workspace/provider-create-request.ts";
import {
  codexAccountProviderNames,
  codexPresetDescriptionKey,
  isReservedCodexForwardPreset,
  openAiAccountProviderState,
} from "../src/provider-workspace/openai-forward-policy.ts";
import { OpenAiEnableError, ensureOpenAiProvider } from "../src/pages/provider-registry.ts";

const CODEX = "https://chatgpt.com/backend-api/codex";

function t(key: string, vars?: Record<string, string | number>): string {
  if (!vars) return key;
  return `${key}:${Object.entries(vars).map(([name, value]) => `${name}=${value}`).join(",")}`;
}

function form(overrides: Partial<ProviderPayloadForm> = {}): ProviderPayloadForm {
  return {
    name: "acme",
    adapter: "openai-chat",
    baseUrl: "https://api.example.test/v1",
    authMode: "key",
    apiKey: "sk-test",
    defaultModel: "gpt-test",
    ...overrides,
  };
}

function provider(overrides: Partial<WorkspaceProvider> & { name?: string } = {}) {
  const { name: _name, ...rest } = overrides;
  return {
    adapter: "openai-chat",
    baseUrl: "https://api.example.test/v1",
    ...rest,
  } satisfies WorkspaceProvider;
}

test("provider payload omits empty optionals and preserves adapter, URL, and auth", () => {
  assert.deepEqual(encodeProviderDraft(form({
    responsesPath: "  ",
    apiKey: "  sk-live  ",
    defaultModel: "  ",
    allowPrivateNetwork: false,
  })), {
    adapter: "openai-chat",
    baseUrl: "https://api.example.test/v1",
    authMode: "key",
    apiKey: "sk-live",
  });
});

test("provider payload keeps responses path, local-network flag, and anthropic bearer transport", () => {
  assert.deepEqual(encodeProviderDraft(form({
    adapter: "anthropic",
    authMode: "key",
    apiKeyTransport: "bearer",
    responsesPath: " /v1/messages ",
    allowPrivateNetwork: true,
    defaultModel: " claude ",
  })), {
    adapter: "anthropic",
    baseUrl: "https://api.example.test/v1",
    responsesPath: "/v1/messages",
    authMode: "key",
    apiKey: "sk-test",
    apiKeyTransport: "bearer",
    defaultModel: "claude",
    allowPrivateNetwork: true,
  });
});

test("provider payload never writes an empty secret and drops local authMode from the wire", () => {
  const local = encodeProviderDraft(form({ authMode: "local", apiKey: "   ", apiKeyTransport: "bearer" }));
  assert.equal(local.authMode, undefined);
  assert.equal(local.apiKey, undefined);
  assert.equal(local.apiKeyTransport, undefined);
  const oauth = encodeProviderDraft(form({ authMode: "oauth", apiKey: "sk-should-omit" }));
  assert.equal(oauth.authMode, "oauth");
  assert.equal(oauth.apiKey, undefined);
});

test("reserved Codex forward POST clones the preset seed and ignores the form name", () => {
  const preset = {
    id: "openai",
    provider: {
      adapter: "openai-responses",
      baseUrl: CODEX,
      authMode: "forward" as const,
      codexAccountMode: "pool" as const,
    },
  };
  const body = encodeProviderCreate(preset, form({ name: "chatgpt" }));
  assert.equal(body.name, "openai");
  assert.deepEqual(body.provider, preset.provider);
  assert.notEqual(body.provider, preset.provider);
  assert.equal(isReservedCodexForwardPreset(preset), true);
  assert.equal(codexPresetDescriptionKey(preset), "prov.openaiPoolDesc");
  assert.equal(codexPresetDescriptionKey({ ...preset, codexAccountMode: "direct" }), "prov.openaiDirectDesc");
});

test("OpenAI account state and Codex forward names stay on the reserved preset", () => {
  assert.equal(openAiAccountProviderState(undefined), "absent");
  assert.equal(openAiAccountProviderState({
    adapter: "openai-responses",
    authMode: "forward",
    baseUrl: `${CODEX}/`,
  }), "ready");
  assert.equal(openAiAccountProviderState({
    adapter: "openai-responses",
    authMode: "forward",
    baseUrl: CODEX,
    disabled: true,
  }), "disabled");
  assert.equal(openAiAccountProviderState({
    adapter: "openai-chat",
    authMode: "forward",
    baseUrl: CODEX,
  }), "invalid");
  assert.deepEqual(
    codexAccountProviderNames({
      openai: { authMode: "forward" },
      "openai-apikey": { authMode: "key" },
      work: { authMode: "forward" },
    }),
    ["openai", "work"],
  );
});

test("ensureOpenAiProvider enables a disabled row or posts the reserved seed", async () => {
  const calls: Array<{ url: string; method?: string; body?: string }> = [];
  const fetchImpl: typeof fetch = async (input, init) => {
    const url = String(input);
    calls.push({ url, method: init?.method, body: typeof init?.body === "string" ? init.body : undefined });
    if (url.endsWith("/api/providers?name=openai")) return new Response(null, { status: 200 });
    if (url.endsWith("/api/provider-presets")) {
      return Response.json({
        providers: [{ id: "openai", provider: { adapter: "openai-responses", baseUrl: CODEX, authMode: "forward" } }],
      });
    }
    if (url.endsWith("/api/providers") && init?.method === "POST") return new Response(null, { status: 200 });
    return new Response("no", { status: 500 });
  };
  await ensureOpenAiProvider("http://127.0.0.1:23100", "disabled", fetchImpl);
  await ensureOpenAiProvider("http://127.0.0.1:23100", "absent", fetchImpl);
  assert.equal(calls[0]?.method, "PATCH");
  assert.equal(calls[0]?.body, JSON.stringify({ disabled: false }));
  assert.equal(calls[1]?.url.endsWith("/api/provider-presets"), true);
  assert.equal(calls[2]?.method, "POST");
  assert.equal(
    calls[2]?.body,
    JSON.stringify({
      name: "openai",
      provider: { adapter: "openai-responses", baseUrl: CODEX, authMode: "forward" },
    }),
  );
});

test("ensureOpenAiProvider maps failures onto stable i18n keys", async () => {
  await assert.rejects(
    () => ensureOpenAiProvider("http://x", "disabled", async () => new Response("no", { status: 500 })),
    (error: unknown) => error instanceof OpenAiEnableError && error.i18nKey === "codexAuth.enableOpenaiFailed",
  );
  await assert.rejects(
    () => ensureOpenAiProvider("http://x", "absent", async () => new Response("no", { status: 500 })),
    (error: unknown) => error instanceof OpenAiEnableError && error.i18nKey === "codexAuth.openaiPresetLoadFailed",
  );
});

test("catalog bins disabled before ready, and treats oauth/forward/local/keyOptional/loopback as ready", () => {
  const sections = buildProviderWorkspace({
    off: provider({ disabled: true, hasApiKey: true }),
    oauth: provider({ authMode: "oauth" }),
    fwd: provider({ authMode: "forward" }),
    local: provider({ authMode: "local" }),
    loop: provider({ baseUrl: "http://127.0.0.1:11434" }),
    keyed: provider({ hasApiKey: true }),
    empty: provider({ hasApiKey: false, authMode: "key" }),
  });
  assert.deepEqual(sections.disabled.map(item => item.name), ["off"]);
  assert.deepEqual(sections.ready.map(item => item.name).sort(), ["fwd", "keyed", "local", "loop", "oauth"]);
  assert.deepEqual(sections.needsSetup.map(item => item.name), ["empty"]);
});

test("account/free/paid tiers and live-discovery exceptions stay on current identifiers", () => {
  const openai = provider({ adapter: "openai-responses", authMode: "forward", baseUrl: CODEX });
  assert.equal(isAccountProvider("openai", openai), true);
  assert.equal(isAccountProvider("chatgpt", openai), false);
  assert.equal(providerTier("openai", openai), "accounts");
  assert.equal(isFreeProvider(provider({ freeTier: true, hasApiKey: true })), true);
  assert.equal(isFreeProvider(provider({ keyOptional: true })), true);
  assert.equal(providerTier("groq", provider({ hasApiKey: true })), "paid");
  assert.equal(providerSupportsLiveModelDiscovery("openai", openai), true);
  assert.equal(providerSupportsLiveModelDiscovery("cline-pass", {
    adapter: "openai-chat",
    baseUrl: "https://api.cline.bot/api/v1",
  }), false);
});

test("workspace lifecycle wins over config-only status bins", () => {
  assert.equal(binProviderStatus(provider({ disabled: true })), "disabled");
  assert.equal(binProviderStatus({ ...provider({ hasApiKey: true }), name: "x", workspaceLifecycle: "attention" }), "needs-setup");
  assert.equal(binProviderStatus({ ...provider(), name: "x", workspaceLifecycle: "healthy" }), "ready");
  assert.equal(providerBoardGroup({ ...provider({ disabled: true }), name: "x" }), "disabled");
  const tagged = applyActiveAccountReauth(
    { ready: [{ name: "anthropic", ...provider({ authMode: "oauth" }) }], needsSetup: [], disabled: [] },
    { anthropic: true },
  );
  assert.equal(tagged.ready[0]?.activeNeedsReauth, true);
});

test("sort and ChatGPT/openai-apikey folding preserve current product rules", () => {
  const items = [
    { name: "zeta", ...provider(), tier: "paid" as const },
    { name: "alpha", ...provider({ keyOptional: true }), tier: "free" as const },
    { name: "openai", adapter: "openai-responses", baseUrl: CODEX, authMode: "forward" as const, tier: "accounts" as const },
  ];
  assert.deepEqual(sortWorkspaceItems(items, "az").map(item => item.name), ["alpha", "openai", "zeta"]);
  assert.deepEqual(sortWorkspaceItems(items, "accounts-first").map(item => item.name), ["openai", "alpha", "zeta"]);
  const folded = hideRedundantChatGptForwardProviders({
    openai: provider({ adapter: "openai-responses", authMode: "forward", baseUrl: CODEX }),
    chatgpt: provider({ adapter: "openai-responses", authMode: "forward", baseUrl: CODEX }),
    "openai-apikey": provider({ authMode: "key", hasApiKey: true }),
  });
  assert.equal("chatgpt" in folded, false);
  assert.equal("openai-apikey" in folded, false);
  assert.equal("openai" in folded, true);
});

test("kind policy splits login, local, self-hosted, and cloud", () => {
  assert.equal(providerKind({ ...provider({ authMode: "oauth" }), name: "xai" }), "login");
  assert.equal(providerKind({ ...provider({ authMode: "forward" }), name: "openai" }), "login");
  assert.equal(isLocalProvider(provider({ baseUrl: "http://localhost:1234" })), true);
  assert.equal(providerKind({ ...provider({ adapter: "ollama", baseUrl: "https://gpu.internal" }), name: "home-ollama" }), "selfHosted");
  assert.equal(providerKind({ ...provider({ hasApiKey: true }), name: "groq" }), "cloud");
});

test("selected-models parsing keeps missing distinct from empty and drops malformed ids", () => {
  const raw = {
    available: { openai: ["a", 1, "b"], bad: "nope" },
    selected: { openai: ["a"], empty: [] },
    liveModelCounts: { openai: 4.8, skip: -1, none: Number.NaN },
  };
  const snapshot = decodeModelSelection(raw);
  assert.deepEqual(snapshot.available, { openai: ["a", "b"] });
  assert.deepEqual(snapshot.selected, { openai: ["a"], empty: [] });
  assert.deepEqual(snapshot.liveCounts, { openai: 4 });
  assert.deepEqual(countAvailableModels(raw), { openai: 2 });
  assert.deepEqual(decodeModelSelection(null), { available: {}, selected: {}, liveCounts: {} });
  assert.deepEqual(decodeModelSelection({ liveModelCounts: { openai: 0 } }).liveCounts, { openai: 0 });
});

test("relative time and access reset copy do not invent zeros for missing timestamps", () => {
  const now = Date.parse("2026-09-10T12:00:00Z");
  assert.equal(formatElapsedSince(undefined, undefined, now), "Not checked");
  assert.equal(formatElapsedSince(now - 30_000, undefined, now), "Just now");
  assert.equal(formatElapsedSince(now - 5 * 60_000, undefined, now), "5 min ago");
  assert.equal(formatAccessQuotaReset(undefined, t, now), "");
  assert.equal(formatAccessQuotaReset(now - 1000, t, now), "");
  assert.equal(formatAccessQuotaReset(now / 1000 + 90, t, now), "quota.resetsRelativeMinutes:n=2");
});

test("quota meters rank windows on raw identities and clamp utilisation without inventing zero", () => {
  const surface = projectQuotaSurface({
    quota: {
      fiveHourPercent: 10,
      weeklyPercent: 40,
      monthlyPercent: 70,
      customWindows: [
        { label: "API usage", percent: 12 },
        { label: "First-party models", percent: 8 },
      ],
      updatedAt: 1,
    },
    threshold: 80,
    t,
    locale: "en",
  });
  assert.equal(surface.mode, "live");
  if (surface.mode !== "live") throw new Error("expected live meters");
  assert.deepEqual(surface.meters.map(meter => meter.caption), [
    "codexAuth.fiveHour",
    "codexAuth.weekly",
    "quota.cursorFirstParty",
    "quota.cursorApiUsage",
    "codexAuth.monthly",
  ]);
  assert.equal(maxQuotaUtilisation(null), -1);
  assert.equal(maxQuotaUtilisation({ weeklyPercent: 12, updatedAt: 1 }), 12);
  assert.equal(quotaIsSpent(99.49), false);
  assert.equal(quotaIsSpent(99.5), true);
  assert.equal(quotaFillRatio(0), 0);
  assert.equal(quotaFillRatio(1), 0.04);
  assert.equal(quotaFillRatio(50), 0.5);
  assert.equal(surface.meters[0]?.tone, "ok");
  const warnSurface = projectQuotaSurface({
    quota: { weeklyPercent: 80, updatedAt: 1 },
    threshold: 80,
    t,
    locale: "en",
  });
  assert.equal(warnSurface.mode === "live" && warnSurface.meters[0]?.tone, "warn");
});

test("soonest exhausted reset ignores unknown and already-elapsed windows", () => {
  const now = Date.parse("2026-09-10T12:00:00Z");
  assert.equal(soonestExhaustedResetAt(null, now), undefined);
  assert.equal(soonestExhaustedResetAt({
    fiveHourPercent: 100,
    fiveHourResetAt: now / 1000 - 10,
    weeklyPercent: 100,
    weeklyResetAt: now / 1000 + 60,
    monthlyPercent: 10,
    monthlyResetAt: now / 1000 + 10,
    updatedAt: 1,
  }, now), now / 1000 + 60);
});

test("provider mutation copy keeps last-provider, combo, and default-disabled codes", () => {
  assert.equal(decodeProviderMutationCopy({ code: "last_provider" }, t, "fallback"), "prov.removeLastProvider");
  assert.equal(
    decodeProviderMutationCopy({ code: "provider_has_dependent_combos", combos: ["a", "b"] }, t, "fallback"),
    "prov.removeHasDependentCombos:combos=a, b",
  );
  assert.equal(decodeProviderMutationCopy({ code: "default_provider_disabled" }, t, "fallback"), "prov.defaultDisabled");
  assert.equal(decodeProviderMutationCopy({ error: "  boom  " }, t, "fallback"), "boom");
  assert.equal(decodeProviderMutationCopy({}, t, "fallback"), "fallback");
});

test("delete/enable/default/update talk to /api/providers and stay truthful on failure", async () => {
  const calls: Array<{ url: string; method?: string; body?: string }> = [];
  const fetchImpl: typeof fetch = async (input, init) => {
    const url = String(input);
    calls.push({ url, method: init?.method, body: typeof init?.body === "string" ? init.body : undefined });
    if (init?.method === "DELETE") return Response.json({ defaultProvider: "openai" });
    if (url.includes("name=broken")) return Response.json({ error: "  nope  " }, { status: 400 });
    return new Response(null, { status: 200 });
  };
  const deleted = await deleteNamedProvider({ apiBase: "http://x", name: "acme", t, fetchImpl });
  assert.deepEqual(deleted, { ok: true, defaultProvider: "openai" });
  const enabled = await setNamedProviderDisabled({ apiBase: "http://x", name: "acme", disabled: false, t, fetchImpl });
  assert.equal(enabled.ok, true);
  const def = await setNamedProviderDefault({ apiBase: "http://x", name: "acme", t, fetchImpl });
  assert.equal(def.ok, true);
  const patched = await applyNamedProviderPatch({
    apiBase: "http://x",
    name: "acme",
    patch: { note: "keep" },
    t,
    fetchImpl,
  });
  assert.deepEqual(patched, { ok: true });
  const failed = await applyNamedProviderPatch({
    apiBase: "http://x",
    name: "broken",
    patch: { disabled: true },
    t,
    fetchImpl,
  });
  assert.deepEqual(failed, { ok: false, error: "nope" });
  assert.equal(calls[0]?.method, "DELETE");
  assert.equal(calls[1]?.body, JSON.stringify({ disabled: false }));
  assert.equal(calls[2]?.body, JSON.stringify({ setDefault: true }));
  assert.equal(calls[3]?.body, JSON.stringify({ note: "keep" }));
});

test("account reauth map uses the active credential and can overlay Codex", () => {
  const needs = (account: { needsReauth?: boolean } | null | undefined) => Boolean(account?.needsReauth);
  assert.deepEqual(buildActiveAccountNeedsReauthMap({
    anthropic: {
      activeAccountId: "a2",
      accounts: [
        { id: "a1", active: false, needsReauth: true },
        { id: "a2", active: true, needsReauth: false },
      ],
    },
  }, false, needs), {});
  assert.deepEqual(buildActiveAccountNeedsReauthMap({
    anthropic: {
      activeAccountId: "a1",
      accounts: [{ id: "a1", active: true, needsReauth: true }],
    },
  }, true, needs), { anthropic: true, openai: true });
});

test("add-provider reducer keeps a late OAuth URL off a switched-away preset", () => {
  const preset = {
    id: "xai",
    label: "xAI",
    adapter: "openai-chat",
    baseUrl: "https://api.x.ai/v1",
    auth: "oauth" as const,
    oauthProvider: "xai",
  };
  let state = createAddProviderSession(false, "Custom");
  state = applyAddProviderCommand(state, {
    op: "selectPreset",
    preset,
    form: addProviderChoosePresetForm(preset, preset.baseUrl),
    endpointChoice: "custom",
  });
  state = applyAddProviderCommand(state, { op: "receiveAuthorizationUrl", url: "https://auth.example/xai", providerId: "other" });
  assert.equal(state.oauth.authorizeUrl, "");
  state = applyAddProviderCommand(state, { op: "receiveAuthorizationUrl", url: "https://auth.example/xai", providerId: "xai" });
  assert.equal(state.oauth.authorizeUrl, "https://auth.example/xai");
  state = applyAddProviderCommand(state, { op: "switchToApiKey", form: form({ authMode: "key" }) });
  assert.equal(state.oauth.busy, false);
  assert.equal(state.oauth.authorizeFor, null);
});

test("add-modal account rows put Codex names first and copy forward login status", () => {
  const config = {
    port: 23100,
    defaultProvider: "openai",
    providers: {
      openai: { adapter: "openai-responses", baseUrl: CODEX, authMode: "forward" },
      work: { adapter: "openai-responses", baseUrl: CODEX, authMode: "forward" },
    },
  };
  const rows = listAccountLoginTargets(config, ["xai"]);
  assert.deepEqual(rows.map(row => row.providerId), ["openai", "work", "xai"]);
  assert.equal(rows[0]?.channel, "codex");
  assert.equal(rows[2]?.channel, "oauth");
  const status = mirrorForwardLoginStatus(config.providers, { openai: { loggedIn: true, email: "a@b.c" } });
  assert.equal(status.work?.loggedIn, true);
  assert.equal(status.work?.email, "a@b.c");
  assert.deepEqual(planAccountLogin(config, "openai", []), { kind: "codex-login" });
  assert.deepEqual(planAccountLogin({
    ...config,
    providers: { ...config.providers, openai: { adapter: "openai-chat", baseUrl: CODEX, authMode: "forward" } },
  }, "openai", []), { kind: "invalid-openai" });
  assert.deepEqual(planAccountLogin({
    port: 23100,
    defaultProvider: "xai",
    providers: { xai: { adapter: "openai-chat", baseUrl: "https://api.x.ai/v1", authMode: "oauth" } },
  }, "xai", ["xai"]), { kind: "oauth" });
});

test("quota pending is a skeleton only when there are no rows", () => {
  const quota = { fiveHourPercent: 10, updatedAt: 1 };
  const withData = projectQuotaSurface({ quota, threshold: 80, t, locale: "en", pending: true });
  assert.equal(withData.mode, "live");
  if (withData.mode !== "live") throw new Error("expected live meters");
  assert.equal(withData.meters.length > 0, true);

  const emptyPending = projectQuotaSurface({ quota: null, threshold: 80, t, locale: "en", pending: true });
  assert.equal(emptyPending.mode, "wait");

  const emptyIdle = projectQuotaSurface({ quota: null, threshold: 80, t, locale: "en", pending: false });
  assert.equal(emptyIdle.mode, "none");
});

test("awaited provider registry reload does not resolve until published config arrives", async () => {
  const { clearClientResourceStoresForTests, readClientResourceSnapshotForTests, setClientResourceData } = await import("../src/client-resource.ts");
  const { reloadPublishedProvidersConfig } = await import("../src/pages/provider-registry.ts");
  clearClientResourceStoresForTests();
  const key = "benes.providers.config.v1:http://registry.test";
  const stale = { port: 1, defaultProvider: "old", providers: {} };
  const fresh = { port: 2, defaultProvider: "new", providers: { openai: { adapter: "openai-responses" } } };
  setClientResourceData(key, stale);
  let release!: () => void;
  const hold = new Promise<void>(resolve => { release = resolve; });
  const fetchImpl: typeof fetch = async (input) => {
    if (!String(input).endsWith("/api/config")) throw new Error(String(input));
    await hold;
    return Response.json(fresh);
  };
  const pending = reloadPublishedProvidersConfig("http://registry.test", key, fetchImpl);
  assert.equal(readClientResourceSnapshotForTests(key).data?.defaultProvider, "old");
  release();
  await pending;
  assert.equal(readClientResourceSnapshotForTests(key).data?.defaultProvider, "new");
  clearClientResourceStoresForTests();
});

test("awaited OAuth registry reload publishes status before resolving", async () => {
  const { clearClientResourceStoresForTests, readClientResourceSnapshotForTests, setClientResourceData } = await import("../src/client-resource.ts");
  const { reloadPublishedOauthStatus } = await import("../src/pages/provider-registry.ts");
  clearClientResourceStoresForTests();
  const key = "providers-oauth:http://registry.test";
  setClientResourceData(key, { providers: [], status: {} });
  let release!: () => void;
  const hold = new Promise<void>(resolve => { release = resolve; });
  const fetchImpl: typeof fetch = async (input) => {
    const url = String(input);
    if (url.endsWith("/api/oauth/providers")) {
      await hold;
      return Response.json({ providers: ["xai"] });
    }
    if (url.includes("/api/oauth/status")) return Response.json({ loggedIn: true, email: "a@b.c" });
    throw new Error(url);
  };
  const pending = reloadPublishedOauthStatus("http://registry.test", key, fetchImpl);
  assert.deepEqual(readClientResourceSnapshotForTests(key).data?.providers, []);
  release();
  await pending;
  assert.deepEqual(readClientResourceSnapshotForTests(key).data?.providers, ["xai"]);
  assert.equal(readClientResourceSnapshotForTests(key).data?.status.xai?.loggedIn, true);
  clearClientResourceStoresForTests();
});

test("explicit empty enriched accounts replace the cheap snapshot", async () => {
  const { applyAccountQuotaEnrichment } = await import("../src/provider-workspace/provider-credential-api.ts");
  const cheap = {
    activeAccountId: "a1",
    accounts: [{ id: "a1", active: true, email: "keep@example.test" }],
  };
  assert.deepEqual(
    applyAccountQuotaEnrichment(cheap, { accounts: [] }),
    { activeAccountId: "a1", accounts: [] },
  );
  assert.deepEqual(
    applyAccountQuotaEnrichment(cheap, {}),
    cheap,
  );
  assert.deepEqual(
    applyAccountQuotaEnrichment(cheap, {
      activeAccountId: "a2",
      accounts: [{ id: "a2", active: true, email: "next@example.test" }],
    }),
    {
      activeAccountId: "a2",
      accounts: [{ id: "a2", active: true, email: "next@example.test" }],
    },
  );
});
