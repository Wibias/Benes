import { chmodSync, existsSync, lstatSync, readdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { isMainModule } from "./node-runtime.ts";

const packageRoot = dirname(fileURLToPath(new URL("../package.json", import.meta.url)));

function tryMode(target: string, mode: number): void {
  try {
    chmodSync(target, mode);
  } catch {
    // Read-only CI volumes still pack; chmod is best-effort.
  }
}

function setRegularFileMode(target: string, mode: number): void {
  if (!existsSync(target)) return;
  if (lstatSync(target).isSymbolicLink()) return;
  tryMode(target, mode);
}

function setDashboardTreeModes(dist: string): void {
  if (!existsSync(dist)) return;
  const root = lstatSync(dist);
  if (root.isSymbolicLink() || !root.isDirectory()) return;
  tryMode(dist, 0o755);
  const stack = [dist];
  while (stack.length > 0) {
    const dir = stack.pop();
    if (!dir) continue;
    let entries;
    try {
      entries = readdirSync(dir, { withFileTypes: true });
    } catch {
      continue;
    }
    for (const entry of entries) {
      if (entry.isSymbolicLink()) continue;
      const abs = join(dir, entry.name);
      if (entry.isDirectory()) {
        tryMode(abs, 0o755);
        stack.push(abs);
        continue;
      }
      if (entry.isFile()) tryMode(abs, 0o644);
    }
  }
}

export function applyPublishedModes(root: string): void {
  setRegularFileMode(join(root, "bin", "benes.mjs"), 0o755);
  setRegularFileMode(join(root, "bin", "package-main.mjs"), 0o644);
  setDashboardTreeModes(join(root, "gui", "dist"));
}

if (isMainModule(import.meta.url)) {
  applyPublishedModes(packageRoot);
}
