#!/usr/bin/env node
/**
 * The published `benes` launcher.
 *
 * `package.json` binds `bin.benes` to this file and ships `bin/`, `cmd/`, `internal/`,
 * `go.mod`, `go.sum` and `gui/dist`; `scripts/release.ts` packs that manifest and
 * `scripts/prepare-package.ts` marks this file 0755. So this module may use Node builtins only:
 * `scripts/` is not published, and the launcher has to run from a bare global install.
 *
 * The documented resolution order (`docs/.../start/install.md`) is the whole contract:
 *
 *   1. `BENES_BIN` — an explicit path to a Benes CLI build;
 *   2. the build packaged beside this file — `benes.exe` on Windows, `benes` elsewhere;
 *   3. `go run ./cmd/benes` against the packaged module.
 *
 * `go.mod` declares `go 1.27.0`, so step 3 pins `GOTOOLCHAIN` to that toolchain unless the
 * caller set one. The CLI keeps the terminal (`stdio: "inherit"`) because it prints the start
 * banner and can prompt.
 */
import { spawn } from "node:child_process";
import { accessSync, constants, existsSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

/** The package root: the working directory `go run ./cmd/benes` needs. */
const packageRoot = join(dirname(fileURLToPath(import.meta.url)), "..");

/** Mirrors `go.mod`; `.github/scripts/local-ci-contract.test.cjs` pins the two together. */
const GO_TOOLCHAIN = "go1.27.0";

const GO_COMMAND = "go";
const GO_RUN_ARGS = ["run", "./cmd/benes"];

/** Where a release install drops the compiled CLI when it ships one. */
export function packagedBinaryPath(platform, root) {
  return join(root, platform === "win32" ? "benes.exe" : "benes");
}

/**
 * A packaged build is only a candidate when this platform can execute it. Existence alone is
 * not enough on POSIX: a tarball whose mode bits were dropped would fail with EACCES instead of
 * running, where `go run` still works.
 */
export function packagedBinaryUsable(path, platform, io = { existsSync, accessSync }) {
  if (!io.existsSync(path)) return false;
  if (platform === "win32") return true;
  try {
    io.accessSync(path, constants.X_OK);
    return true;
  } catch {
    return false;
  }
}

/** The command, its leading arguments, and where it came from, for the failure message. */
export function resolveLauncher({ platform = process.platform, env = process.env, root = packageRoot } = {}) {
  const override = typeof env.BENES_BIN === "string" ? env.BENES_BIN.trim() : "";
  if (override) return { command: override, args: [], source: "BENES_BIN" };

  const packaged = packagedBinaryPath(platform, root);
  if (packagedBinaryUsable(packaged, platform)) {
    return { command: packaged, args: [], source: "the packaged build" };
  }
  return { command: GO_COMMAND, args: GO_RUN_ARGS, source: "go run ./cmd/benes" };
}

/** Pins the Go toolchain for the source path without overriding an explicit caller choice. */
export function launcherEnv(env = process.env) {
  const pinned = typeof env.GOTOOLCHAIN === "string" && env.GOTOOLCHAIN.trim() !== ""
    ? env.GOTOOLCHAIN
    : GO_TOOLCHAIN;
  return { ...env, GOTOOLCHAIN: pinned };
}

function run(argv) {
  const launcher = resolveLauncher();
  const child = spawn(launcher.command, [...launcher.args, ...argv], {
    cwd: packageRoot,
    stdio: "inherit",
    windowsHide: true,
    env: launcherEnv(),
  });

  child.on("error", error => {
    console.error(`benes: cannot start the CLI through ${launcher.source}: ${error.message}`);
    process.exit(1);
  });

  // The CLI holds the terminal, so the terminal already delivers Ctrl+C to it. All that is left
  // is to report the status the CLI itself ended with: its exit code, or the signal that
  // terminated it so a wrapping shell, service manager or CI step sees the same one.
  child.on("exit", (code, signal) => {
    if (signal && process.platform !== "win32") {
      process.kill(process.pid, signal);
      return;
    }
    process.exit(code ?? 1);
  });
}

const entry = process.argv[1];
if (entry && pathToFileURL(resolve(entry)).href === import.meta.url) {
  run(process.argv.slice(2));
}
