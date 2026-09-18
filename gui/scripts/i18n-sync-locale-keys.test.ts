import assert from "node:assert/strict";
import { readFile, stat } from "node:fs/promises";
import { spawnSync } from "node:child_process";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const guiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const overridesPath = path.join(guiRoot, "src", "i18n", "locale-overrides.ts");
const syncPath = path.join(guiRoot, "scripts", "sync-locale-keys.ts");

test("locale sync is idempotent and does not rewrite override source", async () => {
  const before = await readFile(overridesPath, "utf8");
  const beforeStat = await stat(overridesPath);
  const result = spawnSync(process.execPath, ["--experimental-strip-types", syncPath], {
    cwd: guiRoot,
    encoding: "utf8",
  });
  assert.equal(result.status, 0, result.stderr || result.stdout);
  assert.match(result.stdout, /no extra override keys/);
  assert.match(result.stdout, /de: \d+ overrides/);
  // This test intentionally compares file contents and metadata before/after a local child process.
  // codeql[js/file-system-race]
  const after = await readFile(overridesPath, "utf8");
  const afterStat = await stat(overridesPath);
  assert.equal(after, before);
  assert.equal(afterStat.mtimeMs, beforeStat.mtimeMs);
});
