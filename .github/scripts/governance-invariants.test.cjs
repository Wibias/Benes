"use strict";

const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const ROOT = path.join(__dirname, "..", "..");

function read(rel) {
  return fs.readFileSync(path.join(ROOT, rel), "utf8");
}

describe("CODEOWNERS", () => {
  const owners = read(".github/CODEOWNERS");

  it("requests review on credential, OAuth, and release surfaces", () => {
    for (const pattern of [
      "/internal/oauth/",
      "/internal/credentials/",
      "/internal/credentialpool/",
      "/internal/codexauth/",
      "/internal/server/",
      "/internal/providers/",
      "/.github/",
      "/scripts/release.ts",
      "/package.json",
    ]) {
      assert.match(owners, new RegExp(`^${pattern.replace(/[/.]/g, "\\$&")}\\s+@Wibias`, "m"));
    }
  });
});

describe("GitHub generate-notes config", () => {
  const release = read(".github/release.yml");

  it("keeps skip-changelog out of generated notes", () => {
    assert.match(release, /skip-changelog/);
    assert.match(release, /changelog:/);
    assert.match(release, /exclude:/);
  });
});

describe("issue automation least privilege", () => {
  const quality = read(".github/workflows/enforce-issue-quality.yml");
  const tests = read(".github/workflows/issue-quality-tests.yml");

  it("keeps the quality workflow on default-branch scripts and issues:write only", () => {
    assert.match(quality, /^permissions:\s*\{\}\s*$/m);
    assert.match(quality, /contents:\s*read/);
    assert.match(quality, /issues:\s*write/);
    assert.doesNotMatch(quality, /pull-requests:\s*write/);
    assert.match(quality, /ref:\s*\$\{\{\s*github\.event\.repository\.default_branch\s*\}\}/);
    assert.match(quality, /persist-credentials:\s*false/);
    assert.match(quality, /actions\/checkout@[0-9a-f]{40}/);
    assert.doesNotMatch(quality, /uses:\s+\S+@(?:v\d+|main|master)\b/);
    assert.match(quality, /issue-quality-run\.cjs/);
  });

  it("runs automation tests with contents:read only", () => {
    assert.match(tests, /^permissions:\n {2}contents: read\s*$/m);
    assert.doesNotMatch(tests, /issues:\s*write/);
    assert.match(tests, /persist-credentials:\s*false/);
    assert.match(tests, /node \.github\/scripts\/run-automation-tests\.cjs/);
  });
});
