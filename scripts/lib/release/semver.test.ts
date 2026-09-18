import assert from "node:assert/strict";
import { describe, test } from "node:test";
import {
  assertVersionForChannel,
  compareReleaseVersions,
  distTagForChannel,
  gitTagForVersion,
  parsePublishVersion,
  parseSemver,
  releaseChannel,
  ReleaseError,
} from "./semver.ts";

describe("parseSemver", () => {
  test("accepts a stable version", () => {
    assert.deepEqual(parseSemver("4.2.0"), {
      text: "4.2.0",
      major: 4,
      minor: 2,
      patch: 0,
      pre: null,
    });
  });

  test("accepts a preview version", () => {
    assert.deepEqual(parseSemver("4.2.0-preview.3"), {
      text: "4.2.0-preview.3",
      major: 4,
      minor: 2,
      patch: 0,
      pre: ["preview", "3"],
    });
  });

  test("understands build metadata for comparison only", () => {
    assert.equal(parseSemver("4.2.0+build.9").pre, null);
    assert.equal(compareReleaseVersions("4.2.0+aaa", "4.2.0+zzz"), 0);
  });

  test("rejects leading zeroes in core and numeric prerelease identifiers", () => {
    assert.throws(() => parseSemver("01.2.3"), ReleaseError);
    assert.throws(() => parseSemver("1.02.3"), ReleaseError);
    assert.throws(() => parseSemver("1.2.03"), ReleaseError);
    assert.throws(() => parseSemver("1.2.3-preview.01"), ReleaseError);
  });

  test("rejects an empty or malformed version", () => {
    assert.throws(() => parseSemver(""), ReleaseError);
    assert.throws(() => parseSemver("v4.2.0"), ReleaseError);
    assert.throws(() => parseSemver("4.2"), ReleaseError);
    assert.throws(() => parseSemver("4.2.0;rm"), ReleaseError);
  });
});

describe("parsePublishVersion", () => {
  test("accepts the Benes publication grammar", () => {
    assert.equal(parsePublishVersion("4.2.0").text, "4.2.0");
    assert.equal(parsePublishVersion("4.2.0-preview.1.2").text, "4.2.0-preview.1.2");
  });

  test("rejects build metadata and leading zeroes", () => {
    assert.throws(() => parsePublishVersion("4.2.0+build.9"), ReleaseError);
    assert.throws(() => parsePublishVersion("01.2.3"), ReleaseError);
    assert.throws(() => parsePublishVersion("1.02.3"), ReleaseError);
    assert.throws(() => parsePublishVersion("1.2.03"), ReleaseError);
    assert.throws(() => parsePublishVersion("1.2.3-preview.01"), ReleaseError);
  });
});

describe("compareReleaseVersions", () => {
  test("orders numeric cores", () => {
    assert.ok(compareReleaseVersions("4.9.1", "4.10.0") < 0);
    assert.ok(compareReleaseVersions("10.0.0", "9.9.9") > 0);
  });

  test("ranks a stable after prereleases of the same core", () => {
    assert.ok(compareReleaseVersions("4.2.0-preview.1", "4.2.0") < 0);
    assert.ok(compareReleaseVersions("4.2.0", "4.2.0-preview.9") > 0);
  });

  test("compares numeric prerelease identifiers by number", () => {
    assert.ok(compareReleaseVersions("1.0.0-preview.2", "1.0.0-preview.10") < 0);
  });

  test("ranks numeric prerelease identifiers below non-numeric", () => {
    assert.ok(compareReleaseVersions("1.0.0-1", "1.0.0-alpha") < 0);
  });

  test("treats extra prerelease identifiers as greater", () => {
    assert.ok(compareReleaseVersions("1.0.0-preview.1", "1.0.0-preview.1.1") < 0);
  });
});

describe("releaseChannel", () => {
  test("stable and preview channels", () => {
    assert.equal(releaseChannel("4.2.0"), "stable");
    assert.equal(releaseChannel("4.2.0-preview.1"), "preview");
    assert.equal(distTagForChannel("stable"), "latest");
    assert.equal(distTagForChannel("preview"), "preview");
    assert.equal(gitTagForVersion("4.2.0-preview.1"), "v4.2.0-preview.1");
  });

  test("rejects a non-preview prerelease and build-metadata publication", () => {
    assert.throws(() => releaseChannel("4.2.0-rc.1"), ReleaseError);
    assert.throws(() => releaseChannel("4.2.0+build.9"), ReleaseError);
    assert.throws(() => assertVersionForChannel("4.2.0-preview.1", "stable"), ReleaseError);
    assert.throws(() => assertVersionForChannel("4.2.0", "preview"), ReleaseError);
  });
});

describe("comparison boundaries", () => {
  test("accepts an all-zero core and compares it with itself as equal", () => {
    assert.equal(parseSemver("0.0.0").text, "0.0.0");
    assert.equal(compareReleaseVersions("0.0.0", "0.0.0"), 0);
  });

  test("accepts the literal zero as a numeric prerelease identifier", () => {
    assert.equal(parseSemver("1.2.3-preview.0").text, "1.2.3-preview.0");
  });

  test("accepts a mixed alphanumeric prerelease tail", () => {
    assert.deepEqual(parseSemver("1.0.0-preview.1.alpha").pre, ["preview", "1", "alpha"]);
    assert.ok(compareReleaseVersions("1.0.0-preview.1", "1.0.0-preview.1.alpha") < 0);
  });

  test("preserves the frozen Number-based behavior for oversized numeric identifiers", () => {
    // The frozen implementation converted core components to Number before
    // comparison, so these two distinct core values collapse to the same
    // IEEE-754 value and compare as equal.
    assert.equal(compareReleaseVersions("9007199254740993.0.0", "9007199254740992.0.0"), 0);
    // Frozen prerelease comparison falls back to the original identifier text
    // after the numeric Numbers collapse, so this ordering remains positive.
    assert.ok(compareReleaseVersions("1.0.0-preview.9007199254740993", "1.0.0-preview.9007199254740992") > 0);
  });

  test("ignores build metadata when ranking", () => {
    assert.equal(compareReleaseVersions("4.2.0+build.1", "4.2.0+build.2"), 0);
    assert.equal(parseSemver("4.2.0-preview.3+build.7").text, "4.2.0-preview.3");
  });

  test("rejects malformed and empty prerelease components", () => {
    assert.throws(() => parseSemver("1.2.3-"), ReleaseError);
    assert.throws(() => parseSemver("1.2.3-a..b"), ReleaseError);
    assert.throws(() => parseSemver("1.2.3-."), ReleaseError);
  });

  test("keeps a non-preview prerelease out of publication, not out of comparison", () => {
    assert.equal(parsePublishVersion("4.2.0-rc.1").text, "4.2.0-rc.1");
    assert.equal(parseSemver("4.2.0-rc.1").pre?.join("."), "rc.1");
    assert.throws(() => releaseChannel("4.2.0-rc.1"), ReleaseError);
  });
});
