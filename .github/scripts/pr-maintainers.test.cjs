"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const { readMaintainerGitHubLogins } = require("./pr-maintainers.cjs");

const SYNTHETIC = [
  "# Maintainers",
  "",
  "## Current maintainers",
  "",
  "| GitHub account | Project role | Responsibilities |",
  "| --- | --- | --- |",
  "| [@Wibias](https://github.com/Wibias) | Project owner | x |",
  "| [@reviewer](https://github.com/reviewer) | Maintainer | y |",
  "",
  "## Review and merge policy",
  "",
  "- [@alumni](https://github.com/alumni) stepped down.",
  "- [@reviewer](https://github.com/reviewer) is mentioned again here.",
].join("\n");

describe("MAINTAINERS.md GitHub identities", () => {
  it("reads only the Current maintainers H2 and skips later sections", () => {
    assert.deepEqual(readMaintainerGitHubLogins(SYNTHETIC), ["Wibias", "reviewer"]);
  });

  it("returns no logins when that H2 is absent", () => {
    assert.deepEqual(
      readMaintainerGitHubLogins("- [@only](https://github.com/only) is listed."),
      [],
    );
  });

  it("ignores a ### heading and prose that merely mentions the H2", () => {
    assert.deepEqual(
      readMaintainerGitHubLogins(
        ["### Current maintainers", "| [@nested](https://github.com/nested) | x |"].join(
          "\n",
        ),
      ),
      [],
    );
    assert.deepEqual(
      readMaintainerGitHubLogins(
        [
          "See the ## Current maintainers section below.",
          "| [@prose](https://github.com/prose) | x |",
        ].join("\n"),
      ),
      [],
    );
  });

  it("accepts CRLF plus trailing spaces on the H2", () => {
    const crlf = [
      "## Current maintainers \t",
      "| [@crlf](https://github.com/crlf) | x |",
      "## Changelog",
      "| [@alumni](https://github.com/alumni) | y |",
    ].join("\r\n");
    assert.deepEqual(readMaintainerGitHubLogins(crlf), ["crlf"]);
  });

  it("requires the mention and GitHub URL login to match", () => {
    assert.deepEqual(
      readMaintainerGitHubLogins(
        [
          "## Current maintainers",
          "| [@Wibias](https://github.com/someone-else) | owner | x |",
          "| [@ok](https://github.com/ok/) | maintainer | y |",
        ].join("\n"),
      ),
      ["ok"],
    );
  });

  it("collapses duplicates and treats empty input as empty", () => {
    assert.deepEqual(readMaintainerGitHubLogins(""), []);
    assert.deepEqual(readMaintainerGitHubLogins(undefined), []);
    assert.deepEqual(
      readMaintainerGitHubLogins(
        [
          "## Current maintainers",
          "| [@dup](https://github.com/dup) | x |",
          "| [@dup](https://github.com/Dup) | y |",
        ].join("\n"),
      ),
      ["dup"],
    );
  });

  it("reads the live MAINTAINERS.md checkout", () => {
    const text = fs.readFileSync(
      path.join(__dirname, "..", "..", "MAINTAINERS.md"),
      "utf8",
    );
    const logins = readMaintainerGitHubLogins(text);
    assert.equal(logins.includes("Wibias"), true);
    assert.equal(logins.includes("alumni"), false);
  });
});
