import assert from "node:assert/strict";
import { chmodSync, mkdirSync, mkdtempSync, symlinkSync, writeFileSync, lstatSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { describe, test } from "node:test";
import { applyPublishedModes } from "./prepare-package.ts";

function modeOf(file) {
  return lstatSync(file).mode & 0o777;
}

describe("applyPublishedModes", () => {
  test("makes the CLI launcher executable and leaves the package entry non-executable", () => {
    const root = mkdtempSync(path.join(tmpdir(), "benes-pack-"));
    mkdirSync(path.join(root, "bin"));
    const launcher = path.join(root, "bin", "benes.mjs");
    const entry = path.join(root, "bin", "package-main.mjs");
    writeFileSync(launcher, "#!/usr/bin/env node\n");
    writeFileSync(entry, "export {}\n");
    chmodSync(launcher, 0o644);
    chmodSync(entry, 0o755);

    applyPublishedModes(root);

    if (process.platform === "win32") {
      assert.ok(true, "Windows does not persist POSIX execute bits");
      return;
    }
    assert.equal(modeOf(launcher), 0o755);
    assert.equal(modeOf(entry), 0o644);
  });

  test("normalizes dashboard files and does not follow a symlink", () => {
    const root = mkdtempSync(path.join(tmpdir(), "benes-pack-dist-"));
    const dist = path.join(root, "gui", "dist");
    mkdirSync(dist, { recursive: true });
    const asset = path.join(dist, "index.html");
    writeFileSync(asset, "<html></html>\n");
    chmodSync(asset, 0o755);

    const outside = path.join(root, "outside.txt");
    writeFileSync(outside, "keep\n");
    chmodSync(outside, 0o600);
    const link = path.join(dist, "outside-link");
    try {
      symlinkSync(outside, link);
    } catch {
      // Some Windows agents cannot create symlinks without privilege.
    }

    applyPublishedModes(root);

    if (process.platform === "win32") return;
    assert.equal(modeOf(asset), 0o644);
    assert.equal(modeOf(outside), 0o600);
  });

  test("does not chmod a file reached only through a directory symlink under gui/dist", () => {
    const root = mkdtempSync(path.join(tmpdir(), "benes-pack-dirlink-"));
    const dist = path.join(root, "gui", "dist");
    mkdirSync(dist, { recursive: true });
    const asset = path.join(dist, "index.html");
    writeFileSync(asset, "<html></html>\n");
    chmodSync(asset, 0o755);

    const external = path.join(root, "outside-dir");
    mkdirSync(external);
    const secret = path.join(external, "keep.txt");
    writeFileSync(secret, "keep\n");
    chmodSync(secret, 0o600);
    try {
      symlinkSync(external, path.join(dist, "vendor"), "dir");
    } catch {
      if (process.platform === "win32") return;
      throw new Error("could not create directory symlink");
    }

    applyPublishedModes(root);

    if (process.platform === "win32") return;
    assert.equal(modeOf(asset), 0o644);
    assert.equal(modeOf(secret), 0o600);
  });
});
