import assert from "node:assert/strict";
import test from "node:test";
import {
  SESSIONS_ROUTING_BANNER_CLEAR_HASH,
  SESSIONS_ROUTING_BANNER_ROOT_CLASS,
  projectSessionsRoutingBanner,
  sessionsRoutingBannerIsVisible,
} from "../src/pages/sessions-routing-banner-model.ts";

test("banner projection is absent when both ids are empty", () => {
  const projection = projectSessionsRoutingBanner("", "");
  assert.equal(projection.kind, "absent");
  assert.equal(projection.subjectId, "");
  assert.equal(projection.sentenceKey, null);
  assert.equal(sessionsRoutingBannerIsVisible(projection), false);
});

test("banner projection prefers policy when only policyId is set", () => {
  const projection = projectSessionsRoutingBanner("primary", "");
  assert.equal(projection.kind, "policy");
  assert.equal(projection.subjectId, "primary");
  assert.equal(projection.sentenceKey, "sessions.filter.routing.active.policy");
  assert.equal(sessionsRoutingBannerIsVisible(projection), true);
});

test("banner projection uses combo when only comboId is set", () => {
  const projection = projectSessionsRoutingBanner("", "fast");
  assert.equal(projection.kind, "combo");
  assert.equal(projection.subjectId, "fast");
  assert.equal(projection.sentenceKey, "sessions.filter.routing.active.combo");
  assert.equal(sessionsRoutingBannerIsVisible(projection), true);
});

test("banner projection gives policy precedence when both ids are set", () => {
  const projection = projectSessionsRoutingBanner("primary", "fast");
  assert.equal(projection.kind, "policy");
  assert.equal(projection.subjectId, "primary");
  assert.equal(projection.sentenceKey, "sessions.filter.routing.active.policy");
});

test("banner projection keeps raw ids without policy/combo prefixes", () => {
  const policy = projectSessionsRoutingBanner("primary", "");
  const combo = projectSessionsRoutingBanner("", "fast");
  assert.equal(policy.subjectId.includes("policy/"), false);
  assert.equal(combo.subjectId.includes("combo/"), false);
  assert.equal(policy.subjectId, "primary");
  assert.equal(combo.subjectId, "fast");
});

test("banner clear hash stays on the shared sessions authority", () => {
  const projection = projectSessionsRoutingBanner("primary", "");
  assert.equal(projection.clearHash, SESSIONS_ROUTING_BANNER_CLEAR_HASH);
  assert.equal(projection.clearHash, "sessions");
  assert.equal(projection.clearKey, "sessions.filter.routing.clear");
  assert.equal(SESSIONS_ROUTING_BANNER_ROOT_CLASS, "sessions-routing-filter");
});
