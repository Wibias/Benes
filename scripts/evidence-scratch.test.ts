import assert from "node:assert/strict";
import { access, mkdir, readFile, symlink, writeFile } from "node:fs/promises";
import path from "node:path";
import { describe, test } from "node:test";

import {
  createEvidenceScratch,
  protectedUserProfilePaths,
  removeEvidenceScratch,
  validateEvidenceScratchForCleanup,
} from "./evidence-scratch.ts";
import { createTestTempRoot } from "./test-temp-root.ts";

async function expectRejected(fn: () => Promise<unknown>, pattern: RegExp): Promise<void> {
  await assert.rejects(fn, pattern);
}

describe("evidence scratch safety", () => {
  test("protects the user-profile container as well as the profile", () => {
    const profile = path.join(path.sep, "Users", "ws");
    const protectedPaths = protectedUserProfilePaths(profile);

    assert.equal(protectedPaths.includes(path.resolve(profile)), true);
    assert.equal(protectedPaths.includes(path.dirname(path.resolve(profile))), true);
  });

  test("creates a UUID-owned run directory with a matching marker", async () => {
    const parent = await createTestTempRoot("benes-evidence-parent-");
    const root = path.join(parent, "scratch");

    const scratch = await createEvidenceScratch(root);
    const marker = JSON.parse(await readFile(scratch.markerPath, "utf8"));

    assert.equal(scratch.root, path.resolve(root));
    assert.equal(path.dirname(scratch.path), scratch.root);
    assert.match(scratch.runId, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i);
    assert.deepEqual(marker, {
      version: 1,
      runId: scratch.runId,
      root: scratch.root,
      target: scratch.path,
    });
  });

  test("rejects the scratch root itself", async () => {
    const parent = await createTestTempRoot("benes-evidence-root-");
    const root = path.join(parent, "scratch");
    await mkdir(root);

    await expectRejected(
      () => validateEvidenceScratchForCleanup(root, root),
      /scratch root itself/i,
    );
  });

  test("rejects traversal outside the scratch root", async () => {
    const parent = await createTestTempRoot("benes-evidence-traversal-");
    const root = path.join(parent, "scratch");
    await mkdir(root);
    const outside = path.join(root, "..", "outside");
    await mkdir(path.resolve(outside));

    await expectRejected(
      () => validateEvidenceScratchForCleanup(root, outside),
      /outside scratch root/i,
    );
  });

  test("rejects a UUID target nested below a scratch child", async () => {
    const parent = await createTestTempRoot("benes-evidence-deep-");
    const root = path.join(parent, "scratch");
    const runId = "f507dca6-465a-4c46-b27d-56d70bd7dc8d";
    const target = path.join(root, "nested", runId);
    await mkdir(target, { recursive: true });
    await writeFile(path.join(target, ".benes-evidence-owner.json"), JSON.stringify({
      version: 1,
      runId,
      root: path.resolve(root),
      target: path.resolve(target),
    }));

    await expectRejected(
      () => validateEvidenceScratchForCleanup(root, target),
      /direct child/i,
    );
  });

  test("rejects creating scratch inside an additional protected profile before writing", async () => {
    const parent = await createTestTempRoot("benes-evidence-create-protected-");
    const profile = path.join(parent, "Users", "ws");
    const root = path.join(profile, "scratch");
    await mkdir(profile, { recursive: true });

    await expectRejected(
      () => createEvidenceScratch(root, { additionalProtectedPaths: [profile] }),
      /protected path/i,
    );

    await assert.rejects(access(root));
  });

  test("rejects a scratch root through a symlink parent before creating outside state", async (t) => {
    const parent = await createTestTempRoot("benes-evidence-root-link-");
    const outside = path.join(parent, "outside");
    const link = path.join(parent, "link");
    await mkdir(outside);
    try {
      await symlink(outside, link, process.platform === "win32" ? "junction" : "dir");
    } catch (error) {
      t.skip(`symlink/junction unavailable: ${String(error)}`);
      return;
    }

    const root = path.join(link, "scratch");
    await expectRejected(
      () => createEvidenceScratch(root),
      /symlink|reparse/i,
    );
    await assert.rejects(access(path.join(outside, "scratch")));
  });

  test("rejects a target inside a protected profile path", async () => {
    const parent = await createTestTempRoot("benes-evidence-protected-");
    const profile = path.join(parent, "Users", "ws");
    const root = path.join(profile, "scratch");
    const runId = "1f5aee4c-cd7f-4a34-a1b8-f54518f8132a";
    const target = path.join(root, runId);
    await mkdir(target, { recursive: true });
    await writeFile(path.join(target, ".benes-evidence-owner.json"), JSON.stringify({
      version: 1,
      runId,
      root: path.resolve(root),
      target: path.resolve(target),
    }));

    await expectRejected(
      () => validateEvidenceScratchForCleanup(root, target, { additionalProtectedPaths: [profile] }),
      /protected path/i,
    );
  });

  test("rejects an unmarked run directory", async () => {
    const parent = await createTestTempRoot("benes-evidence-unmarked-");
    const root = path.join(parent, "scratch");
    const target = path.join(root, "24561dd6-6afb-485c-87cf-cc25e5dd4473");
    await mkdir(target, { recursive: true });

    await expectRejected(
      () => validateEvidenceScratchForCleanup(root, target),
      /ownership marker/i,
    );
  });

  test("rejects marker run-id mismatch", async () => {
    const parent = await createTestTempRoot("benes-evidence-runid-");
    const root = path.join(parent, "scratch");
    const target = path.join(root, "5d991c28-d85b-4cba-afc0-b3a621a40162");
    await mkdir(target, { recursive: true });
    await writeFile(path.join(target, ".benes-evidence-owner.json"), JSON.stringify({
      version: 1,
      runId: "d21a7a57-343c-42d4-896f-ddef89778f80",
      root: path.resolve(root),
      target: path.resolve(target),
    }));

    await expectRejected(
      () => validateEvidenceScratchForCleanup(root, target),
      /run id/i,
    );
  });

  test("rejects marker root mismatch", async () => {
    const parent = await createTestTempRoot("benes-evidence-marker-root-");
    const root = path.join(parent, "scratch");
    const runId = "98bec6a8-19ab-4e31-bd99-27681899919c";
    const target = path.join(root, runId);
    await mkdir(target, { recursive: true });
    await writeFile(path.join(target, ".benes-evidence-owner.json"), JSON.stringify({
      version: 1,
      runId,
      root: path.join(parent, "other-root"),
      target: path.resolve(target),
    }));

    await expectRejected(
      () => validateEvidenceScratchForCleanup(root, target),
      /marker root/i,
    );
  });

  test("rejects a symlink escape inside the scratch root", async (t) => {
    const parent = await createTestTempRoot("benes-evidence-link-");
    const root = path.join(parent, "scratch");
    const outside = path.join(parent, "outside");
    const runId = "68f078b4-6dd0-4bbb-a248-452ab202f68a";
    await mkdir(root);
    await mkdir(outside);
    const target = path.join(root, runId);
    try {
      await symlink(outside, target, process.platform === "win32" ? "junction" : "dir");
    } catch (error) {
      t.skip(`symlink/junction unavailable: ${String(error)}`);
      return;
    }

    await expectRejected(
      () => validateEvidenceScratchForCleanup(root, target),
      /symlink|reparse|outside scratch root/i,
    );
  });

  test("rejects a symlink escape nested inside an otherwise valid run", async (t) => {
    const parent = await createTestTempRoot("benes-evidence-nested-link-");
    const root = path.join(parent, "scratch");
    const outside = path.join(parent, "outside");
    await mkdir(outside);
    await writeFile(path.join(outside, "keep.txt"), "keep\n");
    const scratch = await createEvidenceScratch(root);
    const escape = path.join(scratch.path, "escape");
    try {
      await symlink(outside, escape, process.platform === "win32" ? "junction" : "dir");
    } catch (error) {
      t.skip(`symlink/junction unavailable: ${String(error)}`);
      return;
    }

    await expectRejected(
      () => removeEvidenceScratch(root, scratch.path),
      /symlink|reparse/i,
    );
    await access(scratch.path);
    await access(path.join(outside, "keep.txt"));
  });

  test("removes only a valid marked run directory and leaves siblings intact", async () => {
    const parent = await createTestTempRoot("benes-evidence-remove-");
    const root = path.join(parent, "scratch");
    const scratch = await createEvidenceScratch(root);
    const sibling = path.join(root, "keep-me");
    await mkdir(sibling);
    await writeFile(path.join(scratch.path, "artifact.txt"), "evidence\n");

    await removeEvidenceScratch(root, scratch.path);

    await assert.rejects(access(scratch.path));
    await access(root);
    await access(sibling);
  });
});
