import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { describe, test } from "node:test";
import { fileURLToPath } from "node:url";

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const RUNNER = path.join(ROOT, "scripts", "benes-run");
const BASH = [
  "C:\\Program Files\\Git\\bin\\bash.exe",
  "C:\\Program Files\\Git\\usr\\bin\\bash.exe",
  "/bin/bash",
  "/usr/bin/bash",
].find((candidate) => existsSync(candidate)) ?? null;

function posix(value) {
  return value.replaceAll("\\", "/");
}

function stub(dir, name, body) {
  const file = path.join(dir, name);
  writeFileSync(file, body, { encoding: "utf8" });
  try {
    chmodSync(file, 0o755);
  } catch {
    // Git Bash on Windows uses the shebang path passed to bash.exe.
  }
}

function run(args, { cwd, env }) {
  return spawnSync(BASH, [posix(RUNNER), ...args], {
    cwd,
    env,
    encoding: "utf8",
    timeout: 30_000,
  });
}

function start(args, { cwd, env }) {
  return spawn(BASH, [posix(RUNNER), ...args], {
    cwd,
    env,
    encoding: "utf8",
  });
}

function collect(child) {
  return new Promise((resolve) => {
    let stdout = "";
    let stderr = "";
    let done = false;
    const finish = (status) => {
      if (done) return;
      done = true;
      resolve({ status, stdout, stderr });
    };
    child.stdout?.on("data", (chunk) => {
      stdout += chunk;
    });
    child.stderr?.on("data", (chunk) => {
      stderr += chunk;
    });
    child.on("close", finish);
    if (child.exitCode !== null) finish(child.exitCode);
  });
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function waitUntil(predicate, label, ms = 8_000) {
  const startAt = Date.now();
  while (Date.now() - startAt < ms) {
    if (predicate()) return;
    await sleep(50);
  }
  throw new Error(`timed out waiting for ${label}`);
}

function jobEnv(root, store, extra = {}) {
  const stubs = path.join(root, "stubs");
  mkdirSync(stubs, { recursive: true });
  stub(stubs, "setsid", "#!/bin/sh\nexec \"$@\"\n");
  const gitUsr = existsSync("C:\\Program Files\\Git\\usr\\bin") ? "/usr/bin" : "";
  return {
    ...process.env,
    PATH: [posix(stubs), gitUsr, process.env.PATH].filter(Boolean).join(":"),
    BENES_RUN_DIR: posix(store),
    ...extra,
  };
}

describe("benes-run", { skip: BASH ? false : "bash is not available" }, () => {
  test("refuses a missing workdir and does not create job state", () => {
    const root = mkdtempSync(path.join(tmpdir(), "benes-run-absent-"));
    const store = path.join(root, "store");
    const missing = path.join(root, "nope");
    const result = run(["probe", posix(missing), "8s", "true"], {
      cwd: root,
      env: { ...process.env, BENES_RUN_DIR: posix(store) },
    });
    assert.notEqual(result.status, 0);
    assert.match(`${result.stderr}${result.stdout}`, /workdir/i);
    assert.equal(existsSync(path.join(store, "probe", "state.json")), false);
  });

  test("starts the child inside workdir, not the caller directory", () => {
    const root = mkdtempSync(path.join(tmpdir(), "benes-run-home-"));
    const workdir = path.join(root, "job-home");
    const caller = path.join(root, "caller");
    const stubs = path.join(root, "stubs");
    const store = path.join(root, "store");
    mkdirSync(workdir);
    mkdirSync(caller);
    mkdirSync(stubs);
    writeFileSync(path.join(workdir, "marker.txt"), "workdir-ok\n");
    writeFileSync(path.join(caller, "marker.txt"), "caller-ok\n");
    stub(stubs, "setsid", "#!/bin/sh\nexec \"$@\"\n");
    const gitUsr = existsSync("C:\\Program Files\\Git\\usr\\bin") ? "/usr/bin" : "";
    const result = run(["cwd-check", posix(workdir), "12s", "cat", "marker.txt"], {
      cwd: caller,
      env: {
        ...process.env,
        PATH: [posix(stubs), gitUsr, process.env.PATH].filter(Boolean).join(":"),
        BENES_RUN_DIR: posix(store),
      },
    });
    const log = existsSync(path.join(store, "cwd-check", "log"))
      ? readFileSync(path.join(store, "cwd-check", "log"), "utf8")
      : "";
    assert.equal(result.status, 0, `stdout=${result.stdout}\nstderr=${result.stderr}\nlog=${log}`);
    assert.equal(log.trim(), "workdir-ok");
  });

  test("ignores CDPATH when entering workdir, including names with spaces", () => {
    const root = mkdtempSync(path.join(tmpdir(), "benes-run-cdpath-"));
    const workdir = path.join(root, "job home");
    const decoy = path.join(root, "decoy", "job home");
    const caller = path.join(root, "caller");
    const stubs = path.join(root, "stubs");
    const store = path.join(root, "store");
    mkdirSync(workdir);
    mkdirSync(decoy, { recursive: true });
    mkdirSync(caller);
    mkdirSync(stubs);
    writeFileSync(path.join(workdir, "marker.txt"), "real-workdir\n");
    writeFileSync(path.join(decoy, "marker.txt"), "cdpath-decoy\n");
    stub(stubs, "setsid", "#!/bin/sh\nexec \"$@\"\n");
    const gitUsr = existsSync("C:\\Program Files\\Git\\usr\\bin") ? "/usr/bin" : "";
    const result = run(["cdpath-check", posix(workdir), "12s", "cat", "marker.txt"], {
      cwd: caller,
      env: {
        ...process.env,
        PATH: [posix(stubs), gitUsr, process.env.PATH].filter(Boolean).join(":"),
        BENES_RUN_DIR: posix(store),
        CDPATH: posix(path.join(root, "decoy")),
      },
    });
    const log = existsSync(path.join(store, "cdpath-check", "log"))
      ? readFileSync(path.join(store, "cdpath-check", "log"), "utf8")
      : "";
    assert.equal(result.status, 0, `stdout=${result.stdout}\nstderr=${result.stderr}\nlog=${log}`);
    assert.equal(log.trim(), "real-workdir");
  });

  test("two near-simultaneous starts of the same job accept exactly one runner", async () => {
    const root = mkdtempSync(path.join(tmpdir(), "benes-run-race-"));
    const workdir = path.join(root, "work");
    const store = path.join(root, "store");
    mkdirSync(workdir);
    const env = jobEnv(root, store);
    const args = ["same", posix(workdir), "12s", "sleep", "2"];
    const first = start(args, { cwd: root, env });
    const second = start(args, { cwd: root, env });
    const results = await Promise.all([collect(first), collect(second)]);
    const codes = results.map((item) => item.status).sort((a, b) => a - b);
    assert.deepEqual(codes, [0, 3], JSON.stringify(results));
    assert.equal(results.filter((item) => item.status === 3).length, 1);
  });

  test("a second start cannot steal the claim before the child pid exists", async () => {
    const root = mkdtempSync(path.join(tmpdir(), "benes-run-window-"));
    const workdir = path.join(root, "work");
    const store = path.join(root, "store");
    mkdirSync(workdir);
    const job = path.join(store, "window");
    const holder = start(["window", posix(workdir), "12s", "true"], {
      cwd: root,
      env: jobEnv(root, store, { BENES_RUN_PAUSE_AFTER_CLAIM: "3" }),
    });
    await waitUntil(() => existsSync(path.join(job, "held")), "owner claim");
    assert.equal(existsSync(path.join(job, "pid")), false);
    const thief = run(["window", posix(workdir), "12s", "true"], {
      cwd: root,
      env: jobEnv(root, store),
    });
    assert.equal(thief.status, 3, `${thief.stdout}${thief.stderr}`);
    const held = await collect(holder);
    assert.equal(held.status, 0, `${held.stdout}${held.stderr}`);
  });

  test("kill plus immediate restart cannot let the old runner overwrite the new state", async () => {
    const root = mkdtempSync(path.join(tmpdir(), "benes-run-kill-"));
    const workdir = path.join(root, "work");
    const store = path.join(root, "store");
    mkdirSync(workdir);
    const job = path.join(store, "recycle");
    const env = jobEnv(root, store, { BENES_RUN_HOLD_AFTER_WAIT: "6" });
    const holder = start(["recycle", posix(workdir), "20s", "sleep", "1"], { cwd: root, env });
    await waitUntil(() => existsSync(path.join(job, "pid")), "child pid");
    const killed = run(["kill", "recycle"], { cwd: root, env });
    assert.equal(killed.status, 0, `${killed.stdout}${killed.stderr}`);
    const replacement = run(["recycle", posix(workdir), "12s", "printf", "replacement-marker\\n"], {
      cwd: root,
      env: jobEnv(root, store),
    });
    assert.equal(replacement.status, 3, `${replacement.stdout}${replacement.stderr}`);
    const finished = await collect(holder);
    const state = readFileSync(path.join(job, "state.json"), "utf8");
    const log = existsSync(path.join(job, "log")) ? readFileSync(path.join(job, "log"), "utf8") : "";
    assert.doesNotMatch(state, /replacement-marker/);
    assert.doesNotMatch(log, /replacement-marker/);
    assert.match(state, /stopped|fail|timeout|ok/);
    assert.ok(finished.status === 0 || finished.status === 137 || finished.status === 143 || finished.status === 124);
  });

  test("a claim whose owner pid is dead is recovered on the next start", () => {
    const root = mkdtempSync(path.join(tmpdir(), "benes-run-stale-"));
    const workdir = path.join(root, "work");
    const store = path.join(root, "store");
    const job = path.join(store, "stale");
    mkdirSync(workdir);
    mkdirSync(job, { recursive: true });
    writeFileSync(path.join(job, "held"), "999999\n");
    writeFileSync(path.join(job, "pid"), "999998\n");
    const result = run(["stale", posix(workdir), "12s", "printf", "recovered\\n"], {
      cwd: root,
      env: jobEnv(root, store),
    });
    const log = existsSync(path.join(job, "log")) ? readFileSync(path.join(job, "log"), "utf8") : "";
    assert.equal(result.status, 0, `${result.stdout}${result.stderr}\nlog=${log}`);
    assert.match(log, /recovered/);
  });

  test("refuses replacement when the owner is dead but the recorded child is still alive", async () => {
    const root = mkdtempSync(path.join(tmpdir(), "benes-run-orphan-child-"));
    const workdir = path.join(root, "work");
    const store = path.join(root, "store");
    const job = path.join(store, "orphan");
    mkdirSync(workdir);
    mkdirSync(job, { recursive: true });
    writeFileSync(path.join(job, "held"), "999999\n");
    const env = jobEnv(root, store);
    const pidFile = path.join(root, "sleeper.pid");
    const child = spawn(BASH, ["-c", `sleep 20 & echo $! > "${posix(pidFile)}"; wait`], {
      cwd: root,
      env,
      stdio: "ignore",
    });
    try {
      await waitUntil(
        () => existsSync(pidFile) && /^\d+$/.test(readFileSync(pidFile, "utf8").trim()),
        "bash child pid",
      );
      writeFileSync(path.join(job, "pid"), `${readFileSync(pidFile, "utf8").trim()}\n`);
      const result = run(["orphan", posix(workdir), "12s", "printf", "should-not-run\\n"], {
        cwd: root,
        env,
      });
      assert.equal(result.status, 3, `${result.stdout}${result.stderr}`);
      assert.equal(readFileSync(path.join(job, "held"), "utf8").trim(), "999999");
      assert.equal(existsSync(path.join(job, "log")), false);
    } finally {
      child.kill("SIGKILL");
      await collect(child);
    }
  });

  test("two reclaimers of one stale claim produce exactly one owner", async () => {
    const root = mkdtempSync(path.join(tmpdir(), "benes-run-reclaim-race-"));
    const workdir = path.join(root, "work");
    const store = path.join(root, "store");
    const job = path.join(store, "race");
    mkdirSync(workdir);
    mkdirSync(job, { recursive: true });
    writeFileSync(path.join(job, "held"), "999999\n");
    const env = jobEnv(root, store);
    const args = ["race", posix(workdir), "12s", "sleep", "2"];
    const results = await Promise.all([
      collect(start(args, { cwd: root, env })),
      collect(start(args, { cwd: root, env })),
    ]);
    const codes = results.map((item) => item.status).sort((a, b) => a - b);
    assert.deepEqual(codes, [0, 3], JSON.stringify(results));
  });

  test("a leftover stop-requested file does not mark a recovered replacement as stopped", () => {
    const root = mkdtempSync(path.join(tmpdir(), "benes-run-stale-stop-"));
    const workdir = path.join(root, "work");
    const store = path.join(root, "store");
    const job = path.join(store, "stale-stop");
    mkdirSync(workdir);
    mkdirSync(job, { recursive: true });
    writeFileSync(path.join(job, "held"), "999999\n");
    writeFileSync(path.join(job, "stop-requested"), "");
    const result = run(["stale-stop", posix(workdir), "12s", "printf", "fresh-ok\\n"], {
      cwd: root,
      env: jobEnv(root, store),
    });
    const state = existsSync(path.join(job, "state.json"))
      ? readFileSync(path.join(job, "state.json"), "utf8")
      : "";
    const log = existsSync(path.join(job, "log")) ? readFileSync(path.join(job, "log"), "utf8") : "";
    assert.equal(result.status, 0, `${result.stdout}${result.stderr}\nstate=${state}\nlog=${log}`);
    assert.match(log, /fresh-ok/);
    assert.match(state, /^ok /);
    assert.doesNotMatch(state, /stopped/);
    assert.equal(existsSync(path.join(job, "stop-requested")), false);
  });

  test("once held exists it already names an owner and cannot be reclaimed as empty", async () => {
    const root = mkdtempSync(path.join(tmpdir(), "benes-run-held-file-"));
    const workdir = path.join(root, "work");
    const store = path.join(root, "store");
    mkdirSync(workdir);
    const job = path.join(store, "heldfile");
    const holder = start(["heldfile", posix(workdir), "12s", "true"], {
      cwd: root,
      env: jobEnv(root, store, { BENES_RUN_PAUSE_AFTER_CLAIM: "3" }),
    });
    await waitUntil(() => existsSync(path.join(job, "held")), "published held");
    const body = readFileSync(path.join(job, "held"), "utf8").trim();
    assert.match(body, /^[1-9][0-9]*$/);
    assert.equal(existsSync(path.join(job, "held", "runner")), false);
    const thief = run(["heldfile", posix(workdir), "12s", "true"], {
      cwd: root,
      env: jobEnv(root, store),
    });
    assert.equal(thief.status, 3, `${thief.stdout}${thief.stderr}`);
    const finished = await collect(holder);
    assert.equal(finished.status, 0, `${finished.stdout}${finished.stderr}`);
  });

  test("a second claimant cannot publish while the first still holds the start gate", { timeout: 25_000 }, async () => {
    const root = mkdtempSync(path.join(tmpdir(), "benes-run-lost-link-"));
    const workdir = path.join(root, "work");
    const store = path.join(root, "store");
    mkdirSync(workdir);
    const job = path.join(store, "lost");
    const first = start(["lost", posix(workdir), "12s", "printf", "first-owner\\n"], {
      cwd: root,
      env: jobEnv(root, store, { BENES_RUN_PAUSE_BEFORE_PUBLISH: "3" }),
    });
    const firstDone = collect(first);
    await waitUntil(() => {
      try {
        return readdirSync(job).some((name) => name.startsWith("owner."));
      } catch {
        return false;
      }
    }, "prepared stamp");
    assert.equal(existsSync(path.join(job, "held")), false);
    const second = start(["lost", posix(workdir), "12s", "printf", "second-should-not-run\\n"], {
      cwd: root,
      env: jobEnv(root, store),
    });
    const secondDone = collect(second);
    const holdUntil = Date.now() + 1_000;
    while (Date.now() < holdUntil) {
      assert.equal(existsSync(path.join(job, "held")), false, "second published during first's gate hold");
      assert.equal(second.exitCode, null);
      await sleep(50);
    }
    const results = await Promise.all([firstDone, secondDone]);
    assert.equal(results[0].status, 0, `${results[0].stdout}${results[0].stderr}`);
    assert.equal(results[1].status, 3, `${results[1].stdout}${results[1].stderr}`);
    const log = existsSync(path.join(job, "log")) ? readFileSync(path.join(job, "log"), "utf8") : "";
    assert.match(log, /first-owner/);
    assert.doesNotMatch(log, /second-should-not-run/);
  });

  test("a forced failed publication cannot return success", () => {
    const root = mkdtempSync(path.join(tmpdir(), "benes-run-fail-pub-"));
    const workdir = path.join(root, "work");
    const store = path.join(root, "store");
    mkdirSync(workdir);
    const result = run(["failpub", posix(workdir), "12s", "true"], {
      cwd: root,
      env: jobEnv(root, store, { BENES_RUN_FAIL_PUBLISH: "1" }),
    });
    assert.notEqual(result.status, 0);
    assert.equal(existsSync(path.join(store, "failpub", "held")), false);
  });

  test("an empty leftover reclaiming directory does not wedge recovery", () => {
    const root = mkdtempSync(path.join(tmpdir(), "benes-run-empty-reclaim-"));
    const workdir = path.join(root, "work");
    const store = path.join(root, "store");
    const job = path.join(store, "empty-reclaim");
    mkdirSync(workdir);
    mkdirSync(job, { recursive: true });
    writeFileSync(path.join(job, "held"), "999999\n");
    mkdirSync(path.join(job, "reclaiming"));
    const result = run(["empty-reclaim", posix(workdir), "12s", "printf", "unwedged\\n"], {
      cwd: root,
      env: jobEnv(root, store),
    });
    const log = existsSync(path.join(job, "log")) ? readFileSync(path.join(job, "log"), "utf8") : "";
    assert.equal(result.status, 0, `${result.stdout}${result.stderr}\nlog=${log}`);
    assert.match(log, /unwedged/);
  });

  test("a claimant cannot publish while stale recovery still owns the transition", { timeout: 25_000 }, async () => {
    const root = mkdtempSync(path.join(tmpdir(), "benes-run-claim-vs-recovery-"));
    const workdir = path.join(root, "work");
    const store = path.join(root, "store");
    const job = path.join(store, "recover");
    mkdirSync(workdir);
    mkdirSync(job, { recursive: true });
    writeFileSync(path.join(job, "held"), "999999\n");
    writeFileSync(path.join(job, "pid"), "999998\n");
    writeFileSync(path.join(job, "stop-requested"), "");
    writeFileSync(path.join(job, "state.json"), "fail rc=1 at=stale\n");
    writeFileSync(path.join(job, "log"), "old-stale-log\n");
    const env = jobEnv(root, store);
    const reclaimer = start(
      ["recover", posix(workdir), "12s", "sh", "-c", "printf 'reclaimer-a\\n'; sleep 2"],
      { cwd: root, env: jobEnv(root, store, { BENES_RUN_PAUSE_AFTER_STALE_CLEAR: "4" }) },
    );
    const reclaimerDone = collect(reclaimer);
    await waitUntil(
      () => existsSync(path.join(job, "recovering")) && !existsSync(path.join(job, "held")),
      "recovery pause after stale held is gone",
    );
    assert.equal(readFileSync(path.join(job, "pid"), "utf8").trim(), "999998");
    const claimant = start(["recover", posix(workdir), "12s", "printf", "claimant-b\\n"], {
      cwd: root,
      env,
    });
    const claimantDone = collect(claimant);
    const holdUntil = Date.now() + 1_000;
    while (Date.now() < holdUntil) {
      assert.equal(existsSync(path.join(job, "held")), false, "claimant published during recovery");
      assert.equal(existsSync(path.join(job, "recovering")), true);
      assert.equal(readFileSync(path.join(job, "pid"), "utf8").trim(), "999998");
      assert.equal(claimant.exitCode, null);
      await sleep(50);
    }
    await waitUntil(
      () => !existsSync(path.join(job, "recovering")) && existsSync(path.join(job, "held")),
      "reclaimer published held",
    );
    const owner = readFileSync(path.join(job, "held"), "utf8").trim();
    assert.match(owner, /^[1-9][0-9]*$/);
    assert.notEqual(owner, "999999");
    await waitUntil(() => {
      if (!existsSync(path.join(job, "pid"))) return false;
      const child = readFileSync(path.join(job, "pid"), "utf8").trim();
      return /^[1-9][0-9]*$/.test(child) && child !== "999998";
    }, "accepted child pid");
    const acceptedPid = readFileSync(path.join(job, "pid"), "utf8").trim();
    await sleep(300);
    assert.equal(readFileSync(path.join(job, "pid"), "utf8").trim(), acceptedPid);
    assert.equal(readFileSync(path.join(job, "held"), "utf8").trim(), owner);
    await waitUntil(() => {
      try {
        return readFileSync(path.join(job, "log"), "utf8").includes("reclaimer-a");
      } catch {
        return false;
      }
    }, "accepted log");
    const results = await Promise.all([reclaimerDone, claimantDone]);
    assert.equal(results[0].status, 0, `${results[0].stdout}${results[0].stderr}`);
    assert.equal(results[1].status, 3, `${results[1].stdout}${results[1].stderr}`);
    const log = readFileSync(path.join(job, "log"), "utf8");
    const state = readFileSync(path.join(job, "state.json"), "utf8");
    assert.match(log, /reclaimer-a/);
    assert.doesNotMatch(log, /claimant-b/);
    assert.doesNotMatch(log, /old-stale-log/);
    assert.match(state, /^ok /);
    assert.doesNotMatch(state, /stopped|stale/);
    assert.equal(existsSync(path.join(job, "pid")), false);
    assert.equal(existsSync(path.join(job, "held")), false);
    assert.equal(existsSync(path.join(job, "stop-requested")), false);
  });
});
