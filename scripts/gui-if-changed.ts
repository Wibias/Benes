/**
 * Decide whether a git range touched `gui/`, then lint or rebuild.
 *
 *   npm run lint:gui:if-changed  →  node scripts/gui-if-changed.ts lint
 *   npm run postmerge            →  node scripts/gui-if-changed.ts build
 *
 * Lint: missing comparison base still runs oxlint (a first GUI-only push
 * must not skip). Build: missing ORIG_HEAD skips (the merge already landed;
 * an unconditional rebuild on every pull is worse than a stale dist).
 */
import { spawnSync } from "node:child_process";
import { join, resolve } from "node:path";
import { isMainModule, npmInvocation } from "./node-runtime.ts";

export type Gate = "lint" | "build";
export type Decision = "run" | "skip";

export function isGuiPath(relPath: string): boolean {
  return relPath === "gui" || relPath.startsWith("gui/");
}

export function decide(gate: Gate, changed: string[] | null): Decision {
  if (gate === "lint") {
    if (changed === null) return "run";
    return changed.some(isGuiPath) ? "run" : "skip";
  }
  if (changed === null) return "skip";
  return changed.some(isGuiPath) ? "run" : "skip";
}

function gitOk(root: string, args: string[]) {
  return spawnSync("git", args, { cwd: root, encoding: "utf8" });
}

function refExists(root: string, ref: string): boolean {
  return gitOk(root, ["rev-parse", "--verify", ref]).status === 0;
}

function namesInRange(root: string, range: string): string[] {
  const diff = gitOk(root, ["diff", "--name-only", range]);
  if (diff.status !== 0) return [];
  return (diff.stdout ?? "").split(/\r?\n/).map((line) => line.trim()).filter(Boolean);
}

function lintChangedPaths(root: string): string[] | null {
  const range = refExists(root, "@{u}")
    ? "@{u}...HEAD"
    : refExists(root, "origin/dev")
      ? "origin/dev...HEAD"
      : refExists(root, "dev")
        ? "dev...HEAD"
        : null;
  if (!range) return null;
  return namesInRange(root, range);
}

function buildChangedPaths(root: string): string[] | null {
  if (!refExists(root, "ORIG_HEAD")) return null;
  return namesInRange(root, "ORIG_HEAD...HEAD");
}

function parseGate(argv: string[]): Gate {
  const gate = argv[0];
  if (gate === "lint" || gate === "build") return gate;
  console.error("usage: node scripts/gui-if-changed.ts lint|build [--dry-run]");
  process.exit(2);
}

if (isMainModule(import.meta.url)) {
  const gate = parseGate(process.argv.slice(2).filter((arg) => arg !== "--dry-run"));
  const dryRun = process.argv.includes("--dry-run");
  const root = resolve(import.meta.dirname, "..");
  const changed = gate === "lint" ? lintChangedPaths(root) : buildChangedPaths(root);
  const decision = decide(gate, changed);

  if (dryRun) {
    console.log(`${gate}:${decision}`);
    process.exit(0);
  }

  if (decision === "skip") {
    if (gate === "lint") {
      console.log("lint:gui: skip (no gui/ paths in the push range)");
    }
    process.exit(0);
  }

  if (gate === "lint") {
    console.log("lint:gui: running oxlint in gui/");
    const invocation = npmInvocation(["run", "lint"]);
    const result = spawnSync(invocation.command, invocation.args, {
      cwd: join(root, "gui"),
      encoding: "utf8",
      stdio: "inherit",
    });
    if (result.error) {
      console.error(`lint:gui: spawn failed: ${result.error.message}`);
      process.exit(1);
    }
    process.exit(result.status === null ? 1 : result.status);
  }

  console.log("build:gui: gui/ changed — rebuilding packaged dashboard");
  const invocation = npmInvocation(["run", "build:gui"]);
  const built = spawnSync(invocation.command, invocation.args, {
    cwd: root,
    stdio: "inherit",
  });
  if (built.status !== 0) {
    console.error("build:gui failed. Serve the previous bundle, or run: npm run build:gui");
  }
  process.exit(0);
}
