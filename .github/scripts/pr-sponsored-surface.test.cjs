"use strict";

const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const {
  classifySecuritySurfaces,
  pathRequiresMaintainerReview,
  missingMaintainerSponsorship,
} = require("./pr-sponsored-surface.cjs");

describe("Benes security surfaces", () => {
  it("covers credentials, GitHub automation, release, and dependency files", () => {
    for (const file of [
      "internal/oauth/chatgpt/login.go",
      "internal/codexauth/refresh.go",
      "internal/credentials/store.go",
      "internal/credentialpool/pool.go",
      "internal/server/oauth_accounts_api.go",
      "internal/server/codex_auth_refresh.go",
      "internal/server/auth_session.go",
      "internal/server/credentials_api.go",
      ".github/workflows/release.yml",
      ".github/scripts/pr-labeler.cjs",
      "scripts/release.ts",
      "scripts/lib/release/plan.ts",
      "scripts/node-runtime.ts",
      "scripts/win-exec.ts",
      "scripts/prepare-package.ts",
      "package.json",
      "package-lock.json",
    ]) {
      assert.equal(pathRequiresMaintainerReview(file), true, file);
    }
  });

  it("leaves ordinary product paths unrestricted", () => {
    for (const file of [
      "internal/router/selector.go",
      "internal/combo/walker.go",
      "internal/server/chat.go",
      "gui/src/pages/Providers.tsx",
      "docs/src/content/docs/start/how-traffic-flows.md",
      ".github/ISSUE_TEMPLATE/bug_report.yml",
    ]) {
      assert.equal(pathRequiresMaintainerReview(file), false, file);
    }
  });

  it("groups changed files by surface id", () => {
    const surfaces = classifySecuritySurfaces([
      "internal/oauth/chatgpt/login.go",
      "internal/combo/walker.go",
      ".github/scripts/pr-hygiene.cjs",
      "scripts/lib/release/plan.ts",
    ]);
    assert.deepEqual(
      surfaces.map((surface) => surface.id).sort(),
      ["credentials", "github-automation", "release"],
    );
  });
});

describe("sponsorship decision", () => {
  it("requires an explicit maintainer-sponsored label for an external author", () => {
    const gaps = missingMaintainerSponsorship({
      changedFiles: ["internal/server/oauth_accounts_api.go"],
    });
    assert.equal(gaps[0].code, "unsponsored_surface");
    assert.deepEqual(gaps[0].paths, ["internal/server/oauth_accounts_api.go"]);
  });

  it("clears when maintainer-sponsored is present as a string or object", () => {
    assert.deepEqual(
      missingMaintainerSponsorship({
        changedFiles: ["internal/oauth/chatgpt/login.go"],
        labels: ["maintainer-sponsored"],
      }),
      [],
    );
    assert.deepEqual(
      missingMaintainerSponsorship({
        changedFiles: ["internal/oauth/chatgpt/login.go"],
        labels: [{ name: "maintainer-sponsored" }],
      }),
      [],
    );
  });

  it("lets an author with write access change security surfaces without the label", () => {
    assert.deepEqual(
      missingMaintainerSponsorship({
        authorHasPushPermission: true,
        changedFiles: ["scripts/release.ts"],
      }),
      [],
    );
  });

  it("does not treat MEMBER association as write access", () => {
    const gaps = missingMaintainerSponsorship({
      authorAssociation: "MEMBER",
      changedFiles: [".github/workflows/release.yml"],
    });
    assert.equal(gaps[0].code, "unsponsored_surface");
  });

  it("passes ordinary product diffs", () => {
    assert.deepEqual(
      missingMaintainerSponsorship({
        changedFiles: ["internal/combo/walker.go", "gui/src/pages/Providers.tsx"],
      }),
      [],
    );
  });
});
