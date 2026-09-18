import assert from "node:assert/strict";
import { describe, test } from "node:test";
import { classifyPrCiPaths, needsCiLocal, scopeEnvValue } from "./ci-pr-scope.ts";

describe("classifyPrCiPaths", () => {
  test("docs-only skips Go, WSL, packaging, and GUI", () => {
    const buckets = classifyPrCiPaths(["docs/src/content/docs/start/install.md"]);
    assert.deepEqual(buckets, {
      docs: true,
      gui: false,
      go: false,
      automation: false,
      privacy: false,
      keyring: false,
      packaging: false,
      linux: false,
      darwin: false,
      full: false,
    });
    assert.equal(needsCiLocal(buckets), false);
    assert.equal(scopeEnvValue(buckets), "docs");
  });

  test("gui-only runs dashboard gates and privacy, not Go or pack", () => {
    const buckets = classifyPrCiPaths(["gui/src/App.tsx"]);
    assert.equal(buckets.gui, true);
    assert.equal(buckets.privacy, true);
    assert.equal(buckets.go, false);
    assert.equal(buckets.packaging, false);
    assert.equal(buckets.linux, false);
    assert.equal(needsCiLocal(buckets), false);
  });

  test("Go source runs full Go tests plus Linux and Darwin compile", () => {
    const buckets = classifyPrCiPaths(["internal/router/router.go"]);
    assert.equal(buckets.go, true);
    assert.equal(buckets.linux, true);
    assert.equal(buckets.darwin, true);
    assert.equal(buckets.packaging, false);
    assert.equal(buckets.docs, false);
    assert.equal(buckets.gui, false);
    assert.equal(needsCiLocal(buckets), true);
    assert.equal(scopeEnvValue(buckets), "go,privacy");
  });

  test("package.json is packaging, not a Go suite", () => {
    const buckets = classifyPrCiPaths(["package.json"]);
    assert.equal(buckets.packaging, true);
    assert.equal(buckets.go, false);
    assert.equal(buckets.linux, true);
    assert.equal(buckets.darwin, false);
  });

  test("local CI script changes prove Go/WSL/Darwin, not GUI or keyring", () => {
    const buckets = classifyPrCiPaths(["scripts/local-pr.ps1"]);
    assert.equal(buckets.full, false);
    assert.equal(buckets.go, true);
    assert.equal(buckets.linux, true);
    assert.equal(buckets.darwin, true);
    assert.equal(buckets.automation, true);
    assert.equal(buckets.privacy, true);
    assert.equal(buckets.gui, false);
    assert.equal(buckets.docs, false);
    assert.equal(buckets.keyring, false);
    assert.equal(buckets.packaging, false);
    assert.equal(scopeEnvValue(buckets), "go,automation,privacy");
  });

  test("CI-script plus docs still skips the dashboard", () => {
    const buckets = classifyPrCiPaths([
      "scripts/local-pr.ps1",
      "docs/src/content/docs/contributing.md",
    ]);
    assert.equal(buckets.docs, true);
    assert.equal(buckets.gui, false);
    assert.equal(buckets.full, false);
  });

  test("unknown paths force a full run", () => {
    const buckets = classifyPrCiPaths(["unexpected/tool.py"]);
    assert.equal(buckets.full, true);
  });

  test("root agent docs are ignored", () => {
    const buckets = classifyPrCiPaths(["AGENTS.md", "MAINTAINERS.md"]);
    assert.equal(buckets.full, false);
    assert.equal(buckets.docs, false);
    assert.equal(needsCiLocal(buckets), false);
  });

  test("forceFull enables every gate", () => {
    const buckets = classifyPrCiPaths(["docs/x.md"], true);
    assert.equal(buckets.full, true);
    assert.equal(buckets.go, true);
  });
});
