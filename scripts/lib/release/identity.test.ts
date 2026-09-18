import assert from "node:assert/strict";
import { describe, test } from "node:test";
import {
  assertPackageVersion,
  parseFullSha,
  parseRepository,
  resolveIdentity,
} from "./identity.ts";
import { ReleaseError } from "./semver.ts";

const SHA = "0123456789abcdef0123456789abcdef01234567";

describe("resolveIdentity", () => {
  test("builds a stable identity on main", () => {
    const identity = resolveIdentity({
      packageName: "benes",
      version: "4.2.0",
      sourceSha: SHA,
      branch: "main",
    });
    assert.deepEqual(identity, {
      packageName: "benes",
      version: "4.2.0",
      gitTag: "v4.2.0",
      distTag: "latest",
      channel: "stable",
      sourceSha: SHA,
      branch: "main",
    });
  });

  test("builds a preview identity on preview", () => {
    const identity = resolveIdentity({
      packageName: "benes",
      version: "4.2.0-preview.2",
      sourceSha: SHA,
      branch: "refs/heads/preview",
      distTag: "preview",
    });
    assert.equal(identity.distTag, "preview");
    assert.equal(identity.branch, "preview");
    assert.equal(identity.gitTag, "v4.2.0-preview.2");
  });

  test("rejects dev, abbreviated SHA, and dist-tag mismatch", () => {
    assert.throws(
      () =>
        resolveIdentity({
          packageName: "benes",
          version: "4.2.0",
          sourceSha: SHA,
          branch: "dev",
        }),
      ReleaseError,
    );
    assert.throws(() => parseFullSha("0123456789abcdef"), ReleaseError);
    assert.throws(() => parseFullSha(SHA.toUpperCase()), ReleaseError);
    assert.throws(
      () =>
        resolveIdentity({
          packageName: "benes",
          version: "4.2.0",
          sourceSha: SHA,
          branch: "main",
          distTag: "preview",
        }),
      ReleaseError,
    );
  });

  test("rejects a preview version on main and a stable version on preview", () => {
    assert.throws(
      () =>
        resolveIdentity({
          packageName: "benes",
          version: "4.2.0-preview.1",
          sourceSha: SHA,
          branch: "main",
        }),
      ReleaseError,
    );
    assert.throws(
      () =>
        resolveIdentity({
          packageName: "benes",
          version: "4.2.0",
          sourceSha: SHA,
          branch: "preview",
        }),
      ReleaseError,
    );
  });

  test("rejects build metadata and leading zeroes as a publish identity", () => {
    assert.throws(
      () =>
        resolveIdentity({
          packageName: "benes",
          version: "4.2.0+build.9",
          sourceSha: SHA,
          branch: "main",
        }),
      ReleaseError,
    );
    assert.throws(
      () =>
        resolveIdentity({
          packageName: "benes",
          version: "01.2.3",
          sourceSha: SHA,
          branch: "main",
        }),
      ReleaseError,
    );
    assert.throws(
      () =>
        resolveIdentity({
          packageName: "benes",
          version: "4.2.0-preview.01",
          sourceSha: SHA,
          branch: "preview",
        }),
      ReleaseError,
    );
  });

  test("rejects tag-injection versions and odd package names", () => {
    assert.throws(
      () =>
        resolveIdentity({
          packageName: "benes",
          version: "4.2.0;rm",
          sourceSha: SHA,
          branch: "main",
        }),
      ReleaseError,
    );
    assert.throws(
      () =>
        resolveIdentity({
          packageName: "../evil",
          version: "4.2.0",
          sourceSha: SHA,
          branch: "main",
        }),
      ReleaseError,
    );
    assert.throws(() => parseRepository("not-a-repo"), ReleaseError);
  });

  test("requires package.json to already match on the publish path", () => {
    const identity = resolveIdentity({
      packageName: "benes",
      version: "4.2.0",
      sourceSha: SHA,
      branch: "main",
    });
    assertPackageVersion(identity, "4.2.0");
    assert.throws(() => assertPackageVersion(identity, "4.1.9"), ReleaseError);
  });
});
