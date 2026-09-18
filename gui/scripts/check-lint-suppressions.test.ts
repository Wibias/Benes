import assert from "node:assert/strict";
import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { compareSuppressions, scanSuppressions } from "./check-lint-suppressions.ts";

async function withFixture(files, run) {
  const root = await mkdtemp(path.join(os.tmpdir(), "benes-lint-suppressions-"));
  try {
    for (const [relativePath, content] of Object.entries(files)) {
      const target = path.join(root, relativePath);
      await mkdir(path.dirname(target), { recursive: true });
      await writeFile(target, content, "utf8");
    }
    await run(root);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
}

function approved(pathname, directive, count = 1) {
  return { path: pathname, directive, count };
}

test("normal source and generated i18n source pass with an empty baseline", async () => {
  await withFixture({
    "src/clean.ts": "export const value = 1;\n",
    "src/i18n/generated.ts": "// eslint-disable no-console\nconsole.log('generated');\n",
  }, async (root) => {
    const actual = await scanSuppressions(root);
    assert.deepEqual(actual, []);
    assert.deepEqual(compareSuppressions(actual, []), []);
  });
});

test("a new suppression fails", async () => {
  await withFixture({
    "src/new.ts": "// eslint-disable-next-line no-console\nconsole.log('x');\n",
  }, async (root) => {
    const actual = await scanSuppressions(root);
    const errors = compareSuppressions(actual, []);
    assert.equal(errors.length, 1);
    assert.match(errors[0], /unapproved suppression/i);
  });
});

test("changing an approved suppression fails", async () => {
  await withFixture({
    "src/example.ts": "// eslint-disable-next-line eqeqeq\nexport const same = 1 == '1';\n",
  }, async (root) => {
    const actual = await scanSuppressions(root);
    const errors = compareSuppressions(actual, [
      approved("src/example.ts", "eslint-disable-next-line no-console"),
    ]);
    assert.equal(errors.length, 2);
    assert.ok(errors.some((error) => /unapproved suppression/i.test(error)));
    assert.ok(errors.some((error) => /stale baseline/i.test(error)));
  });
});

test("removing an approved suppression makes the baseline stale", async () => {
  await withFixture({
    "src/example.ts": "export const value = 1;\n",
  }, async (root) => {
    const actual = await scanSuppressions(root);
    const errors = compareSuppressions(actual, [
      approved("src/example.ts", "eslint-disable-next-line no-console"),
    ]);
    assert.equal(errors.length, 1);
    assert.match(errors[0], /stale baseline/i);
  });
});

test("moving an approved suppression without changing it passes", async () => {
  await withFixture({
    "src/example.ts": "\n\n// eslint-disable-next-line no-console\nconsole.log('x');\n",
  }, async (root) => {
    const actual = await scanSuppressions(root);
    const errors = compareSuppressions(actual, [
      approved("src/example.ts", "eslint-disable-next-line no-console"),
    ]);
    assert.deepEqual(errors, []);
  });
});

test("increasing suppression multiplicity fails", async () => {
  await withFixture({
    "src/example.ts": [
      "// eslint-disable-next-line no-console",
      "console.log('a');",
      "// eslint-disable-next-line no-console",
      "console.log('b');",
      "",
    ].join("\n"),
  }, async (root) => {
    const actual = await scanSuppressions(root);
    const errors = compareSuppressions(actual, [
      approved("src/example.ts", "eslint-disable-next-line no-console"),
    ]);
    assert.equal(errors.length, 1);
    assert.match(errors[0], /count 2.*approved 1/i);
  });
});

test("TypeScript escape hatches are tracked too", async () => {
  await withFixture({
    "src/example.ts": [
      "// @ts-ignore legacy",
      "const first = missing;",
      "// @ts-expect-error intentional",
      "const second = missingToo;",
      "",
    ].join("\n"),
  }, async (root) => {
    const actual = await scanSuppressions(root);
    assert.deepEqual(actual, [
      approved("src/example.ts", "@ts-expect-error intentional"),
      approved("src/example.ts", "@ts-ignore legacy"),
    ]);
  });
});
