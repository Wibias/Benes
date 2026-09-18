import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import { addProviderConnectionCapabilities, addProviderKeyConnectEnabled } from "../src/lib/add-provider-form-policy.ts";
import { addProviderPrimaryDisabled } from "../src/lib/add-provider-submit-policy.ts";
import {
  addProviderOAuthAttemptStale,
  createOAuthPopup,
  isOAuthLoginBusyError,
} from "../src/lib/add-provider-oauth-decode.ts";
import {
  applyAddProviderCommand,
  createAddProviderSession,
} from "../src/components/add-provider-session.ts";
import { addProviderChoosePresetForm } from "../src/lib/add-provider-submit-policy.ts";

test("API-key Connect stays off until the key method is selected and a key is present", () => {
  assert.equal(addProviderKeyConnectEnabled({
    authMode: "oauth",
    apiKey: "",
  }), false);
  assert.equal(addProviderKeyConnectEnabled({
    authMode: "oauth",
    apiKey: "sk-test",
  }), false);
  assert.equal(addProviderKeyConnectEnabled({
    authMode: "key",
    apiKey: "",
  }), false);
  assert.equal(addProviderKeyConnectEnabled({
    authMode: "key",
    apiKey: "   ",
  }), false);
  assert.equal(addProviderKeyConnectEnabled({
    authMode: "key",
    apiKey: "sk-test",
  }), true);
  assert.equal(addProviderKeyConnectEnabled({
    authMode: "key",
    apiKey: "sk-test",
    saving: true,
  }), false);
  assert.equal(addProviderKeyConnectEnabled({
    authMode: "key",
    apiKey: "",
    keyOptional: true,
  }), true);
});

test("a split family does not treat OAuth as an API-key lane on the same config id", () => {
  const split = addProviderConnectionCapabilities({
    auth: "oauth",
    oauthProvider: "command-code",
    oauthSupported: ["command-code"],
    keyOnOauth: false,
  });
  assert.equal(split.canOauth, true);
  assert.equal(split.canKey, false);
  const singleton = addProviderConnectionCapabilities({
    auth: "oauth",
    oauthProvider: "xai",
    oauthSupported: ["xai"],
  });
  assert.equal(singleton.canKey, true);
});

test("connection Continue stays off for a required API key until one is entered", () => {
  const base = {
    saving: false,
    oauthBusy: false,
    hasForm: true,
    hasVerify: false,
    oauthMode: false,
    oauthSupported: [],
    phase: "connection",
    authMode: "key",
  };
  assert.equal(addProviderPrimaryDisabled({ ...base, apiKey: "" }), true);
  assert.equal(addProviderPrimaryDisabled({ ...base, apiKey: "sk-test" }), false);
  assert.equal(addProviderPrimaryDisabled({
    ...base,
    oauthMode: true,
    authMode: "oauth",
    oauthProvider: "command-code",
    oauthSupported: ["command-code"],
    apiKey: "",
  }), true);
});

test("a leftover login-busy error is detected so Add Provider can cancel and retry", () => {
  assert.equal(isOAuthLoginBusyError("A login for command-code is already in progress"), true);
  assert.equal(isOAuthLoginBusyError("A login for xai is already in progress"), true);
  assert.equal(isOAuthLoginBusyError("unknown oauth provider"), false);
  assert.equal(isOAuthLoginBusyError(undefined), false);
});

test("a cancelled OAuth attempt is stale so the status poll does not finish the login", () => {
  assert.equal(addProviderOAuthAttemptStale(1, 1), false);
  assert.equal(addProviderOAuthAttemptStale(1, 2), true);
});

test("cancel-oauth leaves the Connection preset and clears the waiting-for-browser state", () => {
  const preset = {
    id: "command-code",
    label: "Command Code",
    adapter: "openai-chat",
    baseUrl: "https://example.test",
    auth: "oauth",
    oauthProvider: "command-code",
  };
  let state = createAddProviderSession(false, "Custom");
  state = applyAddProviderCommand(state, {
    op: "selectPreset",
    preset,
    form: addProviderChoosePresetForm(preset, preset.baseUrl),
    endpointChoice: "custom",
  });
  state = applyAddProviderCommand(state, { op: "startAuthorization", busy: true });
  state = applyAddProviderCommand(state, { op: "setAuthorizationNotice", notice: "Waiting for browser…" });
  state = applyAddProviderCommand(state, { op: "receiveAuthorizationUrl", url: "https://auth.example/login", providerId: "command-code" });
  state = applyAddProviderCommand(state, { op: "cancelAuthorization" });
  assert.equal(state.selection.preset?.id, "command-code");
  assert.equal(state.selection.form?.authMode, "oauth");
  assert.equal(state.oauth.busy, false);
  assert.equal(state.oauth.notice, "");
  assert.equal(state.oauth.authorizeUrl, "");
  assert.equal(state.oauth.authorizeFor, null);
});

test("Connection OAuth wait offers Cancel next to Waiting for browser", () => {
  const src = readFileSync(
    join(dirname(fileURLToPath(import.meta.url)), "../src/components/add-provider-setup-sections.tsx"),
    "utf8",
  );
  assert.match(src, /oauthBusy \? t\("modal\.waitingBrowser"\)/);
  assert.match(src, /oauthBusy && onCancelLogin/);
  assert.match(src, /t\("common\.cancel"\)/);
});

test("OAuth popup opens a blank window on click then navigates after the login URL arrives", () => {
  const calls = [];
  const fake = { closed: false, href: "about:blank" };
  const popup = createOAuthPopup((url, target, features) => {
    calls.push({ url, target, features });
    if (url === "about:blank") return { get location() { return fake; }, set location(next) { fake.href = next.href ?? next; }, closed: fake.closed };
    return null;
  });
  assert.equal(popup.opened, true);
  assert.equal(calls[0].url, "about:blank");
  popup.navigate("https://commandcode.ai/studio/auth/cli?state=1");
  assert.equal(fake.href, "https://commandcode.ai/studio/auth/cli?state=1");
});

test("OAuth popup closes the blank window when navigation has to open a new tab", () => {
  const fallback = [];
  let blankClosed = false;
  const location = {};
  Object.defineProperty(location, "href", {
    get() { return "about:blank"; },
    set() { throw new Error("blocked"); },
  });
  const popup = createOAuthPopup((url, target) => {
    if (url === "about:blank") {
      return {
        closed: false,
        location,
        close() { blankClosed = true; this.closed = true; },
      };
    }
    fallback.push({ url, target });
    return { closed: false };
  });
  popup.navigate("https://commandcode.ai/studio/auth/cli?state=1");
  assert.equal(blankClosed, true);
  assert.deepEqual(fallback, [{ url: "https://commandcode.ai/studio/auth/cli?state=1", target: "_blank" }]);
});

test("Verify prefers live models over the Command Code seed list", async () => {
  const { verifyDisplayedModels, verifyQuotaKind, verifyQuotaWindows, providerReportsQuota, formatCreditsUsd } = await import(
    "../src/lib/add-provider-verify-policy.ts"
  );
  const seed = [
    "deepseek/deepseek-v4-pro",
    "deepseek/deepseek-v4-flash",
    "zai-org/glm-5.3",
  ];
  assert.deepEqual(verifyDisplayedModels(undefined, seed), seed);
  assert.deepEqual(verifyDisplayedModels(undefined, seed, true), []);
  assert.equal(verifyDisplayedModels(["claude-sonnet-5", "xai/grok-4.6"], seed).length, 2);
  assert.equal(providerReportsQuota({ name: "command-code", authMode: "oauth" }), true);
  assert.equal(providerReportsQuota({ name: "custom", authMode: "key" }), false);
  const windows = verifyQuotaWindows({ fiveHourPercent: 25, weeklyPercent: 40 });
  assert.deepEqual(windows, [
    { labelKey: "quota.fiveHourLimit", remainingPercent: 75 },
    { labelKey: "quota.weeklyLimit", remainingPercent: 60 },
  ]);
  assert.equal(verifyQuotaKind({ testing: true, reportsQuota: true, windows: [] }), "loading");
  assert.equal(verifyQuotaKind({ testing: false, reportsQuota: true, windows }), "windows");
  assert.equal(verifyQuotaKind({ testing: false, reportsQuota: true, windows: [], hasCredits: true }), "windows");
  assert.equal(verifyQuotaKind({ testing: false, reportsQuota: true, windows: [] }), "empty");
  assert.equal(verifyQuotaKind({ testing: false, reportsQuota: false, windows: [] }), "none");
  assert.equal(formatCreditsUsd(12.4), "$12.40");
});

test("Verify loads live Command Code models and quota together", async () => {
  const { loadAddProviderVerifyLive } = await import("../src/lib/add-provider-verify-policy.ts");
  const live = await loadAddProviderVerifyLive("http://127.0.0.1:23100", "command-code", async (url) => {
    const href = String(url);
    if (href.endsWith("/api/model-discovery")) {
      return {
        ok: true,
        json: async () => ({
          ok: true,
          models: ["deepseek/deepseek-v4-pro", "claude-sonnet-5", "xai/grok-4.6"],
        }),
      };
    }
    return {
      ok: true,
      json: async () => ({
        reports: [{
          provider: "command-code",
          updatedAt: 1,
          quota: {
            fiveHourPercent: 10,
            weeklyPercent: 20,
            creditsUsd: { remaining: 4.5, used: 1, limit: 5.5, percent: 18 },
            updatedAt: 1,
          },
        }],
      }),
    };
  });
  assert.equal(live.models?.length, 3);
  assert.deepEqual(live.windows, [
    { labelKey: "quota.fiveHourLimit", remainingPercent: 90 },
    { labelKey: "quota.weeklyLimit", remainingPercent: 80 },
  ]);
  assert.equal(live.creditsRemaining, 4.5);
});

test("Verify live loader yields no models when discovery reports unknown_provider", async () => {
  const { loadAddProviderVerifyLive } = await import("../src/lib/add-provider-verify-policy.ts");
  const live = await loadAddProviderVerifyLive("http://127.0.0.1:23100", "command-code", async (url) => {
    const href = String(url);
    if (href.endsWith("/api/model-discovery")) {
      return {
        ok: false,
        status: 404,
        json: async () => ({ error: "unknown_provider", error_description: "unknown provider" }),
      };
    }
    return { ok: true, json: async () => ({ reports: [] }) };
  });
  assert.equal(live.models, undefined);
  assert.deepEqual(live.windows, []);
});

const oauthCatalogPostBodies = [
  { name: "anthropic", provider: { adapter: "anthropic", baseUrl: "https://api.anthropic.com", authMode: "oauth" } },
  { name: "command-code", provider: { adapter: "command-code", baseUrl: "https://api.commandcode.ai", authMode: "oauth" } },
  { name: "cursor", provider: { adapter: "cursor", baseUrl: "https://api2.cursor.sh", authMode: "oauth" } },
  { name: "kimi", provider: { adapter: "openai-chat", baseUrl: "https://api.kimi.com/coding/v1", authMode: "oauth" } },
  { name: "nous", provider: { adapter: "openai-chat", baseUrl: "https://inference-api.nousresearch.com/v1", authMode: "oauth" } },
  { name: "xai", provider: { adapter: "openai-chat", baseUrl: "https://api.x.ai/v1", authMode: "oauth" } },
];

function mockVerifyFetch(calls, { postStatus = 201, name = "anthropic", models = ["live-model-a", "live-model-b"] } = {}) {
  return async (url, init) => {
    const href = String(url);
    calls.push({ url: href, method: init?.method, body: init?.body });
    if (href.endsWith("/api/providers") && init?.method === "POST") {
      return {
        ok: postStatus >= 200 && postStatus < 300,
        status: postStatus,
        json: async () => (postStatus === 409 ? { error: "provider already exists" } : { ok: true, name }),
      };
    }
    if (href.endsWith("/api/model-discovery")) {
      return { ok: true, json: async () => ({ ok: true, models }) };
    }
    return {
      ok: true,
      json: async () => ({
        reports: [{ provider: name, updatedAt: 1, quota: { fiveHourPercent: 10, weeklyPercent: 20, updatedAt: 1 } }],
      }),
    };
  };
}

test("OAuth Verify posts then discovers for every catalog OAuth provider", async () => {
  const { ensureConfiguredThenLoadAddProviderVerifyLive } = await import("../src/lib/add-provider-verify-policy.ts");
  const helperSrc = readFileSync(
    join(dirname(fileURLToPath(import.meta.url)), "../src/lib/add-provider-verify-policy.ts"),
    "utf8",
  );
  assert.doesNotMatch(helperSrc, /command-code/);
  for (const postBody of oauthCatalogPostBodies) {
    const calls = [];
    const live = await ensureConfiguredThenLoadAddProviderVerifyLive(
      "http://127.0.0.1:23100",
      postBody,
      mockVerifyFetch(calls, { name: postBody.name }),
    );
    assert.equal(calls[0]?.url, "http://127.0.0.1:23100/api/providers");
    assert.equal(calls[0]?.method, "POST");
    const posted = JSON.parse(String(calls[0]?.body ?? "{}"));
    assert.equal(posted.name, postBody.name);
    assert.equal(posted.provider.authMode, "oauth");
    const discovery = calls.findIndex((row) => row.url.endsWith("/api/model-discovery"));
    assert.ok(discovery > 0, `${postBody.name}: discovery must run after POST /api/providers`);
    assert.equal(live.configured, true);
    assert.deepEqual(live.models, ["live-model-a", "live-model-b"]);
  }
});

test("OAuth Verify still discovers when the provider row already exists", async () => {
  const { ensureConfiguredThenLoadAddProviderVerifyLive } = await import("../src/lib/add-provider-verify-policy.ts");
  const postBody = oauthCatalogPostBodies.find((row) => row.name === "kimi");
  const calls = [];
  const live = await ensureConfiguredThenLoadAddProviderVerifyLive(
    "http://127.0.0.1:23100",
    postBody,
    mockVerifyFetch(calls, { postStatus: 409, name: postBody.name }),
  );
  assert.equal(calls[0]?.url, "http://127.0.0.1:23100/api/providers");
  assert.ok(calls.some((row) => row.url.endsWith("/api/model-discovery")));
  assert.equal(live.configured, true);
  assert.equal(live.models?.length, 2);
});

test("OAuth Verify does not discover when creating the provider fails", async () => {
  const { ensureConfiguredThenLoadAddProviderVerifyLive } = await import("../src/lib/add-provider-verify-policy.ts");
  const postBody = oauthCatalogPostBodies.find((row) => row.name === "xai");
  const calls = [];
  const live = await ensureConfiguredThenLoadAddProviderVerifyLive(
    "http://127.0.0.1:23100",
    postBody,
    mockVerifyFetch(calls, { postStatus: 500, name: postBody.name }),
  );
  assert.equal(calls.some((row) => row.url.endsWith("/api/model-discovery")), false);
  assert.equal(live.configured, false);
  assert.equal(live.models, undefined);
});

test("OAuth Verify finish posts then discovers instead of discovering a missing config row", () => {
  const src = readFileSync(
    join(dirname(fileURLToPath(import.meta.url)), "../src/components/AddProviderModal.tsx"),
    "utf8",
  );
  const oauthFinish = src.slice(src.indexOf("const finishOAuthIntoVerify"), src.indexOf("const { loginOAuth"));
  assert.match(oauthFinish, /ensureConfiguredThenLoadAddProviderVerifyLive/);
  assert.doesNotMatch(oauthFinish, /loadAddProviderVerifyLive\(/);
  assert.doesNotMatch(oauthFinish, /command-code/);
});
