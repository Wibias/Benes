import assert from "node:assert/strict";
import { access, mkdir, symlink } from "node:fs/promises";
import path from "node:path";
import { test } from "node:test";

import { acquireBrowserAutomationProfile } from "./evidence-browser-runtime.ts";
import { createTestTempRoot } from "./test-temp-root.ts";

async function createDirectoryLink(target: string, link: string): Promise<void> {
  await symlink(target, link, process.platform === "win32" ? "junction" : "dir");
}

test("persistent browser profile root rejects a symlink/reparse escape into a protected path before writing", async (t) => {
  const parent = await createTestTempRoot("benes-browser-profile-root-link-");
  const protectedTarget = path.join(parent, "protected");
  const link = path.join(parent, "automation-link");
  await mkdir(protectedTarget);

  try {
    await createDirectoryLink(protectedTarget, link);
  } catch (error) {
    t.skip(`symlink/junction unavailable: ${String(error)}`);
    return;
  }

  const root = path.join(link, "automation");
  await assert.rejects(
    acquireBrowserAutomationProfile({
      root,
      runId: "671c066b-1d41-4f74-bdd2-e16f842cf524",
      additionalProtectedPaths: [protectedTarget],
    }),
    /symlink|reparse|protected/i,
  );

  await assert.rejects(access(path.join(protectedTarget, "automation")));
});

test("persistent browser profile rejects an existing chrome-user-data symlink/reparse escape", async (t) => {
  const parent = await createTestTempRoot("benes-browser-profile-link-");
  const root = path.join(parent, "automation");
  const outside = path.join(parent, "outside");
  const profilePath = path.join(root, "chrome-user-data");
  await mkdir(root);
  await mkdir(outside);

  try {
    await createDirectoryLink(outside, profilePath);
  } catch (error) {
    t.skip(`symlink/junction unavailable: ${String(error)}`);
    return;
  }

  await assert.rejects(
    acquireBrowserAutomationProfile({
      root,
      runId: "41144a3f-ab83-44f4-856a-6b987ec6a8f5",
    }),
    /symlink|reparse/i,
  );
});
