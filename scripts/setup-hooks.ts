/**
 * Point this clone at scripts/hooks via local git config:
 *   npm run setup:hooks
 *
 * pre-push   → npm run prepush
 * post-merge → npm run postmerge
 *
 * The stored value is the relative path `scripts/hooks` so linked
 * worktrees share one config entry and each uses its own checkout.
 *
 * Emergency bypass: git push --no-verify / git pull --no-verify
 *
 * A custom local core.hooksPath is left unchanged. That conflict is a
 * refusal, not an overwrite.
 */
import { execFileSync } from "node:child_process";
import { existsSync } from "node:fs";
import { isAbsolute, resolve } from "node:path";
import { isMainModule } from "./node-runtime.ts";

export const CANONICAL_HOOKS_PATH = "scripts/hooks";

export type HooksSetupResult =
  | { status: "configured"; hooksPath: string }
  | { status: "already"; hooksPath: string }
  | { status: "conflict"; hooksPath: string; current: string };

function gitLocal(repoRoot: string, args: string[], extra: { stdio?: "ignore" | "pipe" } = {}) {
  return execFileSync("git", args, {
    cwd: repoRoot,
    encoding: "utf8",
    stdio: extra.stdio ?? "pipe",
  });
}

function readLocalHooksPath(repoRoot: string): string | undefined {
  try {
    const value = gitLocal(repoRoot, ["config", "--local", "--get", "core.hooksPath"]);
    const trimmed = value.trim();
    return trimmed.length > 0 ? trimmed : undefined;
  } catch {
    return undefined;
  }
}

function posixSlashes(value: string): string {
  return value.replaceAll("\\", "/");
}

function looksAbsolute(value: string): boolean {
  return isAbsolute(value) || /^[A-Za-z]:[\\/]/.test(value) || value.startsWith("/");
}

function samePath(left: string, right: string): boolean {
  const a = posixSlashes(resolve(left)).replace(/\/+$/, "");
  const b = posixSlashes(resolve(right)).replace(/\/+$/, "");
  return process.platform === "win32" ? a.toLowerCase() === b.toLowerCase() : a === b;
}

export function linkedWorktreeRoots(repoRoot: string): string[] {
  const output = gitLocal(repoRoot, ["worktree", "list", "--porcelain"]);
  const roots: string[] = [];
  for (const line of output.split(/\r?\n/)) {
    if (line.startsWith("worktree ")) {
      roots.push(line.slice("worktree ".length).trim());
    }
  }
  return roots;
}

export function isBenesManagedHooksPath(value: string, repoRoot: string): boolean {
  const trimmed = value.trim();
  const posix = posixSlashes(trimmed).replace(/^\.\//, "");
  if (posix === CANONICAL_HOOKS_PATH) return true;
  if (samePath(resolve(repoRoot, trimmed), resolve(repoRoot, CANONICAL_HOOKS_PATH))) {
    if (!looksAbsolute(trimmed)) return true;
  }
  if (!looksAbsolute(trimmed)) return false;
  try {
    const allowed = linkedWorktreeRoots(repoRoot).map((root) => resolve(root, CANONICAL_HOOKS_PATH));
    return allowed.some((hooks) => samePath(trimmed, hooks));
  } catch {
    return false;
  }
}

export function setupLocalHooks(repoRoot: string): HooksSetupResult {
  const checkoutHooks = resolve(repoRoot, CANONICAL_HOOKS_PATH);
  if (!existsSync(checkoutHooks)) {
    throw new Error(`setup-hooks: missing ${checkoutHooks}`);
  }

  try {
    gitLocal(repoRoot, ["rev-parse", "--is-inside-work-tree"], { stdio: "ignore" });
  } catch {
    throw new Error("setup-hooks: git is missing, or this is not a repository.");
  }

  const current = readLocalHooksPath(repoRoot);
  if (current === undefined) {
    gitLocal(repoRoot, ["config", "--local", "core.hooksPath", CANONICAL_HOOKS_PATH]);
    return { status: "configured", hooksPath: CANONICAL_HOOKS_PATH };
  }
  if (posixSlashes(current).replace(/^\.\//, "") === CANONICAL_HOOKS_PATH) {
    return { status: "already", hooksPath: CANONICAL_HOOKS_PATH };
  }
  if (isBenesManagedHooksPath(current, repoRoot)) {
    gitLocal(repoRoot, ["config", "--local", "core.hooksPath", CANONICAL_HOOKS_PATH]);
    return { status: "configured", hooksPath: CANONICAL_HOOKS_PATH };
  }
  return { status: "conflict", hooksPath: CANONICAL_HOOKS_PATH, current };
}

function printResult(result: HooksSetupResult): void {
  if (result.status === "conflict") {
    console.error("setup-hooks: local core.hooksPath is already set to:");
    console.error(`  ${result.current}`);
    console.error("Benes needs:");
    console.error(`  ${result.hooksPath}`);
    console.error("Left the custom path unchanged. To switch deliberately:");
    console.error(`  git config --local core.hooksPath ${result.hooksPath}`);
    return;
  }
  const verb = result.status === "already" ? "already" : "set";
  console.log(`core.hooksPath ${verb} → ${result.hooksPath}`);
  console.log("pre-push runs the local prepush gate; post-merge rebuilds the dashboard when sources changed.");
  console.log("Skip in an emergency with: git push --no-verify / git pull --no-verify");
}

if (isMainModule(import.meta.url)) {
  const root = resolve(import.meta.dirname, "..");
  try {
    const result = setupLocalHooks(root);
    printResult(result);
    process.exit(result.status === "conflict" ? 1 : 0);
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exit(1);
  }
}
