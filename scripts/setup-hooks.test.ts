import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { describe, test } from "node:test";
import { CANONICAL_HOOKS_PATH, setupLocalHooks } from "./setup-hooks.ts";

function git(repo: string, args: string[]) {
  const result = spawnSync("git", args, { cwd: repo, encoding: "utf8" });
  assert.equal(result.status, 0, `${args.join(" ")}\n${result.stderr}`);
  return result.stdout;
}

function scratchRepo() {
  const repo = mkdtempSync(path.join(tmpdir(), "benes-hooks-"));
  mkdirSync(path.join(repo, "scripts", "hooks"), { recursive: true });
  writeFileSync(path.join(repo, "scripts", "hooks", "pre-push"), "#!/bin/sh\necho MAIN\n");
  writeFileSync(path.join(repo, "README"), "hooks\n");
  git(repo, ["init"]);
  git(repo, ["config", "user.email", "hooks@example.test"]);
  git(repo, ["config", "user.name", "hooks"]);
  git(repo, ["add", "."]);
  git(repo, ["commit", "-m", "init"]);
  return repo;
}

function localHooksPath(repo: string) {
  const result = spawnSync("git", ["config", "--local", "--get", "core.hooksPath"], {
    cwd: repo,
    encoding: "utf8",
  });
  return result.status === 0 ? result.stdout.replace(/\r?\n$/, "") : undefined;
}

function resolvedHooks(repo: string) {
  return git(repo, ["rev-parse", "--path-format=absolute", "--git-path", "hooks"]).trim();
}

describe("setupLocalHooks", () => {
  test("configures scripts/hooks on a fresh repository", () => {
    const repo = scratchRepo();
    const result = setupLocalHooks(repo);
    assert.equal(result.status, "configured");
    assert.equal(localHooksPath(repo), CANONICAL_HOOKS_PATH);
    assert.equal(path.isAbsolute(localHooksPath(repo) ?? ""), false);
  });

  test("is idempotent when hooksPath already points at Benes hooks", () => {
    const repo = scratchRepo();
    assert.equal(setupLocalHooks(repo).status, "configured");
    const first = localHooksPath(repo);
    const second = setupLocalHooks(repo);
    assert.equal(second.status, "already");
    assert.equal(localHooksPath(repo), first);
    assert.equal(first, CANONICAL_HOOKS_PATH);
  });

  test("migrates a previous absolute Benes hooksPath to the relative value", () => {
    const repo = scratchRepo();
    git(repo, ["config", "--local", "core.hooksPath", path.resolve(repo, "scripts", "hooks")]);
    const result = setupLocalHooks(repo);
    assert.equal(result.status, "configured");
    assert.equal(localHooksPath(repo), CANONICAL_HOOKS_PATH);
  });

  test("refuses to overwrite a custom local hooksPath and leaves the value unchanged", () => {
    const repo = scratchRepo();
    const custom = path.join(repo, "company-hooks");
    mkdirSync(custom);
    git(repo, ["config", "--local", "core.hooksPath", custom]);
    const before = localHooksPath(repo);
    assert.equal(typeof before, "string");
    const result = setupLocalHooks(repo);
    assert.equal(result.status, "conflict");
    if (result.status === "conflict") {
      assert.equal(result.current, before);
    }
    assert.equal(localHooksPath(repo), before);
    const global = spawnSync("git", ["config", "--global", "--get", "core.hooksPath"], {
      encoding: "utf8",
    });
    assert.notEqual(global.stdout.trim(), CANONICAL_HOOKS_PATH);
    assert.notEqual(global.stdout.trim(), path.resolve(repo, "scripts", "hooks"));
  });

  test("refuses a foreign absolute path that only happens to end in scripts/hooks", () => {
    const repo = scratchRepo();
    const foreign = process.platform === "win32"
      ? "C:\\security\\scripts\\hooks"
      : "/opt/company/scripts/hooks";
    git(repo, ["config", "--local", "core.hooksPath", foreign]);
    const before = localHooksPath(repo);
    const result = setupLocalHooks(repo);
    assert.equal(result.status, "conflict");
    assert.equal(localHooksPath(repo), before);
    assert.equal(localHooksPath(repo), foreign);
  });

  test("migrates an absolute hooksPath from another linked Benes worktree", () => {
    const repoA = scratchRepo();
    const repoB = `${repoA}-b`;
    git(repoA, ["worktree", "add", repoB]);
    try {
      git(repoA, ["config", "--local", "core.hooksPath", path.resolve(repoB, "scripts", "hooks")]);
      const result = setupLocalHooks(repoA);
      assert.equal(result.status, "configured");
      assert.equal(localHooksPath(repoA), CANONICAL_HOOKS_PATH);
    } finally {
      spawnSync("git", ["worktree", "remove", "--force", repoB], { cwd: repoA, encoding: "utf8" });
      rmSync(repoB, { recursive: true, force: true });
    }
  });

  test("linked worktrees share a relative hooksPath and keep their own hook files", () => {
    const repoA = scratchRepo();
    const repoB = `${repoA}-b`;
    git(repoA, ["worktree", "add", repoB]);
    try {
      writeFileSync(path.join(repoB, "scripts", "hooks", "pre-push"), "#!/bin/sh\necho WORKTREE\n");
      const result = setupLocalHooks(repoB);
      assert.ok(result.status === "configured" || result.status === "already");
      const stored = localHooksPath(repoA);
      assert.equal(stored, CANONICAL_HOOKS_PATH);
      assert.equal(localHooksPath(repoB), CANONICAL_HOOKS_PATH);
      assert.equal(stored?.includes(repoB), false);
      assert.equal(path.isAbsolute(stored ?? ""), false);

      const hooksA = path.resolve(resolvedHooks(repoA));
      const hooksB = path.resolve(resolvedHooks(repoB));
      assert.equal(hooksA, path.resolve(repoA, "scripts", "hooks"));
      assert.equal(hooksB, path.resolve(repoB, "scripts", "hooks"));
      assert.notEqual(hooksA, hooksB);

      git(repoA, ["worktree", "remove", "--force", repoB]);
      assert.equal(localHooksPath(repoA), CANONICAL_HOOKS_PATH);
      assert.equal(path.resolve(resolvedHooks(repoA)), path.resolve(repoA, "scripts", "hooks"));
      assert.equal(existsSync(repoB), false);
    } finally {
      spawnSync("git", ["worktree", "remove", "--force", repoB], { cwd: repoA, encoding: "utf8" });
      rmSync(repoB, { recursive: true, force: true });
    }
  });
});
