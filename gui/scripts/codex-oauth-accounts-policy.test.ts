import assert from "node:assert/strict";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import test from "node:test";

import {
  diagnosticsFromOxlintJson,
  summarizeStructuralDiagnostics,
} from "./check-structural-baseline.ts";
import {
  addProviderAuthMethodKey,
  addProviderChoosePresetForm,
  addProviderHidePrimary,
  addProviderInitialSelection,
  addProviderPostErrorMessage,
  addProviderPrimaryAction,
  addProviderPrimaryDisabled,
  addProviderProviderChoiceReady,
  addProviderVerifyAllowsBack,
  addProviderVerifyPrimaryEnabled,
  addProviderWizardPane,
  addProviderWizardRetreat,
  addProviderWizardStep,
  decodeProviderTestOutcome,
  firstFeaturedCatalogPreset,
  providerTestMessage,
  validateAddProviderSubmit,
} from "../src/lib/add-provider-submit-policy.ts";
import {
  accountSetupActionKind,
  accountSetupCodexLoginKind,
  accountSetupStatusCopy,
  addProviderAccessKind,
  addProviderAuthPanel,
  addProviderBaseUrlPlaceholderHintVisible,
  addProviderConnectionCapabilities,
  addProviderEndpointLabelKey,
  addProviderSetupGuideVisible,
  addProviderShowsEndpointChoices,
  addProviderShowsPrivateNetworkHint,
} from "../src/lib/add-provider-form-policy.ts";
import {
  accountPickerCopyKind,
  accountPickerInitialLoadFailed,
  accountPickerSaveFeedbackKind,
  accountPickerShowsCompatibility,
  accountPickerShowsRefreshFailed,
  decodeAccountPickerEnabled,
  decodeAccountPickerSave,
} from "../src/lib/codex-account-picker-policy.ts";
import {
  autoSwitchBlurCommit,
  autoSwitchDescriptionKey,
  autoSwitchFeedbackView,
  autoSwitchKeyCommand,
} from "../src/lib/codex-auto-switch-policy.ts";
import {
  decodeResetCreditsPayload,
  poolAccountAddedFeedbackKind,
  poolAccountDisplayKind,
  poolActiveNonMainAccount,
  poolMutationBusy,
  poolPauseExhaustedToastKind,
  poolPauseToastKey,
  poolPriorityUnchanged,
  poolRemoveToastKind,
  poolSwitchTargetId,
  poolSwitchToastKind,
  redeemResultClosesModal,
  sortResetCredits,
} from "../src/lib/codex-account-pool-action-policy.ts";
import {
  codexAccountHealthFlags,
  codexAccountQuotaMode,
  codexAccountShowsSwitch,
  mainAccountSessionBadge,
  mainCardDotClass,
  mainCardSwitchEntry,
  mainQuotaPending,
  poolCardDotClass,
  poolCardIsNext,
  poolCardOrderDisabled,
  poolCardShowsNeedsReauthBadge,
  poolCardShowsNextBadge,
  poolCardShowsPinned,
  poolQuotaPending,
} from "../src/lib/codex-account-card-policy.ts";
import {
  codexResetModalView,
  consumeResetCreditBody,
  isRedeemRequestId,
  resetConfirmCredit,
  resetCreditCount,
  resetCreditsAvailable,
} from "../src/lib/codex-account-reset-policy.ts";
import {
  codexAuthLoginAccountId,
  codexAuthLoginRequestBody,
  codexAuthLoginStatusUrl,
  codexAuthManualCodeBlocked,
  decodeCodexAuthConflictStep,
  decodeCodexAuthLoginOpened,
  decodeCodexAuthManualCodeFailure,
  decodeCodexAuthPollCatch,
  decodeCodexAuthPollTick,
  decodeCodexAuthStartCatch,
  decodeCodexAuthTimeoutApplies,
} from "../src/lib/codex-account-oauth-decode.ts";
import {
  decodeAddProviderManualCodeFailure,
  decodeAddProviderOAuthPollTick,
  decodeAddProviderOAuthStart,
} from "../src/lib/add-provider-oauth-decode.ts";
import {
  providersOAuthAccountFetchList,
  providersOAuthCompletedTarget,
  providersOAuthErrorIsCancelled,
  providersOAuthIdentityOutcome,
  providersOAuthLoginBody,
  providersOAuthLoginInfo,
  providersOAuthPollProgress,
  providersOAuthReauthTargetId,
  providersOAuthSameIdentityAdd,
  providersOAuthSeededAccountSet,
  providersOAuthStartFailedMessage,
} from "../src/lib/providers-oauth-policy.ts";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const guiRoot = path.resolve(scriptDir, "..");
const oxlintBin = path.join(guiRoot, "node_modules", "oxlint", "bin", "oxlint");
const configPath = path.join(guiRoot, ".oxlintrc.json");
const targetFiles = [
  path.join(guiRoot, "src", "lib", "add-provider-submit-policy.ts"),
  path.join(guiRoot, "src", "lib", "add-provider-form-policy.ts"),
  path.join(guiRoot, "src", "lib", "codex-account-picker-policy.ts"),
  path.join(guiRoot, "src", "lib", "codex-auto-switch-policy.ts"),
  path.join(guiRoot, "src", "lib", "codex-account-pool-action-policy.ts"),
  path.join(guiRoot, "src", "lib", "codex-account-card-policy.ts"),
  path.join(guiRoot, "src", "lib", "codex-account-reset-policy.ts"),
  path.join(guiRoot, "src", "lib", "codex-account-oauth-decode.ts"),
  path.join(guiRoot, "src", "lib", "add-provider-oauth-decode.ts"),
  path.join(guiRoot, "src", "lib", "providers-oauth-policy.ts"),
  path.join(guiRoot, "src", "components", "AddProviderModal.tsx"),
  path.join(guiRoot, "src", "components", "add-provider-modal-chrome.tsx"),
  path.join(guiRoot, "src", "components", "add-provider-modal-body.tsx"),
  path.join(guiRoot, "src", "components", "add-provider-form-pane.tsx"),
  path.join(guiRoot, "src", "components", "add-provider-form-sections.tsx"),
  path.join(guiRoot, "src", "components", "add-provider-setup-pane.tsx"),
  path.join(guiRoot, "src", "components", "add-provider-setup-sections.tsx"),
  path.join(guiRoot, "src", "components", "add-provider-account-setup.tsx"),
  path.join(guiRoot, "src", "components", "CodexAccountPickerSetting.tsx"),
  path.join(guiRoot, "src", "components", "codex-account-picker-sections.tsx"),
  path.join(guiRoot, "src", "components", "CodexAccountPool.tsx"),
  path.join(guiRoot, "src", "components", "codex-account-pool-surfaces.tsx"),
  path.join(guiRoot, "src", "components", "CodexAutoSwitchSetting.tsx"),
  path.join(guiRoot, "src", "components", "codex-auto-switch-sections.tsx"),
  path.join(guiRoot, "src", "components", "codex-account-pool-mutations.ts"),
  path.join(guiRoot, "src", "components", "codex-account-reset-modal.tsx"),
  path.join(guiRoot, "src", "components", "codex-account-reset-views.tsx"),
  path.join(guiRoot, "src", "components", "use-add-codex-account-oauth.ts"),
  path.join(guiRoot, "src", "components", "use-add-provider-oauth.ts"),
  path.join(guiRoot, "src", "components", "provider-workspace", "OAuthAccountsTable.tsx"),
  path.join(guiRoot, "src", "pages", "use-providers-oauth.ts"),
];

function structuralDiagnostics() {
  const result = spawnSync(
    process.execPath,
    [oxlintBin, "-c", configPath, "--format=json", ...targetFiles],
    { cwd: guiRoot, encoding: "utf8" },
  );
  if (result.error) throw result.error;
  const stdout = String(result.stdout ?? "").trim();
  assert.ok(stdout, `Oxlint Codex OAuth scan produced no output:\n${String(result.stderr ?? "")}`);
  return summarizeStructuralDiagnostics(diagnosticsFromOxlintJson(JSON.parse(stdout), guiRoot));
}

test("codex oauth / add-provider cluster contains no structural debt", () => {
  assert.deepEqual(structuralDiagnostics(), []);
});

test("add-provider submit validation preserves name then URL then placeholder precedence", () => {
  assert.deepEqual(
    validateAddProviderSubmit({ reserved: false, name: "  ", resolvedBaseUrl: "" }),
    { ok: false, errorKey: "modal.nameRequired" },
  );
  assert.deepEqual(
    validateAddProviderSubmit({ reserved: false, name: "acme", resolvedBaseUrl: "" }),
    { ok: false, errorKey: "modal.baseUrlRequired" },
  );
  assert.deepEqual(
    validateAddProviderSubmit({ reserved: false, name: "acme", resolvedBaseUrl: "https://x/{region}" }),
    { ok: false, errorKey: "modal.baseUrlPlaceholderError" },
  );
  assert.deepEqual(
    validateAddProviderSubmit({ reserved: true, name: "", resolvedBaseUrl: "" }),
    { ok: true },
  );
});

test("add-provider post and test decode preserve empty-string fallbacks", () => {
  assert.equal(addProviderPostErrorMessage({ error: "nope" }, 500, () => "status"), "nope");
  assert.equal(addProviderPostErrorMessage({ error: "" }, 418, ({ status }) => `failed ${status}`), "failed 418");
  assert.deepEqual(
    decodeProviderTestOutcome(true, { ok: false, message: "", error: "boom" }),
    { ok: false, messageKind: "raw", rawMessage: "boom" },
  );
  assert.deepEqual(
    decodeProviderTestOutcome(false, { applicable: false }),
    { ok: false, messageKind: "not-applicable" },
  );
  assert.equal(
    providerTestMessage({ messageKind: "ok" }, key => key),
    "pws.connectionOk",
  );
});

test("add-provider wizard is an explicit three-step flow", () => {
  assert.equal(addProviderWizardStep({ phase: "provider", hasVerify: false }), "provider");
  assert.equal(addProviderWizardStep({ phase: "connection", hasVerify: false }), "connection");
  assert.equal(addProviderWizardStep({ phase: "connection", hasVerify: true }), "verify");
  assert.equal(addProviderWizardRetreat("verify"), "connection");
  assert.equal(addProviderWizardRetreat("connection"), "provider");
  assert.equal(addProviderWizardPane("provider"), "catalog");
  assert.equal(addProviderWizardPane("connection"), "setup");
  assert.equal(addProviderHidePrimary("connection", true), true);
  assert.equal(addProviderHidePrimary("connection", false), false);
  assert.equal(addProviderAuthMethodKey("oauth"), "modal.badge.oauth");
  assert.equal(addProviderAuthMethodKey("key"), "modal.badge.apiKey");
  assert.equal(addProviderProviderChoiceReady(true, false), true);
  assert.equal(addProviderProviderChoiceReady(false, true), true);
  assert.equal(addProviderProviderChoiceReady(false, false), false);
  assert.equal(addProviderVerifyAllowsBack({ testing: true }), false);
  assert.equal(addProviderVerifyAllowsBack({ testing: false, ok: true }), true);
  assert.equal(addProviderVerifyAllowsBack({ testing: false, ok: false }), true);
  assert.equal(addProviderVerifyPrimaryEnabled({ testing: false, ok: true }), true);
  assert.equal(addProviderVerifyPrimaryEnabled({ testing: false, ok: false }), false);
  assert.equal(addProviderPrimaryAction({
    hasVerify: false, oauthMode: true, oauthProvider: "x", phase: "provider",
  }), "advance-provider");
  assert.equal(addProviderPrimaryAction({
    hasVerify: true, oauthMode: true, oauthProvider: "x", phase: "verify",
  }), "finish-verify");
  assert.equal(addProviderPrimaryAction({
    hasVerify: false, oauthMode: true, oauthProvider: "x", phase: "connection",
  }), "oauth-login");
  assert.equal(addProviderPrimaryAction({
    hasVerify: false, oauthMode: true, phase: "connection",
  }), "submit");
  assert.equal(addProviderPrimaryDisabled({
    saving: false,
    oauthBusy: false,
    hasForm: false,
    hasVerify: false,
    oauthMode: false,
    oauthSupported: [],
    phase: "provider",
  }), true);
  assert.equal(addProviderPrimaryDisabled({
    saving: false,
    oauthBusy: false,
    hasForm: true,
    hasVerify: false,
    oauthMode: false,
    oauthSupported: [],
    phase: "provider",
  }), false);
  assert.equal(addProviderPrimaryDisabled({
    saving: false,
    oauthBusy: false,
    hasForm: true,
    hasVerify: false,
    oauthMode: true,
    oauthProvider: "anthropic",
    oauthSupported: ["openai"],
    phase: "connection",
  }), true);
  assert.equal(addProviderPrimaryDisabled({
    saving: false,
    oauthBusy: false,
    hasForm: true,
    hasVerify: false,
    oauthMode: true,
    oauthProvider: "anthropic",
    oauthSupported: ["anthropic"],
    phase: "connection",
  }), true);
  assert.equal(addProviderPrimaryDisabled({
    saving: false,
    oauthBusy: false,
    hasForm: true,
    hasVerify: true,
    verifyOk: false,
    oauthMode: false,
    oauthSupported: [],
    phase: "verify",
  }), true);
  assert.equal(addProviderPrimaryDisabled({
    saving: false,
    oauthBusy: false,
    hasForm: true,
    hasVerify: true,
    verifyOk: true,
    oauthMode: false,
    oauthSupported: [],
    phase: "verify",
  }), false);
});

test("add-provider initial selection does not fall through from accounts to featured", () => {
  assert.equal(addProviderInitialSelection({
    hasPreset: false,
    hasSelectedAccount: false,
    initialCustom: false,
    presetsLoading: false,
    initialTier: "accounts",
  }), "first-account");
  assert.equal(addProviderInitialSelection({
    hasPreset: false,
    hasSelectedAccount: false,
    initialCustom: false,
    presetsLoading: false,
  }), "first-preset");
  assert.equal(
    firstFeaturedCatalogPreset([{ id: "custom" }, { id: "deepseek" }, { id: "anthropic" }], ["anthropic", "openai-apikey"]).id,
    "anthropic",
  );
  const form = addProviderChoosePresetForm({
    id: "custom",
    adapter: "openai-chat",
    baseUrl: "",
    auth: "key",
  }, "https://example.invalid");
  assert.equal(form.name, "");
  assert.equal(form.authMode, "key");
  assert.equal(form.allowPrivateNetwork, false);
});

test("add-provider form visibility preserves reserved, optional-key, and transport gates", () => {
  assert.equal(addProviderSetupGuideVisible({
    isReservedForward: false,
    isCustom: false,
    isLocal: false,
    keyOptional: false,
    note: "get a key",
  }), true);
  assert.equal(addProviderSetupGuideVisible({
    isReservedForward: false,
    isCustom: false,
    isLocal: false,
    keyOptional: true,
    note: "get a key",
  }), false);
  assert.equal(addProviderBaseUrlPlaceholderHintVisible("https://x/{slot}"), true);
  assert.equal(addProviderShowsEndpointChoices([{ id: "payg" }]), true);
  assert.equal(addProviderEndpointLabelKey("payg"), "modal.endpoint.payAsYouGo");
  assert.equal(addProviderEndpointLabelKey("other"), null);
  assert.deepEqual(
    addProviderAuthPanel({ authMode: "forward", adapter: "openai-responses" }),
    { kind: "forward" },
  );
  assert.deepEqual(
    addProviderAuthPanel({ authMode: "key", adapter: "anthropic", dashboardUrl: "https://x" }),
    { kind: "api-key", showDashboard: true, showTransport: true },
  );
  assert.equal(addProviderShowsPrivateNetworkHint(undefined), false);
  assert.equal(addProviderAccessKind({ isLocal: true, tier: "free" }), "local");
  assert.deepEqual(
    addProviderConnectionCapabilities({ auth: "oauth", oauthProvider: "anthropic", oauthSupported: ["anthropic"] }),
    { canOauth: true, canKey: true, oauthId: "anthropic", oauthReady: true },
  );
  assert.equal(accountSetupActionKind({ kind: "codex", loggedIn: true, busy: false }), "codex");
  assert.equal(accountSetupActionKind({ kind: "oauth", loggedIn: false, busy: true }), "busy");
  assert.deepEqual(accountSetupStatusCopy(true, undefined), { kind: "logged-in" });
  assert.equal(accountSetupCodexLoginKind(true, true), "enabling");
});

test("account picker decode rejects non-boolean enabled and unconfirmed saves", () => {
  assert.equal(decodeAccountPickerEnabled({ codexAccountPickerEnabled: true }), true);
  assert.equal(decodeAccountPickerEnabled({ codexAccountPickerEnabled: "true" }), null);
  assert.deepEqual(decodeAccountPickerSave({ ok: true, codexAccountPickerEnabled: false }), {
    ok: true,
    enabled: false,
    catalogRefreshPending: false,
  });
  assert.deepEqual(decodeAccountPickerSave({ ok: true, codexAccountPickerEnabled: true, catalogRefreshPending: true }), {
    ok: true,
    enabled: true,
    catalogRefreshPending: true,
  });
  assert.deepEqual(decodeAccountPickerSave({ ok: false, codexAccountPickerEnabled: true }), { ok: false });
  assert.equal(accountPickerCopyKind({ loadError: true, hydrated: false, enabled: false }), "load-failed");
  assert.equal(accountPickerCopyKind({ loadError: false, hydrated: false, enabled: false }), "loading");
  assert.equal(accountPickerShowsCompatibility(true, true), true);
  assert.equal(accountPickerShowsRefreshFailed(true, true), true);
  assert.equal(accountPickerSaveFeedbackKind(true), "pending");
  assert.equal(accountPickerInitialLoadFailed(true, false), true);
});

test("auto-switch description and blur/key policy preserve strategy and pointer intent", () => {
  assert.equal(autoSwitchDescriptionKey("quota", true), "codexAuth.autoSwitchQuotaDesc");
  assert.equal(autoSwitchDescriptionKey("round-robin", false), "codexAuth.autoSwitchRoundRobinDesc");
  assert.deepEqual(autoSwitchFeedbackView(true, { tone: "err", message: "nope" }), { kind: "saving" });
  assert.deepEqual(autoSwitchFeedbackView(false, null), { kind: "empty" });
  assert.equal(autoSwitchBlurCommit({
    relatedTargetInside: true,
    pointerIntent: true,
    enabled: true,
    controlsDisabled: false,
  }), "ignore");
  assert.equal(autoSwitchBlurCommit({
    relatedTargetInside: false,
    pointerIntent: true,
    enabled: true,
    controlsDisabled: false,
  }), "clear-pointer");
  assert.equal(autoSwitchBlurCommit({
    relatedTargetInside: false,
    pointerIntent: false,
    enabled: true,
    controlsDisabled: false,
  }), "commit");
  assert.equal(autoSwitchKeyCommand({ composing: true, controlsDisabled: false, key: "Enter" }), "ignore");
  assert.equal(autoSwitchKeyCommand({ composing: false, controlsDisabled: false, key: "Escape" }), "cancel");
});

test("account-pool action policy preserves busy short-circuit and silent remove success", () => {
  assert.equal(poolActiveNonMainAccount([{ id: "a" }], "__main__"), null);
  assert.equal(poolActiveNonMainAccount([{ id: "a" }], "a")?.id, "a");
  assert.equal(poolSwitchTargetId("x"), "x");
  assert.equal(poolAccountDisplayKind(null), "main");
  assert.equal(poolSwitchToastKind("direct"), "prepared");
  assert.equal(poolMutationBusy(false, "busy"), true);
  assert.equal(poolPauseToastKey(true, true), "codexAuth.pauseSucceeded");
  assert.equal(poolPriorityUnchanged(3, 3), true);
  assert.equal(poolRemoveToastKind(true, false), "silent");
  assert.equal(poolRemoveToastKind(true, true), "refresh-pending");
  assert.equal(poolPauseExhaustedToastKind(false, "busy", 2), "busy");
  assert.equal(poolPauseExhaustedToastKind(true, undefined, 0), "none");
  assert.equal(poolAccountAddedFeedbackKind(true), "pending");
  assert.equal(redeemResultClosesModal(true), true);
  const sorted = sortResetCredits([
    { granted_at: "2026-02-01T00:00:00Z", expires_at: "2026-03-01T00:00:00Z" },
    { granted_at: "2026-01-01T00:00:00Z", expires_at: "2026-02-01T00:00:00Z" },
  ]);
  assert.equal(sorted[0]?.granted_at, "2026-01-01T00:00:00Z");
  assert.equal(decodeResetCreditsPayload(undefined), null);
  assert.deepEqual(decodeResetCreditsPayload({ credits: [] }), []);
});

test("account card policy preserves reauth over cooldown over quota bars", () => {
  const flags = codexAccountHealthFlags({ needsReauth: true, health: { status: "healthy" } });
  assert.equal(flags.showReauth, true);
  assert.equal(poolCardIsNext({ id: "a", paused: true }, "a"), false);
  assert.equal(poolCardDotClass(true, true), "dot-amber");
  assert.equal(mainCardDotClass(false), "dot-green");
  assert.equal(codexAccountShowsSwitch({ paused: false, isActive: false, showReauth: false, inCooldown: true }), false);
  assert.equal(codexAccountQuotaMode(true, true), "reauth");
  assert.equal(codexAccountQuotaMode(false, true), "cooldown");
  assert.equal(poolCardShowsNeedsReauthBadge(true, ""), true);
  assert.equal(poolCardShowsNextBadge(true, false, false), true);
  assert.equal(poolCardShowsPinned("__main__", "__main__", true), false);
  assert.deepEqual(mainAccountSessionBadge(true, true, "direct"), {
    show: false,
    className: "badge-muted",
    key: "codexAuth.current",
  });
  assert.equal(mainAccountSessionBadge(false, true, "direct").key, "codexAuth.poolPrepared");
  assert.equal(mainQuotaPending({ quota: null }), true);
  assert.equal(mainQuotaPending(undefined), false);
  assert.equal(poolQuotaPending(null), true);
  assert.equal(poolCardOrderDisabled(null, "sw"), true);
  const entry = mainCardSwitchEntry(undefined, "Codex app");
  assert.equal(entry.id, "__main__");
  assert.equal(entry.email, "Codex app");
  assert.equal(entry.hasCredential, true);
});

test("reset modal view preserves confirm over available over empty", () => {
  assert.equal(resetCreditCount({ resetCredits: 2 }), 2);
  assert.equal(resetCreditCount(null), 0);
  assert.equal(resetCreditsAvailable(0), false);
  assert.equal(codexResetModalView(true, 0), "confirm");
  assert.equal(codexResetModalView(false, 1), "available");
  assert.equal(resetConfirmCredit([{ granted_at: "a" }, { granted_at: "b" }])?.granted_at, "a");
  assert.equal(isRedeemRequestId("550e8400-e29b-41d4-a716-446655440000"), true);
  assert.equal(isRedeemRequestId("0123456789abcdef"), false);
  assert.deepEqual(
    consumeResetCreditBody("__main__", "550e8400-e29b-41d4-a716-446655440000"),
    { accountId: "__main__", redeemRequestId: "550e8400-e29b-41d4-a716-446655440000" },
  );
});

test("Codex add-account OAuth decode preserves 409 retry, poll streak, and paste failure", () => {
  assert.deepEqual(codexAuthLoginRequestBody("acct", "ignored"), { id: "acct", reauth: true });
  assert.deepEqual(codexAuthLoginRequestBody(undefined, "  extra  "), { id: "extra" });
  assert.deepEqual(codexAuthLoginRequestBody(undefined, "   "), {});
  assert.equal(codexAuthLoginAccountId(undefined, undefined), "");
  assert.match(codexAuthLoginStatusUrl("http://127.0.0.1:23100", "flow", "acct", "acct"), /reauth=1/);
  assert.equal(codexAuthLoginStatusUrl("http://x", "", "", undefined), "http://x/api/codex-auth/login-status");
  assert.deepEqual(decodeCodexAuthLoginOpened({ url: "https://auth", flowId: "f" }), {
    kind: "opened",
    url: "https://auth",
    flowId: "f",
  });
  assert.deepEqual(decodeCodexAuthLoginOpened({ error: "nope", url: "" }), { kind: "error", error: "nope" });
  assert.equal(decodeCodexAuthConflictStep({ firstStatus: 200, alive: true, aborted: false }).kind, "continue");
  assert.equal(decodeCodexAuthConflictStep({ firstStatus: 409, alive: false, aborted: false }).kind, "dead");
  assert.equal(decodeCodexAuthConflictStep({ firstStatus: 409, alive: true, aborted: false, retryStatus: 409 }).kind, "already-in-progress");
  const missing = decodeCodexAuthPollTick({
    alive: true,
    aborted: false,
    status: null,
    errorStreak: 2,
    manualCodeWaiting: false,
  });
  assert.deepEqual(missing, { kind: "missing", retrying: true, nextStreak: 3 });
  assert.equal(decodeCodexAuthPollTick({
    alive: true,
    aborted: false,
    status: { status: "done", catalogRefreshPending: true },
    errorStreak: 1,
    manualCodeWaiting: true,
  }).kind, "done");
  assert.equal(decodeCodexAuthPollCatch({
    alive: true,
    aborted: false,
    isAbortError: true,
    errorStreak: 9,
  }).kind, "abort");
  assert.equal(codexAuthManualCodeBlocked(null, "x", false, false), true);
  assert.equal(decodeCodexAuthManualCodeFailure({ error: undefined }, "Bad"), "Bad");
  assert.equal(decodeCodexAuthTimeoutApplies(true), true);
  assert.equal(decodeCodexAuthStartCatch(true, true), false);
});

test("add-provider OAuth decode preserves unknown-provider string and poll error precedence", () => {
  assert.deepEqual(
    decodeAddProviderOAuthStart(false, { error: "unknown oauth provider" }),
    { kind: "unknown-provider" },
  );
  assert.deepEqual(
    decodeAddProviderOAuthStart(false, { error: "" }),
    { kind: "start-failed", error: "" },
  );
  assert.deepEqual(
    decodeAddProviderOAuthStart(true, { url: "https://auth" }),
    { kind: "opened", url: "https://auth" },
  );
  assert.deepEqual(
    decodeAddProviderOAuthPollTick({ error: "boom", loggedIn: true }),
    { kind: "error", error: "boom" },
  );
  assert.equal(decodeAddProviderOAuthPollTick({ loggedIn: true }).kind, "logged-in");
  assert.equal(
    decodeAddProviderOAuthPollTick({ loggedIn: true, pending: true }).kind,
    "continue",
    "leftover tokens must not finish a login that is still in the authorize window",
  );
  assert.equal(decodeAddProviderOAuthPollTick({ loggedIn: false, pending: true }).kind, "continue");
  assert.equal(decodeAddProviderManualCodeFailure({ error: "" }, "Nope"), "Nope");
});

test("providers OAuth poll preserves add-account count, reauth identity, and same-identity notice", () => {
  assert.deepEqual(providersOAuthLoginBody("anthropic", false), { provider: "anthropic" });
  assert.deepEqual(providersOAuthLoginBody("anthropic", false, "  acct  "), {
    provider: "anthropic",
    addAccount: true,
    accountId: "acct",
    reauth: true,
  });
  assert.equal(providersOAuthReauthTargetId("  "), undefined);
  assert.equal(providersOAuthLoginInfo({}, "anthropic"), null);
  assert.equal(providersOAuthErrorIsCancelled("Login cancelled by user"), true);
  assert.equal(providersOAuthPollProgress(null, { addAccount: false, baselineCount: 0 }).kind, "continue");
  assert.equal(providersOAuthPollProgress({
    loggedIn: false,
    error: "cancel now",
  }, { addAccount: false, baselineCount: 0 }).kind, "status-error");
  assert.equal(providersOAuthPollProgress({
    loggedIn: false,
    accounts: [{ id: "a", active: true }, { id: "b", active: false }],
    done: true,
  }, { addAccount: true, baselineCount: 2 }).kind, "completed");
  assert.equal(providersOAuthPollProgress({
    loggedIn: true,
  }, { addAccount: false, baselineCount: 0 }).kind, "completed");
  assert.equal(providersOAuthPollProgress({
    loggedIn: true,
    done: true,
    pending: true,
  }, { addAccount: false, baselineCount: 0 }).kind, "continue");
  const target = providersOAuthCompletedTarget(
    [{ id: "a", active: false }, { id: "b", active: true }],
    undefined,
    "a",
  );
  assert.equal(target?.id, "b");
  assert.equal(providersOAuthIdentityOutcome({ reauthTargetId: "missing" }), "missing");
  assert.equal(providersOAuthIdentityOutcome({ target: { needsReauth: true } }), "mismatch");
  assert.deepEqual(
    providersOAuthSeededAccountSet([{ id: "a", active: true }], null),
    { activeAccountId: "a", accounts: [{ id: "a", active: true }] },
  );
  assert.equal(providersOAuthSameIdentityAdd(true, undefined, 1, 1), true);
  assert.equal(providersOAuthSameIdentityAdd(true, "acct", 1, 1), false);
  assert.deepEqual(providersOAuthAccountFetchList(["openai"], "anthropic"), ["openai", "anthropic"]);
  assert.equal(providersOAuthStartFailedMessage({ error: "" }, "fallback"), "fallback");
});
