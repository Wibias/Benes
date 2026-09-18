import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { describe, test } from "node:test";
import { fileURLToPath } from "node:url";
import { gitRefAlreadyExists, npmVersionAlreadyOnRegistry } from "./live.ts";

const root = join(dirname(fileURLToPath(import.meta.url)), "..", "..", "..");

function read(rel: string): string {
  return readFileSync(join(root, rel), "utf8");
}

describe("release security surfaces", () => {
  test("does not introduce an npm token path", () => {
    const files = [
      "scripts/release.ts",
      "scripts/lib/release/live.ts",
      "scripts/lib/release/execute.ts",
      ".github/workflows/release.yml",
    ];
    for (const file of files) {
      const body = read(file);
      assert.doesNotMatch(body, /NPM_TOKEN/);
      assert.doesNotMatch(body, /NODE_AUTH_TOKEN\s*:/);
    }
    assert.match(read(".github/workflows/release.yml"), /unset NODE_AUTH_TOKEN/);
  });

  test("publish mutations go through GitHub API tags, not git push of tags", () => {
    assert.match(read("scripts/lib/release/live.ts"), /repos\/\$\{repository\}\/git\/refs/);
    assert.doesNotMatch(read("scripts/lib/release/live.ts"), /git push origin ["']refs\/tags\//);
    assert.doesNotMatch(read(".github/workflows/release.yml"), /git push origin ["']refs\/tags\//);
  });

  test("treats an already-published npm version as retryable rather than a secret-bearing error", () => {
    assert.equal(
      npmVersionAlreadyOnRegistry("You cannot publish over the previously published versions: 4.2.0"),
      true,
    );
    assert.equal(npmVersionAlreadyOnRegistry("EOTP failed"), false);
    assert.equal(gitRefAlreadyExists("Reference already exists"), true);
  });

  test("the workflow stays dispatch-only with OIDC and a serial release group", () => {
    const workflow = read(".github/workflows/release.yml");
    assert.match(workflow, /^\s+workflow_dispatch:\s*$/m);
    assert.doesNotMatch(workflow, /^\s+push:\s*$/m);
    assert.doesNotMatch(workflow, /^\s+pull_request:\s*$/m);
    assert.match(workflow, /^permissions:\s*\{\}\s*$/m);
    assert.match(workflow, /group:\s*release/);
    assert.match(workflow, /cancel-in-progress:\s*false/);
    assert.match(workflow, /id-token:\s*write/);
    assert.match(workflow, /scripts\/release\.ts publish/);
    assert.match(workflow, /need >= 11\.5\.1/);
    assert.match(workflow, /^\s{6}dry-run:\s*$/m);
    assert.match(workflow, /default:\s*true/);
  });
});
