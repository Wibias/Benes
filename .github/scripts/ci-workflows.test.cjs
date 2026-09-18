"use strict";

const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const ROOT = path.join(__dirname, "..", "..");

function readWorkflow(rel) {
  return fs.readFileSync(path.join(ROOT, rel), "utf8");
}

function jobEntries(workflow) {
  const jobsMatch = workflow.match(/^jobs:\n([\s\S]*)$/m);
  assert.ok(jobsMatch, "workflow must declare jobs");
  const names = [];
  const lines = jobsMatch[1].split("\n");
  for (let i = 0; i < lines.length; i += 1) {
    const match = /^  ([A-Za-z][A-Za-z0-9_-]*):\s*$/.exec(lines[i]);
    if (!match) continue;
    const body = [];
    for (let j = i + 1; j < lines.length; j += 1) {
      if (/^  [A-Za-z][A-Za-z0-9_-]*:\s*$/.test(lines[j])) break;
      body.push(lines[j]);
    }
    names.push({ name: match[1], body: body.join("\n") });
  }
  return names;
}

function jobMap(workflow) {
  return Object.fromEntries(jobEntries(workflow).map((job) => [job.name, job.body]));
}

function parseNeeds(body) {
  const line = body.split("\n").find((candidate) => /^\s+needs:/.test(candidate));
  assert.ok(line, "job must declare needs");
  const bracket = line.match(/needs:\s*\[([^\]]+)\]/);
  if (bracket) {
    return bracket[1].split(",").map((item) => item.trim()).filter(Boolean);
  }
  const scalar = line.match(/needs:\s*(\S+)/);
  return scalar ? [scalar[1]] : [];
}

function timeoutMinutes(body) {
  const match = body.match(/^\s+timeout-minutes:\s+(\d+)\s*$/m);
  return match ? Number(match[1]) : undefined;
}

function setupNodeVersions(workflow) {
  const versions = [];
  const lines = workflow.split("\n");
  for (let i = 0; i < lines.length; i += 1) {
    if (!/uses:\s+actions\/setup-node@/.test(lines[i])) continue;
    const window = lines.slice(i, i + 8).join("\n");
    const match = window.match(/node-version:\s*['"]?(\d+)/);
    if (match) versions.push(Number(match[1]));
  }
  return versions;
}

describe("CI", () => {
  const ci = readWorkflow(".github/workflows/ci.yml");
  const goCore = readWorkflow(".github/workflows/go-core.yml");
  const jobs = jobMap(ci);

  it("classifies hosted CI with the same local path buckets", () => {
    assert.match(ci, /^ {2}pull_request:\s*\{\}\s*$/m);
    assert.doesNotMatch(ci, /dorny\/paths-filter/);
    assert.ok(jobs.changes, "ci.yml must classify paths before producers run");
    assert.match(jobs.changes, /node --experimental-strip-types scripts\/ci-pr-scope\.ts/);
    assert.match(jobs.linux, /^\s+needs:\s+changes\s*$/m);
    assert.match(jobs["go-core"], /^\s+needs:\s+changes\s*$/m);
    assert.match(jobs.credentials, /^\s+needs:\s+changes\s*$/m);
    assert.match(jobs.package, /^\s+needs:\s+changes\s*$/m);
    assert.deepEqual(Object.keys(jobs).sort(), [
      "changes",
      "ci",
      "credentials",
      "go-core",
      "linux",
      "package",
    ]);
    const versions = setupNodeVersions(ci);
    assert.ok(versions.length >= 3, "ci.yml must set up Node in linux, credentials, and package");
    assert.ok(
      versions.every((version) => version >= 22),
      `Node versions must be 22+: ${versions.join(",")}`,
    );
    assert.match(jobs.linux, /node --experimental-strip-types scripts\/privacy-scan\.ts/);
    assert.match(jobs.credentials, /node --experimental-strip-types scripts\/keyring-smoke\.ts/);
  });

  it("folds go-core into the aggregate ci check", () => {
    assert.ok(jobs["go-core"], "ci.yml must have a go-core job");
    assert.match(jobs["go-core"], /uses:\s+\.\/\.github\/workflows\/go-core\.yml/);
    const goCoreLines = goCore.split(/\r?\n/);
    const onLine = goCoreLines.indexOf("on:");
    const workflowCallLine = goCoreLines.indexOf("  workflow_call:");
    assert.ok(onLine >= 0 && workflowCallLine === onLine + 1, "go-core.yml must expose workflow_call under on");
    assert.match(goCore, /^\s+run:\s+go test \.\/\.\.\.\s*$/m);
    assert.match(goCore, /^\s+run:\s+go vet \.\/\.\.\.\s*$/m);
    assert.match(goCore, /^\s+run:\s+go test -race \.\/\.\.\.\s*$/m);
    assert.doesNotMatch(goCore, /^  pull_request:/m);
    assert.match(goCore, /go-version-file:\s+go\.mod/);
  });

  it("requires the classifier to succeed and treats skipped producers as pass", () => {
    const gate = jobs.ci;
    assert.ok(gate, "aggregate ci job missing");
    assert.match(gate, /^\s+if:\s+always\(\)\s*$/m);
    const needs = parseNeeds(gate).sort();
    const producers = Object.keys(jobs).filter((name) => name !== "ci").sort();
    assert.deepEqual(needs, ["changes", "credentials", "go-core", "linux", "package"]);
    assert.deepEqual(needs, producers);
    assert.match(gate, /\.changes\.result == "success"/);
    assert.match(gate, /\.value\.result != "skipped"/);
    assert.match(gate, /skipped/);
    assert.doesNotMatch(gate, /\.linux\.result == "success"/);
  });

  it("bounds every local job; reusable jobs pin go-core.yml instead", () => {
    for (const [name, body] of Object.entries(jobs)) {
      if (/^\s+uses:\s+\.\//m.test(body)) {
        assert.equal(timeoutMinutes(body), undefined, `${name} reusable job cannot set timeout-minutes`);
        continue;
      }
      assert.equal(typeof timeoutMinutes(body), "number", `${name} must set timeout-minutes`);
    }
    assert.equal(timeoutMinutes(jobMap(goCore).test), 15);
  });

  it("keeps GUI lint/build, CLI help, and packaging smokes", () => {
    assert.match(jobs.linux, /npm run lint/);
    assert.match(jobs.linux, /npm run build/);
    assert.match(jobs.linux, /go run \.\/cmd\/benes help/);
    assert.match(jobs.linux, /node \.github\/scripts\/run-automation-tests\.cjs/);
    assert.match(jobs.linux, /needs\.changes\.outputs\.gui/);
    assert.match(jobs.package, /node \.github\/scripts\/npm-pack-json\.cjs verify pack\.json/);
    assert.match(jobs.package, /npm install -g/);
    assert.match(jobs.package, /benes help/);
  });

  it("pins checkout and setup-node to immutable SHAs", () => {
    assert.match(ci, /actions\/checkout@9c091bb21b7c1c1d1991bb908d89e4e9dddfe3e0/);
    assert.match(ci, /actions\/setup-node@48b55a011bda9f5d6aeb4c2d9c7362e8dae4041e/);
    assert.match(ci, /actions\/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e/);
    assert.doesNotMatch(ci, /uses:\s+\S+@(?:v\d+|main|master)\b/);
  });
});

const WORKFLOW_DIR = path.join(ROOT, ".github", "workflows");
const CHECKOUT_SHA = "9c091bb21b7c1c1d1991bb908d89e4e9dddfe3e0";
const SETUP_NODE_SHA = "48b55a011bda9f5d6aeb4c2d9c7362e8dae4041e";
const SETUP_GO_SHA = "b7ad1dad31e06c5925ef5d2fc7ad053ef454303e";
const GITHUB_SCRIPT_SHA = "3a2844b7e9c422d3c10d287c895573f7108da1b3";
const STALE_SHA = "1e223db275d687790206a7acac4d1a11bd6fe629";
const DEPLOY_PAGES_SHA = "cd2ce8fcbc39b97be8ca5fce6e763baed58fa128";
const UPLOAD_PAGES_SHA = "7b1f4a764d45c48632c6b24a0339c27f5614fb0b";

const TRUSTED_MUTATION_WORKFLOWS = new Set([
  "enforce-pr-target.yml",
  "pr-hygiene.yml",
  "pr-labeler.yml",
]);

function workflowFiles() {
  return fs.readdirSync(WORKFLOW_DIR).filter((name) => name.endsWith(".yml")).sort();
}

function actionPins(workflow) {
  return [...workflow.matchAll(/uses:\s+(?!.*\/\.github\/)(\S+?)@([0-9a-f]{40}|[^\s]+)/g)].map(
    (match) => ({ action: match[1], ref: match[2] }),
  );
}

describe("Non-release workflow corpus", () => {
  const files = workflowFiles();

  it("audits the current workflow set and leaves release.yml in place", () => {
    assert.deepEqual(files, [
      "ci.yml",
      "cleanup-orphaned-workflows.yml",
      "deploy-docs.yml",
      "enforce-issue-quality.yml",
      "enforce-pr-target.yml",
      "go-core.yml",
      "issue-quality-tests.yml",
      "issue-translation.yml",
      "issue-triage.yml",
      "pr-hygiene.yml",
      "pr-labeler.yml",
      "release.yml",
      "service-lifecycle.yml",
      "stale-needs-info.yml",
    ]);
    assert.match(readWorkflow(".github/workflows/release.yml"), /^name:\s*Release\s*$/m);
    assert.equal(fs.existsSync(path.join(WORKFLOW_DIR, "react-doctor.yml")), false);
    assert.equal(fs.existsSync(path.join(WORKFLOW_DIR, "codeql.yml")), false);
  });

  it("pins every third-party action to a 40-character SHA", () => {
    for (const name of files) {
      if (name === "release.yml") continue;
      const workflow = readWorkflow(`.github/workflows/${name}`);
      const pins = actionPins(workflow);
      assert.ok(pins.length > 0, `${name} must use at least one action`);
      for (const pin of pins) {
        assert.match(
          pin.ref,
          /^[0-9a-f]{40}$/,
          `${name} must pin ${pin.action} to a full SHA, got ${pin.ref}`,
        );
      }
      assert.doesNotMatch(workflow, /uses:\s+\S+@(?:v\d+|main|master)\b/);
    }
  });

  it("does not use pull_request_target except on trusted mutation gates", () => {
    for (const name of files) {
      if (name === "release.yml") continue;
      const workflow = readWorkflow(`.github/workflows/${name}`);
      if (TRUSTED_MUTATION_WORKFLOWS.has(name)) {
        assert.match(workflow, /^ {2}pull_request_target:\s*$/m);
        continue;
      }
      assert.doesNotMatch(workflow, /pull_request_target:/, `${name} must not use pull_request_target`);
    }
  });

  it("keeps persist-credentials off for every checkout", () => {
    for (const name of files) {
      if (name === "release.yml") continue;
      const workflow = readWorkflow(`.github/workflows/${name}`);
      const checkouts = [...workflow.matchAll(/uses:\s+actions\/checkout@[\s\S]*?(?=\n(?: {2,6}- |\S|$))/g)];
      if (name === "stale-needs-info.yml") {
        assert.equal(checkouts.length, 0, "stale workflow must not check out the repository");
        continue;
      }
      assert.ok(checkouts.length > 0, `${name} must check out the repository`);
      for (const checkout of checkouts) {
        assert.match(
          checkout[0],
          /persist-credentials:\s*false/,
          `${name} checkout must set persist-credentials: false`,
        );
      }
    }
  });

  it("does not give untrusted pull_request jobs secrets or write tokens", () => {
    for (const name of ["ci.yml", "go-core.yml", "issue-quality-tests.yml", "service-lifecycle.yml"]) {
      const workflow = readWorkflow(`.github/workflows/${name}`);
      assert.match(workflow, /contents:\s*read/);
      assert.doesNotMatch(workflow, /secrets\./);
      assert.doesNotMatch(workflow, /contents:\s*write/);
      assert.doesNotMatch(workflow, /pull-requests:\s*write/);
      assert.doesNotMatch(workflow, /issues:\s*write/);
    }
  });
});

describe("Idle needs-info issues", () => {
  const workflow = readWorkflow(".github/workflows/stale-needs-info.yml");

  it("is schedule-only and does not process pull requests", () => {
    assert.match(workflow, /^ {2}schedule:\s*$/m);
    assert.match(workflow, /cron:\s*"42 5 \* \* \*"/);
    assert.doesNotMatch(workflow, /^ {2}workflow_dispatch:/m);
    assert.doesNotMatch(workflow, /^ {2}pull_request:/m);
    assert.match(workflow, /only-issue-labels:\s*needs-info/);
    assert.match(workflow, /days-before-pr-stale:\s*-1/);
    assert.match(workflow, /days-before-pr-close:\s*-1/);
    assert.match(workflow, /days-before-issue-stale:\s*14/);
    assert.match(workflow, /days-before-issue-close:\s*7/);
    assert.match(workflow, /stale-issue-label:\s*stale/);
    assert.match(workflow, /remove-stale-when-updated:\s*true/);
  });

  it("uses the standard stale action and a one-line label bootstrap", () => {
    assert.match(workflow, new RegExp(`actions/stale@${STALE_SHA}`));
    assert.match(workflow, /gh label create stale/);
    assert.doesNotMatch(workflow, /actions\/github-script@/);
    assert.match(workflow, /timeout-minutes:\s*10/);
    assert.match(workflow, /issues:\s*write/);
  });
});

describe("Docs Pages publish", () => {
  const workflow = readWorkflow(".github/workflows/deploy-docs.yml");

  it("builds the Starlight app from docs/ with repository npm commands", () => {
    assert.match(workflow, /branches:\s*\[main\]/);
    assert.match(workflow, /docs\/\*\*/);
    assert.match(workflow, /working-directory:\s*docs/);
    assert.match(workflow, /npm ci/);
    assert.match(workflow, /npm run build/);
    assert.doesNotMatch(workflow, /withastro\/action/);
    assert.doesNotMatch(workflow, /docs-site/);
    assert.doesNotMatch(workflow, /setup-bun|package-manager:\s*bun/);
  });

  it("uses official Pages artifact deploy with least privilege and a serial group", () => {
    const jobs = jobMap(workflow);
    assert.match(workflow, /^permissions:\n {2}contents: read\s*$/m);
    assert.doesNotMatch(workflow, /^ {2}workflow_dispatch:/m);
    assert.match(jobs.build, /^\s+ref:\s*main\s*$/m);
    assert.doesNotMatch(jobs.build, /pages:\s*write/);
    assert.doesNotMatch(jobs.build, /id-token:\s*write/);
    assert.match(jobs.deploy, /pages:\s*write/);
    assert.match(jobs.deploy, /id-token:\s*write/);
    assert.match(jobs.deploy, /contents:\s*read/);
    assert.doesNotMatch(workflow, /actions\/configure-pages/);
    assert.match(workflow, new RegExp(`actions/upload-pages-artifact@${UPLOAD_PAGES_SHA}`));
    assert.match(workflow, new RegExp(`actions/deploy-pages@${DEPLOY_PAGES_SHA}`));
    assert.match(workflow, /path:\s*docs\/dist/);
    assert.match(jobs.deploy, /name:\s*github-pages/);
    assert.match(workflow, /cancel-in-progress:\s*false/);
  });
});

describe("Orphaned workflow cleanup", () => {
  const workflow = readWorkflow(".github/workflows/cleanup-orphaned-workflows.yml");

  it("is a thin Node wrapper around the repository cleanup command", () => {
    assert.match(workflow, /cron:\s*"17 4 \* \* \*"/);
    assert.doesNotMatch(workflow, /^ {2}workflow_dispatch:/m);
    assert.match(workflow, /branches:\s*\[main\]/);
    assert.match(workflow, /actions:\s*write/);
    assert.match(workflow, /contents:\s*read/);
    assert.match(workflow, /node-version:\s*24/);
    assert.match(workflow, /node --experimental-strip-types scripts\/ci\/cleanup-orphaned-workflows\.ts/);
    assert.doesNotMatch(workflow, /setup-bun|bun scripts\//);
    assert.match(workflow, new RegExp(`actions/checkout@${CHECKOUT_SHA}`));
    assert.match(workflow, new RegExp(`actions/setup-node@${SETUP_NODE_SHA}`));
    assert.match(workflow, /timeout-minutes:\s*10/);
  });
});

describe("PR target gate", () => {
  const workflow = readWorkflow(".github/workflows/enforce-pr-target.yml");
  const jobs = jobMap(workflow);

  it("loads trusted scripts only and keeps the enforce-target check name", () => {
    assert.ok(jobs["enforce-target"], "required check name enforce-target must remain");
    assert.match(workflow, /^permissions:\s*\{\}\s*$/m);
    assert.match(jobs["enforce-target"], /contents:\s*write/);
    assert.match(jobs["enforce-target"], /pull-requests:\s*write/);
    assert.match(workflow, /pr-quality-resolve\.cjs/);
    assert.match(workflow, /pr-quality-run\.cjs/);
    assert.match(workflow, new RegExp(`actions/checkout@${CHECKOUT_SHA}`));
    assert.match(workflow, new RegExp(`actions/github-script@${GITHUB_SCRIPT_SHA}`));
    assert.doesNotMatch(workflow, /ref:\s*\$\{\{\s*github\.event\.pull_request\.head/);
  });

  it("enforces from the resolved PR trusted-ref, not the event type", () => {
    assert.match(jobs.resolve, /trusted-ref:/);
    assert.match(
      jobs.resolve,
      /ref:\s*\$\{\{\s*github\.event_name == 'pull_request_target' && \(github\.event\.pull_request\.base\.ref == 'main' && 'main' \|\| 'dev'\) \|\| 'dev'\s*\}\}/,
    );
    assert.match(jobs["enforce-target"], /needs\.resolve\.outputs\.trusted-ref == 'main'/);
    assert.match(jobs["enforce-target"], /needs\.resolve\.outputs\.trusted-ref == 'dev'/);
    assert.match(
      jobs["enforce-target"],
      /ref:\s*\$\{\{\s*needs\.resolve\.outputs\.trusted-ref\s*\}\}/,
    );
    assert.doesNotMatch(jobs["enforce-target"], /default_branch/);
    assert.doesNotMatch(jobs["enforce-target"], /pull_request\.head/);
    assert.doesNotMatch(workflow, /github\.event\.pull_request\.base\.sha/);
    assert.doesNotMatch(
      workflow,
      /ref:\s*\$\{\{\s*github\.event\.pull_request\.base\.ref\s*\}\}/,
    );
  });
});

describe("Issue automation orchestration", () => {
  const quality = readWorkflow(".github/workflows/enforce-issue-quality.yml");
  const translation = readWorkflow(".github/workflows/issue-translation.yml");
  const triage = readWorkflow(".github/workflows/issue-triage.yml");
  const tests = readWorkflow(".github/workflows/issue-quality-tests.yml");

  it("keeps mutation on default-branch scripts with issues:write only", () => {
    for (const workflow of [quality, translation, triage]) {
      assert.match(workflow, /^permissions:\s*\{\}\s*$/m);
      assert.match(workflow, /contents:\s*read/);
      assert.match(workflow, /issues:\s*write/);
      assert.doesNotMatch(workflow, /pull-requests:\s*write/);
      assert.match(workflow, /ref:\s*\$\{\{\s*github\.event\.repository\.default_branch\s*\}\}/);
      assert.match(workflow, new RegExp(`actions/checkout@${CHECKOUT_SHA}`));
      assert.match(workflow, /persist-credentials:\s*false/);
      assert.match(workflow, /cancel-in-progress:\s*false/);
    }
    assert.match(quality, /issue-quality-run\.cjs/);
    assert.match(quality, /issue-quality-backfill\.cjs/);
    assert.match(translation, /issue-translation-run\.cjs/);
    assert.match(triage, /issue-triage-run\.cjs/);
  });

  it("runs repository automation tests with contents:read only", () => {
    assert.match(tests, /^permissions:\n {2}contents: read\s*$/m);
    assert.match(tests, /node \.github\/scripts\/run-automation-tests\.cjs/);
    assert.match(tests, new RegExp(`actions/checkout@${CHECKOUT_SHA}`));
    assert.match(tests, /node-version:\s*24/);
    assert.doesNotMatch(tests, /issues:\s*write/);
  });
});

describe("PR automation orchestration", () => {
  const hygiene = readWorkflow(".github/workflows/pr-hygiene.yml");
  const labeler = readWorkflow(".github/workflows/pr-labeler.yml");

  it("serializes hygiene with the target gate comment and never checks out PR head", () => {
    assert.match(hygiene, /^permissions:\s*\{\}\s*$/m);
    assert.match(hygiene, /group:\s*pr-gate-comment-\$\{\{\s*github\.event\.pull_request\.number\s*\}\}/);
    assert.match(hygiene, /cancel-in-progress:\s*false/);
    assert.match(hygiene, /pr-hygiene-run\.cjs/);
    assert.match(hygiene, /runPrHygieneGate/);
    assert.match(
      hygiene,
      /ref:\s*\$\{\{\s*github\.event\.pull_request\.base\.ref == 'main' && 'main' \|\| 'dev'\s*\}\}/,
    );
    assert.doesNotMatch(hygiene, /ref:\s*\$\{\{\s*github\.event\.pull_request\.head/);
  });

  it("labels from the same trusted integration ref as hygiene", () => {
    const trustedRef =
      /ref:\s*\$\{\{\s*github\.event\.pull_request\.base\.ref == 'main' && 'main' \|\| 'dev'\s*\}\}/;
    assert.match(labeler, /pr-labeler-run\.cjs/);
    assert.match(labeler, /runPrLabeler/);
    assert.match(labeler, trustedRef);
    assert.match(hygiene, trustedRef);
    assert.doesNotMatch(labeler, /default_branch/);
    assert.doesNotMatch(labeler, /pull_request\.head/);
    assert.doesNotMatch(
      labeler,
      /ref:\s*\$\{\{\s*github\.event\.pull_request\.base\.ref\s*\}\}/,
    );
    assert.doesNotMatch(
      hygiene,
      /ref:\s*\$\{\{\s*github\.event\.pull_request\.base\.ref\s*\}\}/,
    );
    assert.match(labeler, /contents:\s*read/);
    assert.match(labeler, /pull-requests:\s*write/);
    assert.match(labeler, /issues:\s*write/);
    assert.doesNotMatch(labeler, /contents:\s*write/);
    assert.match(labeler, /cancel-in-progress:\s*true/);
    assert.match(labeler, new RegExp(`actions/checkout@${CHECKOUT_SHA}`));
  });
});

describe("Service lifecycle proof", () => {
  const workflow = readWorkflow(".github/workflows/service-lifecycle.yml");
  const jobs = jobMap(workflow);

  it("proves install, health, stop, and uninstall on Linux, macOS, and Windows", () => {
    assert.ok(jobs.linux);
    assert.ok(jobs.macos);
    assert.ok(jobs.windows);
    assert.match(workflow, /pull_request:\s*\n(?:[ \t].*\n)*?[ \t]+paths:/);
    assert.doesNotMatch(workflow, /pull_request:[\s\S]*branches:\s*\[main, dev\]/);
    assert.match(workflow, /contents:\s*read/);
    assert.doesNotMatch(workflow, /secrets\./);
    for (const name of ["linux", "macos", "windows"]) {
      assert.match(jobs[name], /benes(?:\.exe)? service install/);
      assert.match(jobs[name], /benes(?:\.exe)? stop/);
      assert.match(jobs[name], /benes(?:\.exe)? service uninstall/);
      assert.match(jobs[name], /127\.0\.0\.1:23100\/healthz/);
      assert.equal(timeoutMinutes(jobs[name]), 10);
    }
    assert.match(jobs.linux, /Restart=on-failure|kill -9/);
    assert.match(jobs.linux, /systemctl --user/);
    assert.match(jobs.macos, /launchctl/);
    assert.match(jobs.windows, /Get-ScheduledTask/);
  });
});

describe("React Doctor stays least-privilege", () => {
  it("does not ship a dedicated GitHub workflow", () => {
    assert.equal(
      fs.existsSync(path.join(ROOT, ".github/workflows/react-doctor.yml")),
      false,
    );
  });

  it("loads the oxlint plugin from the GUI config", () => {
    const oxlintrc = fs.readFileSync(path.join(ROOT, "gui/.oxlintrc.json"), "utf8");
    assert.match(oxlintrc, /"name":\s*"react-doctor"/);
    assert.match(oxlintrc, /"specifier":\s*"oxlint-plugin-react-doctor"/);
  });

  it("does not run a doctor sidecar on prepush", () => {
    const pkg = JSON.parse(fs.readFileSync(path.join(ROOT, "package.json"), "utf8"));
    assert.doesNotMatch(pkg.scripts.prepush, /doctor:gui/);
    assert.equal(pkg.scripts["doctor:gui"], undefined);
    assert.equal(pkg.scripts["doctor:gui:full"], undefined);
    assert.equal(pkg.scripts["doctor:gui:if-changed"], undefined);
    assert.equal(fs.existsSync(path.join(ROOT, "scripts/doctor-gui-if-changed.ts")), false);
  });
});
