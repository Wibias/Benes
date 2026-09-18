import assert from "node:assert/strict";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import test from "node:test";

import {
  compareStructuralBaseline,
  diagnosticsFromOxlintJson,
  summarizeStructuralDiagnostics,
} from "./check-structural-baseline.ts";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const guiRoot = path.resolve(scriptDir, "..");
const oxlintBin = path.join(guiRoot, "node_modules", "oxlint", "bin", "oxlint");
const configPath = path.join(guiRoot, ".oxlintrc.json");

async function lintFixture(source) {
  const fixtureDir = await mkdtemp(path.join(guiRoot, ".lint-structure-"));
  const fixturePath = path.join(fixtureDir, "fixture.ts");
  try {
    await writeFile(fixturePath, source, "utf8");
    const result = spawnSync(
      process.execPath,
      [oxlintBin, "-c", configPath, "--format=json", fixturePath],
      {
        cwd: guiRoot,
        encoding: "utf8",
      },
    );
    if (result.error) throw result.error;
    const stdout = String(result.stdout ?? "").trim();
    assert.ok(stdout, `Oxlint JSON fixture scan produced no output:\n${String(result.stderr ?? "")}`);
    const diagnostics = diagnosticsFromOxlintJson(JSON.parse(stdout), guiRoot);
    return {
      result,
      diagnostics,
      structural: summarizeStructuralDiagnostics(diagnostics),
    };
  } finally {
    await rm(fixtureDir, { recursive: true, force: true });
  }
}

function output(result) {
  return `${result.stdout ?? ""}\n${result.stderr ?? ""}`;
}

test("ordinary control flow produces no structural debt", async () => {
  const { result, structural } = await lintFixture(`
export function classify(value: number): number {
  if (value > 10) return 2;
  if (value > 0) return 1;
  return 0;
}
`);
  assert.equal(result.status, 0, output(result));
  assert.deepEqual(structural, []);
  assert.deepEqual(compareStructuralBaseline(structural, []), []);
});

test("nesting deeper than four blocks is rejected when not baselined", async () => {
  const { result, diagnostics, structural } = await lintFixture(`
export function tooDeep(a: boolean, b: boolean, c: boolean, d: boolean, e: boolean): number {
  if (a) {
    if (b) {
      if (c) {
        if (d) {
          if (e) return 1;
        }
      }
    }
  }
  return 0;
}
`);
  const extra = diagnostics.filter((item) => item.code !== "eslint(max-depth)");
  assert.ok(
    extra.every((item) => String(item.code).startsWith("react-doctor(")),
    output(result),
  );
  assert.equal(structural.length, 1);
  assert.equal(structural[0].code, "eslint(max-depth)");
  const errors = compareStructuralBaseline(structural, []);
  assert.equal(errors.length, 1);
  assert.match(errors[0], /unapproved structural violation/i);
  assert.match(errors[0], /max-depth/i);
});

test("modified cyclomatic complexity above fifteen is rejected when not baselined", async () => {
  const branches = Array.from({ length: 15 }, (_, index) => `  if (value === ${index}) total += ${index + 1};`).join("\n");
  const { result, structural } = await lintFixture(`
export function tooComplex(value: number): number {
  let total = 0;
${branches}
  return total;
}
`);
  assert.equal(result.status, 0, output(result));
  assert.equal(structural.length, 1);
  assert.equal(structural[0].code, "eslint(complexity)");
  const errors = compareStructuralBaseline(structural, []);
  assert.equal(errors.length, 1);
  assert.match(errors[0], /unapproved structural violation/i);
  assert.match(errors[0], /complexity/i);
});
