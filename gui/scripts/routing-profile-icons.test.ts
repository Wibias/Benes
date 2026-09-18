import assert from "node:assert/strict";
import test from "node:test";
import {
  ensureRoutingProfileIconCatalog,
  filterRoutingProfileIconIds,
  normalizeRoutingProfileIconId,
  resetRoutingProfileIconCatalogCacheForTests,
  ROUTING_PROFILE_DEFAULT_ICON,
  routingProfileIconPath,
} from "../src/pages/routing-profile-icon-data.ts";

test("normalizeRoutingProfileIconId falls back for empty/unknown", () => {
  resetRoutingProfileIconCatalogCacheForTests();
  assert.equal(normalizeRoutingProfileIconId(""), ROUTING_PROFILE_DEFAULT_ICON);
  assert.equal(normalizeRoutingProfileIconId("not-an-icon"), ROUTING_PROFILE_DEFAULT_ICON);
  assert.equal(normalizeRoutingProfileIconId("rocket-launch"), "rocket-launch");
});

test("filterRoutingProfileIconIds matches substring case-insensitively", () => {
  const ids = ["rocket-launch", "file-document", "shield-check"];
  assert.deepEqual(filterRoutingProfileIconIds(ids, "ROCKET"), ["rocket-launch"]);
  assert.deepEqual(filterRoutingProfileIconIds(ids, "  "), ids);
});

test("ensureRoutingProfileIconCatalog loads curated paths and unlocks normalize", async () => {
  resetRoutingProfileIconCatalogCacheForTests();
  assert.equal(normalizeRoutingProfileIconId("shield-lock"), ROUTING_PROFILE_DEFAULT_ICON);
  const catalog = await ensureRoutingProfileIconCatalog();
  assert.ok(catalog.ids.length >= 100);
  assert.equal(normalizeRoutingProfileIconId("shield-lock"), "shield-lock");
  assert.notEqual(
    routingProfileIconPath("shield-lock"),
    routingProfileIconPath(ROUTING_PROFILE_DEFAULT_ICON),
  );
});
