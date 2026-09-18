import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  SERVICE_TIER_OPTIONS,
  isRequestPolicyServiceTier,
  readRequestPolicyServiceTier,
  requestPolicyPutBody,
} from "../src/request-policy-service-tier.ts";

const guiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const settingSource = () => readFileSync(
  path.join(guiRoot, "src", "pages", "RequestPolicyServiceTierSetting.tsx"),
  "utf8",
);

test("service-tier choices keep unset distinct from OpenAI auto", () => {
  assert.deepEqual(SERVICE_TIER_OPTIONS, ["", "auto", "default", "flex", "priority"]);
});

test("settings response reads only canonical service-tier values", () => {
  assert.equal(readRequestPolicyServiceTier({ requestPolicy: { serviceTier: "priority" } }), "priority");
  assert.equal(readRequestPolicyServiceTier({ requestPolicy: { serviceTier: "fast" } }), "");
  assert.equal(readRequestPolicyServiceTier({ requestPolicy: null }), "");
  assert.equal(readRequestPolicyServiceTier(null), "");
});

test("PUT shape clears the canonical owner instead of inventing auto", () => {
  assert.deepEqual(requestPolicyPutBody(""), { requestPolicy: {} });
  assert.deepEqual(requestPolicyPutBody("auto"), { requestPolicy: { serviceTier: "auto" } });
  assert.deepEqual(requestPolicyPutBody("priority"), { requestPolicy: { serviceTier: "priority" } });
});

test("service-tier type guard rejects non-contract values", () => {
  assert.equal(isRequestPolicyServiceTier("flex"), true);
  assert.equal(isRequestPolicyServiceTier("fast"), false);
  assert.equal(isRequestPolicyServiceTier("PRIORITY"), false);
});

test("the settings card reports through the shared toast and management read policy", () => {
  const source = settingSource();
  assert.match(source, /<ToastNotice/);
  assert.match(source, /async function settingsEnvelope\(response: Response\)/);
  assert.match(source, /failure\?\.error\?\.message \|\| failure\?\.message/);
  assert.match(source, /announce\("err", error instanceof Error \? error\.message/);
  // No save or read result may be parked in page flow as an inline banner.
  assert.doesNotMatch(source, /className="muted" role="status"/);
});
