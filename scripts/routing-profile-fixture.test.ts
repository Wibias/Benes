import assert from "node:assert/strict";
import test from "node:test";
import {
  FIXTURE_PROVIDERS,
  buildFixtureConfig,
  buildProfileSeeds,
  flattenCatalog,
  longestCatalogEntries,
} from "./routing-profile-fixture.ts";

test("fixture catalog yields enough distinct candidates for representative seeds", () => {
  const catalog = flattenCatalog(FIXTURE_PROVIDERS);
  assert.ok(catalog.length >= 10);
  const providers = new Set(catalog.map((row) => row.provider));
  assert.ok(providers.size >= 5);
  const long = longestCatalogEntries(catalog);
  assert.ok(long.provider.length > 0);
  assert.ok(long.model.includes("/") || long.model.length >= 8);
});

test("buildProfileSeeds covers required cases without inventing suite ids", () => {
  const catalog = flattenCatalog();
  const withoutSuites = buildProfileSeeds(catalog, []);
  assert.equal(withoutSuites.seeds.length, 6);
  assert.ok(withoutSuites.limitations.some((line) => /suite/i.test(line)));
  const strict = withoutSuites.seeds.find((seed) => seed.id === "strict");
  assert.ok(strict);
  const compat = (strict!.profile.compatibility ?? {}) as { requiredSuites?: unknown };
  assert.equal(compat.requiredSuites, undefined);

  const withSuites = buildProfileSeeds(catalog, ["codex", "claude"]);
  const strictOn = withSuites.seeds.find((seed) => seed.id === "strict")!;
  const suites = (strictOn.profile.compatibility as { requiredSuites: Array<{ suiteId: string }> })
    .requiredSuites;
  assert.deepEqual(suites, [{ suiteId: "codex", evidenceLayer: "protocol_conformance" }]);

  const explicit = withSuites.seeds.find((seed) => seed.id === "explicit-off")!;
  assert.deepEqual(explicit.profile.require, {
    tools: false,
    imageInput: false,
    structuredOutput: false,
    localOnly: false,
    remoteAllowed: false,
    encryptedCodexTasks: false,
    minQuotaHeadroom: 0,
  });

  const minimal = withSuites.seeds.find((seed) => seed.id === "minimal")!;
  assert.equal("alias" in minimal.profile, false);
  assert.equal((minimal.profile.candidates as unknown[]).length, 1);

  const many = withSuites.seeds.find((seed) => seed.id === "many-candidates")!;
  assert.ok((many.profile.candidates as unknown[]).length >= 6);

  const long = withSuites.seeds.find((seed) => seed.label === "long-identifiers")!;
  assert.ok(long.id.length <= 64);
  assert.ok(String(long.profile.alias).length > 40);
});

test("fixture config never embeds secrets and avoids reserved ports by construction", () => {
  const config = buildFixtureConfig(24567);
  assert.equal(config.port, 24567);
  assert.equal(config.hostname, "127.0.0.1");
  const raw = JSON.stringify(config);
  assert.equal(/"apiKey"\s*:|"sk-[A-Za-z0-9]/.test(raw), false);
  assert.ok(Object.keys(config.providers).length >= 5);
});
