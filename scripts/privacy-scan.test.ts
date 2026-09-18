import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { describe, it } from "node:test";
import {
  formatFinding,
  scanText,
  scanTrackedFiles,
  shouldScan,
} from "./privacy-scan.ts";

describe("shouldScan", () => {
  it("scans tracked text files and skips lockfiles and generated GUI output", () => {
    assert.equal(shouldScan("scripts/privacy-scan.ts"), true);
    assert.equal(shouldScan("package-lock.json"), false);
    assert.equal(shouldScan("gui/dist/index.html"), false);
    assert.equal(shouldScan("internal/router/router.go"), false);
  });
});

describe("scanText", () => {
  it("accepts a clean fixture", () => {
    assert.deepEqual(
      scanText("README.md", "Local Go proxy on 127.0.0.1:23100.\n"),
      [],
    );
  });

  it("flags secret-like material without returning the raw secret", () => {
    const token = ["sk-", "abcdefghijklmnopqrstuvwxyz012345"].join("");
    const bearer = ["access_token_value_not_a_fixture", "abcdefgh"].join("_");
    const email = ["nia", "mail.invalid"].join("@");
    const home = ["/Users/", "nia", "/.benes/config.json"].join("");
    const findings = scanText(
      "scripts/example.ts",
      [
        `const key = '${token}';`,
        `Authorization: Bearer ${bearer}`,
        `email ${email}`,
        `path ${home}`,
      ].join("\n"),
    );
    assert.deepEqual(
      findings.map((finding) => finding.kind).sort(),
      ["bearer-token", "email", "home-path", "token-looking"],
    );
    for (const finding of findings) {
      assert.equal("value" in finding, false);
      assert.match(formatFinding(finding), /\[redacted\]$/);
      assert.doesNotMatch(formatFinding(finding), new RegExp(token));
      assert.doesNotMatch(formatFinding(finding), new RegExp(email.replace(".", "\\.")));
    }
  });

  it("allows documented non-secret fixtures", () => {
    const testHome = ["/Users/", "example", "/repo/"].join("");
    const docsHome = ["/Users/", "example", "/.benes/"].join("");
    assert.deepEqual(
      scanText(
        "tests/auth_fixture.ts",
        ["sk-test-1234abc", "user@example.test", testHome].join("\n"),
      ),
      [],
    );
    assert.deepEqual(
      scanText(
        "docs/src/content/docs/start/install.md",
        `Install under ${docsHome}\n`,
      ),
      [],
    );
  });
});

describe("scanTrackedFiles", () => {
  it("applies tests/ policy to logical git paths even when the repo lives at an absolute temp path", () => {
    const repo = mkdtempSync(path.join(tmpdir(), "benes-privacy-repo-"));
    mkdirSync(path.join(repo, "tests"));
    mkdirSync(path.join(repo, "scripts"));
    const allowedToken = ["sk-test-", "1234abc"].join("");
    const leakToken = ["sk-", "abcdefghijklmnopqrstuvwxyz012345"].join("");
    writeFileSync(path.join(repo, "tests", "auth_fixture.ts"), `export const probe = "${allowedToken}";\n`);
    writeFileSync(path.join(repo, "scripts", "example.ts"), `export const key = "${leakToken}";\n`);
    const git = (args) => {
      const result = spawnSync("git", args, { cwd: repo, encoding: "utf8" });
      assert.equal(result.status, 0, result.stderr);
    };
    git(["init"]);
    git(["add", "tests/auth_fixture.ts", "scripts/example.ts"]);

    const findings = scanTrackedFiles(repo);
    assert.equal(
      findings.some((finding) => finding.file === "tests/auth_fixture.ts"),
      false,
    );
    const leak = findings.filter((finding) => finding.file === "scripts/example.ts");
    assert.equal(leak.length > 0, true);
    for (const finding of leak) {
      assert.equal(finding.file.includes(repo), false);
      assert.equal(path.isAbsolute(finding.file), false);
      assert.equal("value" in finding, false);
      assert.doesNotMatch(formatFinding(finding), new RegExp(leakToken));
    }
  });
});
