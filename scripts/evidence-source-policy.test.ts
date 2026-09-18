import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { describe, test } from "node:test";
import { fileURLToPath } from "node:url";

const SCRIPTS = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(SCRIPTS, "..");
const CLEANUP_PRIMITIVE = "scripts/evidence-scratch.ts";
const SOURCE_EXTENSION_RE = /\.(?:ts|mjs|js|cjs|ps1)$/i;

function isEvidenceAutomationPath(name: string): boolean {
  const normalized = name.replaceAll("\\", "/");
  if (!SOURCE_EXTENSION_RE.test(normalized) || /\.test\.(?:ts|js|cjs|mjs)$/i.test(normalized)) return false;
  const basename = path.posix.basename(normalized);
  return /evidence|cdp|ui-browser|capture-ui-evidence/i.test(basename);
}

function usesRecursiveDelete(source: string): boolean {
  return /\bRemove-Item\b[\s\S]{0,600}?(?:\s|`)-Recurse\b/iu.test(source)
    || /\b(?:rm|rmSync)\s*\([\s\S]{0,800}?\brecursive\s*:\s*true/iu.test(source);
}

function usesGlobalBrowserKill(source: string): boolean {
  return /\btaskkill(?:\.exe)?\b[\s\S]{0,400}?(?:\/IM\b|-IM\b)/iu.test(source)
    || /\bStop-Process\b[\s\S]{0,400}?(?:chrome|msedge|firefox)/iu.test(source)
    || /\bGet-Process\b[\s\S]{0,400}?(?:chrome|msedge|firefox)/iu.test(source);
}

function importsModule(source: string, specifier: string): boolean {
  return source.includes(`"${specifier}"`);
}

function presetsChromiumPasswordManagerPreferences(source: string): boolean {
  return /(?:os_password_blank|os_password_last_changed|password_manager|Login Data)/iu.test(source);
}

const CANONICAL_RUNNER = "scripts/evidence-browser-runner.ts";
const BROWSER_RUNTIME = "scripts/evidence-browser-runtime.ts";
const CDP_SESSION = "scripts/evidence-cdp-session.ts";

// Arguments the canonical runner and the browser runtime own on the caller's
// behalf; a caller-supplied copy would move the browser off the owned profile or
// off the bounded CDP transport.
const LIFECYCLE_CRITICAL_BROWSER_ARGS = [
  "--user-data-dir",
  "--remote-debugging-port",
  "--remote-debugging-pipe",
];

async function evidenceSources(): Promise<Array<{ name: string; source: string }>> {
  const tracked = execFileSync("git", ["-C", ROOT, "ls-files", "-z"], { encoding: "utf8" })
    .split("\0")
    .filter(Boolean)
    .filter(isEvidenceAutomationPath)
    .sort();

  return await Promise.all(tracked.map(async (name) => ({
    name,
    source: await readFile(path.join(ROOT, name), "utf8"),
  })));
}

describe("evidence automation source policy", () => {
  test("discovers historical and nested evidence/browser runner naming patterns", () => {
    assert.equal(isEvidenceAutomationPath("scripts/evidence-runner.ts"), true);
    assert.equal(isEvidenceAutomationPath("scripts/ui-browser.ps1"), true);
    assert.equal(isEvidenceAutomationPath("scripts/nested/capture-ui-evidence.mjs"), true);
    assert.equal(isEvidenceAutomationPath("scripts/nested/cdp-session.ts"), true);
  });

  test("detects recursive deletion even when it is formatted across lines", () => {
    assert.equal(usesRecursiveDelete("Remove-Item `\n  -LiteralPath $target `\n  -Recurse -Force"), true);
    assert.equal(usesRecursiveDelete("await rm(target, {\n  recursive: true,\n  force: false,\n});"), true);
  });

  test("recursive deletion exists only in the owned-scratch cleanup primitive", async () => {
    for (const { name, source } of await evidenceSources()) {
      const destructive = usesRecursiveDelete(source);
      if (name === CLEANUP_PRIMITIVE) {
        assert.equal(destructive, true, `${name} must remain the single cleanup primitive`);
      } else {
        assert.equal(destructive, false, `${name} must not recursively delete paths`);
      }
    }
  });

  test("evidence sources never terminate browser processes globally by executable name", async () => {
    for (const { name, source } of await evidenceSources()) {
      assert.equal(usesGlobalBrowserKill(source), false, `${name} must not globally terminate a browser`);
    }
  });

  test("browser profile selection is centralized in the browser runtime", async () => {
    const owners: string[] = [];
    for (const { name, source } of await evidenceSources()) {
      if (source.includes("--user-data-dir=")) owners.push(name);
    }
    assert.deepEqual(owners, [BROWSER_RUNTIME]);
  });

  test("the canonical runner names every lifecycle-critical browser argument it owns", async () => {
    const runner = (await evidenceSources()).find(({ name }) => name === CANONICAL_RUNNER);
    assert.ok(runner, `${CANONICAL_RUNNER} must exist`);

    for (const flag of LIFECYCLE_CRITICAL_BROWSER_ARGS) {
      assert.ok(
        runner.source.includes(`"${flag}"`),
        `${CANONICAL_RUNNER} must reject caller-supplied ${flag}`,
      );
    }
  });

  test("only the browser runtime starts owned processes", async () => {
    const owners: string[] = [];
    for (const { name, source } of await evidenceSources()) {
      if (importsModule(source, "./evidence-process.ts")) owners.push(name);
    }
    assert.deepEqual(owners, [BROWSER_RUNTIME]);
  });

  test("only the canonical runner composes the browser automation lifecycle", async () => {
    const owners: string[] = [];
    for (const { name, source } of await evidenceSources()) {
      if (importsModule(source, "./evidence-browser-runtime.ts")) owners.push(name);
    }
    assert.deepEqual(owners, [CANONICAL_RUNNER]);
  });

  test("evidence sources never preseed Chromium password-manager preferences", async () => {
    for (const { name, source } of await evidenceSources()) {
      assert.equal(
        presetsChromiumPasswordManagerPreferences(source),
        false,
        `${name} must not preseed Chromium password-manager preferences`,
      );
    }
  });

  test("CDP request registry ownership is centralized in the socket-bound session", async () => {
    const owners: string[] = [];
    for (const { name, source } of await evidenceSources()) {
      if (source.includes("new CdpRequestRegistry(")) owners.push(name);
    }
    assert.deepEqual(owners, [CDP_SESSION]);
  });

  test("only the socket-bound CDP session binds the CDP request registry", async () => {
    const owners: string[] = [];
    for (const { name, source } of await evidenceSources()) {
      if (importsModule(source, "./cdp-request-registry.ts")) owners.push(name);
    }
    assert.deepEqual(owners, [CDP_SESSION]);
  });

  test("scripts/AGENTS.md states the evidence-browser ownership rules", async () => {
    const policy = await readFile(path.join(SCRIPTS, "AGENTS.md"), "utf8");

    for (const required of [
      "runBrowserEvidence",
      "scripts/evidence-browser-runner.ts",
      "persistent",
      "--remote-debugging-pipe",
    ]) {
      assert.ok(
        policy.includes(required),
        `scripts/AGENTS.md must state the evidence-browser ownership rule for: ${required}`,
      );
    }
  });
});
