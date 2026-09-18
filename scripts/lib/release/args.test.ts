import assert from "node:assert/strict";
import { describe, test } from "node:test";
import { parseReleaseArgv } from "./args.ts";
import { ReleaseError } from "./semver.ts";

const SHA = "0123456789abcdef0123456789abcdef01234567";

describe("parseReleaseArgv", () => {
  test("parses a backward-compatible cut invocation", () => {
    assert.deepEqual(parseReleaseArgv(["4.2.0", "--tag", "latest"]), {
      kind: "cut",
      version: "4.2.0",
      distTag: "latest",
      publish: false,
      planOnly: false,
    });
    assert.equal(parseReleaseArgv(["cut", "4.2.0", "--publish"]).kind, "cut");
    assert.equal(parseReleaseArgv(["watch"]).kind, "watch");
  });

  test("publish reads flags or environment, defaulting to dry-run", () => {
    const fromFlags = parseReleaseArgv([
      "publish",
      "--version",
      "4.2.0",
      "--tag",
      "latest",
      "--expected-sha",
      SHA,
    ]);
    assert.deepEqual(fromFlags, {
      kind: "publish",
      version: "4.2.0",
      distTag: "latest",
      expectedSha: SHA,
      dryRun: true,
    });
    const fromEnv = parseReleaseArgv(["publish"], {
      RELEASE_VERSION: "4.2.0-preview.1",
      NPM_DIST_TAG: "preview",
      EXPECTED_SHA: SHA,
      RELEASE_DRY_RUN: "false",
    });
    assert.equal(fromEnv.kind, "publish");
    if (fromEnv.kind === "publish") {
      assert.equal(fromEnv.dryRun, false);
      assert.equal(fromEnv.distTag, "preview");
    }
  });

  test("rejects an injected version, build metadata, or abbreviated SHA", () => {
    assert.throws(() => parseReleaseArgv(["1.0.0;rm"]), ReleaseError);
    assert.throws(() => parseReleaseArgv(["4.2.0+build.9"]), ReleaseError);
    assert.throws(() => parseReleaseArgv(["1.2.03"]), ReleaseError);
    assert.throws(
      () =>
        parseReleaseArgv([
          "publish",
          "--version",
          "4.2.0",
          "--tag",
          "latest",
          "--expected-sha",
          "abc",
        ]),
      ReleaseError,
    );
  });
});
