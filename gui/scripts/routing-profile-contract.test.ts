/**
 * Routing-profile editor contract.
 *
 * Pins the DTO <-> draft codec, the PUT body shape, the fail-honest response
 * helpers, and the ownership boundary between profile eligibility and the
 * generic request policy.
 */

import assert from "node:assert/strict";
import test from "node:test";

import {
  ROUTING_PROFILE_DEFAULT_ICON,
  newDraftCandidate,
  newDraftOptimize,
  newDraftUnknownEvidence,
  newRoutingProfileDraft,
  type RoutingProfileDraft,
  type RoutingProfileDto,
} from "../src/routing-profile/profile-model.ts";
import {
  normalizeCompatibilityDto,
  normalizeCompatibilitySuites,
  parseRoutingProfiles,
  routingProfileDraftFromDto,
  routingProfilePutBody,
} from "../src/routing-profile/profile-codec.ts";
import {
  routingProfileResponseError,
  routingProfileResponseSucceeded,
} from "../src/routing-profile/profile-response.ts";

function profileDto(overrides: Partial<RoutingProfileDto> = {}): RoutingProfileDto {
  return {
    id: "fast",
    alias: null,
    icon: null,
    model: "combo/fast",
    revision: "rev-1",
    candidates: [{ provider: "openai", model: "gpt-5" }],
    require: {},
    optimize: { latency: 0.5, health: 0.3, cost: 0.1, quota: 0.1 },
    limits: {},
    unknownEvidence: { capability: "exclude", health: "penalize", quota: "penalize", cost: "penalize" },
    ...overrides,
  };
}

function wire(profile: Record<string, unknown>, key: string): Record<string, unknown> {
  const value = profile[key];
  assert.ok(value && typeof value === "object", `expected ${key} to be an object`);
  return value as Record<string, unknown>;
}

// ---------------------------------------------------------------------------
// New draft
// ---------------------------------------------------------------------------

test("a new draft carries the shipped defaults", () => {
  const draft = newRoutingProfileDraft("openai", "gpt-5");
  assert.equal(draft.id, "");
  assert.equal(draft.alias, "");
  assert.equal(draft.icon, ROUTING_PROFILE_DEFAULT_ICON);
  assert.deepEqual(draft.candidates.map(candidate => [candidate.provider, candidate.model]), [["openai", "gpt-5"]]);
  assert.deepEqual(draft.optimize, newDraftOptimize());
  assert.deepEqual(draft.unknownEvidence, newDraftUnknownEvidence());
  assert.deepEqual(draft.limits, { maxEstimatedCostUsd: "", onUnknownCost: "allow" });
  assert.deepEqual(draft.compatibility, {
    enabled: false,
    requiredSuites: [],
    minStatus: "",
    maxEvidenceAgeMs: "",
    unknownEvidence: "exclude",
    degradedEvidence: "penalize",
  });
  assert.deepEqual(Object.values(draft.require), ["", "", "", "", "", "", "", "", "", ""]);
});

test("drafts do not share candidate, weight or suite state", () => {
  const first = newRoutingProfileDraft();
  const second = newRoutingProfileDraft();
  assert.notEqual(first.candidates[0]?.key, second.candidates[0]?.key);
  first.optimize.latency = "0.9";
  first.unknownEvidence.capability = "allow";
  first.compatibility.requiredSuites.push({ suiteId: "codex", evidenceLayer: "protocol_conformance" });
  first.candidates.push(newDraftCandidate("anthropic", "claude"));
  assert.equal(second.optimize.latency, newDraftOptimize().latency);
  assert.equal(second.unknownEvidence.capability, "exclude");
  assert.deepEqual(second.compatibility.requiredSuites, []);
  assert.equal(second.candidates.length, 1);
});

test("a draft candidate keeps its key while the row is edited", () => {
  const candidate = newDraftCandidate("openai", "gpt-5");
  const key = candidate.key;
  candidate.model = "gpt-5-mini";
  assert.equal(candidate.key, key);
  assert.notEqual(newDraftCandidate("openai", "gpt-5").key, key);
});

// ---------------------------------------------------------------------------
// DTO -> draft
// ---------------------------------------------------------------------------

test("a nullable alias becomes an empty editor string", () => {
  assert.equal(routingProfileDraftFromDto(profileDto()).alias, "");
  assert.equal(routingProfileDraftFromDto(profileDto({ alias: "Fast lane" })).alias, "Fast lane");
});

test("a missing or blank icon falls back to the default icon", () => {
  assert.equal(routingProfileDraftFromDto(profileDto()).icon, ROUTING_PROFILE_DEFAULT_ICON);
  assert.equal(routingProfileDraftFromDto(profileDto({ icon: "   " })).icon, ROUTING_PROFILE_DEFAULT_ICON);
  assert.equal(routingProfileDraftFromDto(profileDto({ icon: "  rocket-launch " })).icon, "rocket-launch");
});

test("candidates gain editor keys and keep their provider/model", () => {
  const draft = routingProfileDraftFromDto(profileDto({
    candidates: [{ provider: "openai", model: "gpt-5" }, { provider: "anthropic", model: "claude" }],
  }));
  assert.deepEqual(draft.candidates.map(candidate => [candidate.provider, candidate.model]), [
    ["openai", "gpt-5"],
    ["anthropic", "claude"],
  ]);
  const keys = draft.candidates.map(candidate => candidate.key);
  assert.equal(keys.every(key => typeof key === "string" && key.length > 0), true);
  assert.equal(new Set(keys).size, 2);
});

test("requirement values round-trip through the editor representation", () => {
  const draft = routingProfileDraftFromDto(profileDto({
    require: {
      minContextWindow: 128_000,
      minQuotaHeadroom: 12.5,
      tools: true,
      imageInput: false,
      structuredOutput: true,
      reasoningEffort: "high",
      serviceTier: "priority",
      localOnly: false,
      remoteAllowed: true,
      encryptedCodexTasks: true,
    },
  }));
  assert.deepEqual(draft.require, {
    minContextWindow: "128000",
    minQuotaHeadroom: "12.5",
    tools: "true",
    imageInput: "false",
    structuredOutput: "true",
    reasoningEffort: "high",
    serviceTier: "priority",
    localOnly: "false",
    remoteAllowed: "true",
    encryptedCodexTasks: "true",
  });
  assert.deepEqual(routingProfileDraftFromDto(profileDto()).require, {
    minContextWindow: "",
    minQuotaHeadroom: "",
    tools: "",
    imageInput: "",
    structuredOutput: "",
    reasoningEffort: "",
    serviceTier: "",
    localOnly: "",
    remoteAllowed: "",
    encryptedCodexTasks: "",
  });
});

test("weights, limits and unknown-evidence policy become editor values", () => {
  const draft = routingProfileDraftFromDto(profileDto({
    optimize: { latency: 0.55, health: 0.25, cost: 0.1, quota: 0.1 },
    limits: { maxEstimatedCostUsd: 1.5, onUnknownCost: "exclude" },
  }));
  assert.deepEqual(draft.optimize, { latency: "0.55", health: "0.25", cost: "0.1", quota: "0.1" });
  assert.deepEqual(draft.limits, { maxEstimatedCostUsd: "1.5", onUnknownCost: "exclude" });
  assert.deepEqual(routingProfileDraftFromDto(profileDto()).limits, {
    maxEstimatedCostUsd: "",
    onUnknownCost: "allow",
  });
  assert.deepEqual(draft.unknownEvidence, {
    capability: "exclude",
    health: "penalize",
    quota: "penalize",
    cost: "penalize",
  });
});

test("an absent compatibility policy stays disabled with presentation defaults", () => {
  assert.deepEqual(routingProfileDraftFromDto(profileDto()).compatibility, {
    enabled: false,
    requiredSuites: [],
    minStatus: "",
    maxEvidenceAgeMs: "",
    unknownEvidence: "exclude",
    degradedEvidence: "penalize",
  });
});

test("a normalized compatibility policy enables the editor block", () => {
  const draft = routingProfileDraftFromDto(profileDto({
    compatibility: {
      requiredSuites: [{ suiteId: "codex", evidenceLayer: "live_route_compatibility" }],
      minStatus: "VERIFIED",
      maxEvidenceAgeMs: 3_600_000,
      unknownEvidence: "penalize",
      degradedEvidence: "exclude",
    },
  }));
  assert.deepEqual(draft.compatibility, {
    enabled: true,
    requiredSuites: [{ suiteId: "codex", evidenceLayer: "live_route_compatibility" }],
    minStatus: "VERIFIED",
    maxEvidenceAgeMs: "3600000",
    unknownEvidence: "penalize",
    degradedEvidence: "exclude",
  });
});

test("malformed compatibility blocks are absent, never fabricated", () => {
  for (const raw of [null, undefined, "nope", 5, [], true]) {
    assert.equal(normalizeCompatibilityDto(raw), undefined);
  }
  assert.deepEqual(normalizeCompatibilityDto({ minStatus: "MAYBE", unknownEvidence: "maybe" }), {
    requiredSuites: [],
  });
  assert.deepEqual(normalizeCompatibilityDto({ maxEvidenceAgeMs: -1 }), { requiredSuites: [] });
  assert.deepEqual(normalizeCompatibilityDto({ maxEvidenceAgeMs: Number.NaN }), { requiredSuites: [] });
});

test("suite rows normalize and drop unusable entries", () => {
  assert.deepEqual(normalizeCompatibilitySuites([
    null,
    5,
    "codex",
    {},
    { suiteId: "   " },
    { suiteId: "codex" },
    { suiteId: " x ", evidenceLayer: "task_effectiveness" },
    { suiteId: "  codex  ", evidenceLayer: "protocol_conformance" },
    { suiteId: "live", evidenceLayer: "live_route_compatibility" },
  ]), [
    { suiteId: "codex", evidenceLayer: "protocol_conformance" },
    { suiteId: "live", evidenceLayer: "live_route_compatibility" },
  ]);
  assert.deepEqual(normalizeCompatibilitySuites("codex"), []);
});


// ---------------------------------------------------------------------------
// Draft -> PUT body
// ---------------------------------------------------------------------------

test("create sends mode and id without an expected revision", () => {
  const draft = newRoutingProfileDraft("openai", "gpt-5");
  draft.id = "  fast  ";
  const body = routingProfilePutBody(draft, "create");
  assert.equal(body.mode, "create");
  assert.equal(body.id, "fast");
  assert.equal("expectedRevision" in body, false);
  assert.deepEqual(Object.keys(body), ["mode", "id", "profile"]);
});

test("update carries the optimistic revision only when one was loaded", () => {
  const draft = newRoutingProfileDraft();
  draft.id = "fast";
  const withRevision = routingProfilePutBody(draft, "update", "rev-7");
  assert.equal(withRevision.expectedRevision, "rev-7");
  assert.deepEqual(Object.keys(withRevision), ["mode", "id", "expectedRevision", "profile"]);
  const withoutRevision = routingProfilePutBody(draft, "update");
  assert.equal("expectedRevision" in withoutRevision, false);
  assert.equal("expectedRevision" in routingProfilePutBody(draft, "update", ""), false);
});

test("blank alias and blank icon fall back to the current server contract", () => {
  const draft = newRoutingProfileDraft();
  draft.alias = "   ";
  draft.icon = "  ";
  const profile = routingProfilePutBody(draft, "create").profile;
  assert.equal("alias" in profile, false);
  assert.equal(profile.icon, ROUTING_PROFILE_DEFAULT_ICON);

  draft.alias = "  Fast  ";
  draft.icon = "  tune  ";
  const named = routingProfilePutBody(draft, "create").profile;
  assert.equal(named.alias, "Fast");
  assert.equal(named.icon, "tune");
});

test("client-only candidate keys never reach the server", () => {
  const draft = newRoutingProfileDraft(" openai ", " gpt-5 ");
  draft.candidates.push(newDraftCandidate("anthropic", "claude"));
  const profile = routingProfilePutBody(draft, "create").profile;
  const candidates = profile.candidates as Array<Record<string, unknown>>;
  assert.deepEqual(candidates, [
    { provider: "openai", model: "gpt-5" },
    { provider: "anthropic", model: "claude" },
  ]);
  assert.equal(JSON.stringify(candidates).includes("key"), false);
});

test("requirement fields are omitted when the editor means not specified", () => {
  const draft = newRoutingProfileDraft();
  assert.equal("require" in routingProfilePutBody(draft, "create").profile, false);

  draft.require.minContextWindow = " 128000 ";
  draft.require.minQuotaHeadroom = "";
  draft.require.tools = "true";
  draft.require.imageInput = "false";
  draft.require.structuredOutput = "";
  draft.require.reasoningEffort = "  ";
  draft.require.serviceTier = "priority";
  draft.require.localOnly = "";
  draft.require.remoteAllowed = "";
  draft.require.encryptedCodexTasks = "true";
  assert.deepEqual(wire(routingProfilePutBody(draft, "create").profile, "require"), {
    minContextWindow: 128_000,
    tools: true,
    imageInput: false,
    serviceTier: "priority",
    encryptedCodexTasks: true,
  });
});

test("blank numeric input stays absent instead of becoming zero", () => {
  const draft = newRoutingProfileDraft();
  draft.require.minContextWindow = "   ";
  draft.limits.maxEstimatedCostUsd = "";
  const profile = routingProfilePutBody(draft, "create").profile;
  assert.equal("require" in profile, false);
  assert.equal("limits" in profile, false);

  draft.limits.maxEstimatedCostUsd = " 0 ";
  assert.equal(wire(routingProfilePutBody(draft, "create").profile, "limits").maxEstimatedCostUsd, 0);
});


test("weights are emitted as numbers and unknown-evidence policy travels whole", () => {
  const draft = newRoutingProfileDraft();
  draft.optimize = { latency: "0.55", health: "0.25", cost: "0.1", quota: "0.1" };
  draft.unknownEvidence = { capability: "allow", health: "exclude", quota: "penalize", cost: "exclude" };
  const profile = routingProfilePutBody(draft, "create").profile;
  assert.deepEqual(profile.optimize, { latency: 0.55, health: 0.25, cost: 0.1, quota: 0.1 });
  assert.deepEqual(profile.unknownEvidence, {
    capability: "allow",
    health: "exclude",
    quota: "penalize",
    cost: "exclude",
  });
});

test("the cost cap treats allow as omission and exclude as an explicit value", () => {
  const draft = newRoutingProfileDraft();
  assert.equal("limits" in routingProfilePutBody(draft, "create").profile, false);
  draft.limits.onUnknownCost = "exclude";
  assert.deepEqual(wire(routingProfilePutBody(draft, "create").profile, "limits"), {
    onUnknownCost: "exclude",
  });
  draft.limits.onUnknownCost = "allow";
  assert.equal("limits" in routingProfilePutBody(draft, "create").profile, false);
});

test("a disabled compatibility policy is omitted entirely", () => {
  const draft = newRoutingProfileDraft();
  draft.compatibility.requiredSuites.push({ suiteId: "codex", evidenceLayer: "protocol_conformance" });
  draft.compatibility.minStatus = "VERIFIED";
  assert.equal("compatibility" in routingProfilePutBody(draft, "create").profile, false);
});

test("an enabled compatibility policy emits its normalized fields", () => {
  const draft = newRoutingProfileDraft();
  draft.compatibility.enabled = true;
  draft.compatibility.requiredSuites = [{ suiteId: "codex", evidenceLayer: "live_route_compatibility" }];
  draft.compatibility.minStatus = "PROBED";
  draft.compatibility.maxEvidenceAgeMs = "3600000";
  draft.compatibility.unknownEvidence = "exclude";
  draft.compatibility.degradedEvidence = "penalize";
  const block = wire(routingProfilePutBody(draft, "create").profile, "compatibility");
  assert.deepEqual(block, {
    requiredSuites: [{ suiteId: "codex", evidenceLayer: "live_route_compatibility" }],
    minStatus: "PROBED",
    maxEvidenceAgeMs: 3_600_000,
    unknownEvidence: "exclude",
    degradedEvidence: "penalize",
  });
  assert.equal("enabled" in block, false);
});

test("an enabled compatibility policy keeps an empty suite list but drops blank optionals", () => {
  const draft = newRoutingProfileDraft();
  draft.compatibility.enabled = true;
  draft.compatibility.minStatus = "";
  draft.compatibility.maxEvidenceAgeMs = "   ";
  assert.deepEqual(wire(routingProfilePutBody(draft, "create").profile, "compatibility"), {
    requiredSuites: [],
    unknownEvidence: "exclude",
    degradedEvidence: "penalize",
  });
});

test("profile eligibility never writes the generic request policy", () => {
  const draft = newRoutingProfileDraft("openai", "gpt-5");
  draft.require.serviceTier = "priority";
  const body = routingProfilePutBody(draft, "create");
  assert.equal("requestPolicy" in body, false);
  assert.equal(JSON.stringify(body).includes("requestPolicy"), false);
  assert.deepEqual(wire(body.profile, "require"), { serviceTier: "priority" });
});


// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

test("a malformed profile envelope yields an empty list", () => {
  assert.deepEqual(parseRoutingProfiles(null), []);
  assert.deepEqual(parseRoutingProfiles([]), []);
  assert.deepEqual(parseRoutingProfiles({}), []);
  assert.deepEqual(parseRoutingProfiles({ profiles: "fast" }), []);
});

test("structurally invalid profile rows are skipped, not accepted", () => {
  const parsed = parseRoutingProfiles({
    profiles: [
      profileDto({ id: "fast" }),
      null,
      5,
      "fast",
      [],
      { id: "no-model", revision: "rev" },
      { ...profileDto({ id: "no-candidates" }), candidates: "none" },
      { ...profileDto({ id: "no-require" }), require: null },
      { ...profileDto({ id: "no-optimize" }), optimize: [] },
      { ...profileDto({ id: "no-limits" }), limits: "none" },
      { ...profileDto({ id: "no-unknown" }), unknownEvidence: 7 },
      profileDto({ id: "slow" }),
    ],
  });
  assert.deepEqual(parsed.map(profile => profile.id), ["fast", "slow"]);
});

test("aliases and icons are normalized on the way in", () => {
  const parsed = parseRoutingProfiles({
    profiles: [
      profileDto({ id: "a", alias: null, icon: null }),
      profileDto({ id: "b", alias: "  Fast  ", icon: "  tune  " }),
      { ...profileDto({ id: "c" }), alias: undefined, icon: "" },
    ],
  });
  assert.deepEqual(parsed.map(profile => [profile.alias, profile.icon]), [
    [null, null],
    ["  Fast  ", "tune"],
    [null, null],
  ]);
});

test("a malformed compatibility block on one row does not disturb the others", () => {
  const parsed = parseRoutingProfiles({
    profiles: [
      profileDto({ id: "broken", compatibility: "nope" }),
      profileDto({ id: "broken-array", compatibility: [] }),
      profileDto({
        id: "good",
        compatibility: {
          requiredSuites: [null, { suiteId: "codex" }, { suiteId: " codex ", evidenceLayer: "live_route_compatibility" }],
          minStatus: "VERIFIED",
          maxEvidenceAgeMs: "soon",
          unknownEvidence: "penalize",
          degradedEvidence: "nope",
        },
      }),
    ],
  });
  assert.equal("compatibility" in parsed[0]!, false);
  assert.equal("compatibility" in parsed[1]!, false);
  assert.deepEqual(parsed[2]!.compatibility, {
    requiredSuites: [{ suiteId: "codex", evidenceLayer: "live_route_compatibility" }],
    minStatus: "VERIFIED",
    unknownEvidence: "penalize",
  });
});


// ---------------------------------------------------------------------------
// Response helpers
// ---------------------------------------------------------------------------

test("error copy is read from a plain string or a nested message", () => {
  assert.equal(routingProfileResponseError({ error: "unknown profile" }), "unknown profile");
  assert.equal(routingProfileResponseError({ error: { message: "revision conflict" } }), "revision conflict");
  assert.equal(routingProfileResponseError({ error: { message: "  " } }), undefined);
  assert.equal(routingProfileResponseError({ message: "top level" }), undefined);
});

test("a malformed error envelope yields no copy rather than a fabricated message", () => {
  assert.equal(routingProfileResponseError(null), undefined);
  assert.equal(routingProfileResponseError([]), undefined);
  assert.equal(routingProfileResponseError("boom"), undefined);
  assert.equal(routingProfileResponseError({ error: 5 }), undefined);
  assert.equal(routingProfileResponseError({ error: "" }), undefined);
  assert.equal(routingProfileResponseError({ error: "   " }), undefined);
  assert.equal(routingProfileResponseError({ error: [] }), undefined);
  assert.equal(routingProfileResponseError({ error: { message: 5 } }), undefined);
});

test("only an explicit success flag counts as success", () => {
  assert.equal(routingProfileResponseSucceeded({ success: true }), true);
  assert.equal(routingProfileResponseSucceeded({ success: false }), false);
  assert.equal(routingProfileResponseSucceeded({ success: "true" }), false);
  assert.equal(routingProfileResponseSucceeded({}), false);
  assert.equal(routingProfileResponseSucceeded({ success: true, revision: "rev-2" }), true);
  assert.equal(routingProfileResponseSucceeded([]), false);
  assert.equal(routingProfileResponseSucceeded(null), false);
  assert.equal(routingProfileResponseSucceeded("ok"), false);
});

// ---------------------------------------------------------------------------
// Round trip
// ---------------------------------------------------------------------------

test("a DTO survives draft conversion and back to the wire", () => {
  const dto = profileDto({
    id: "fast",
    alias: "Fast lane",
    icon: "rocket-launch",
    revision: "rev-9",
    candidates: [{ provider: "openai", model: "gpt-5" }, { provider: "anthropic", model: "claude" }],
    require: { minContextWindow: 128_000, tools: true, serviceTier: "priority" },
    optimize: { latency: 0.5, health: 0.3, cost: 0.15, quota: 0.05 },
    limits: { maxEstimatedCostUsd: 2, onUnknownCost: "exclude" },
    compatibility: {
      requiredSuites: [{ suiteId: "codex", evidenceLayer: "protocol_conformance" }],
      minStatus: "VERIFIED",
      maxEvidenceAgeMs: 60_000,
      unknownEvidence: "exclude",
      degradedEvidence: "penalize",
    },
  });
  const draft: RoutingProfileDraft = routingProfileDraftFromDto(dto);
  const body = routingProfilePutBody(draft, "update", dto.revision);
  assert.equal(body.mode, "update");
  assert.equal(body.id, "fast");
  assert.equal(body.expectedRevision, "rev-9");
  assert.deepEqual(body.profile, {
    alias: "Fast lane",
    icon: "rocket-launch",
    candidates: [{ provider: "openai", model: "gpt-5" }, { provider: "anthropic", model: "claude" }],
    require: { minContextWindow: 128_000, tools: true, serviceTier: "priority" },
    optimize: { latency: 0.5, health: 0.3, cost: 0.15, quota: 0.05 },
    limits: { maxEstimatedCostUsd: 2, onUnknownCost: "exclude" },
    unknownEvidence: { capability: "exclude", health: "penalize", quota: "penalize", cost: "penalize" },
    compatibility: {
      requiredSuites: [{ suiteId: "codex", evidenceLayer: "protocol_conformance" }],
      minStatus: "VERIFIED",
      maxEvidenceAgeMs: 60_000,
      unknownEvidence: "exclude",
      degradedEvidence: "penalize",
    },
  });
});
