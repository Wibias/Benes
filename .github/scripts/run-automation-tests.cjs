"use strict";

/**
 * Canonical local entrypoint for repository-automation tests.
 * Workflows and package scripts should call this instead of listing files in YAML.
 */

const { spawnSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");

const SCRIPTS_DIR = __dirname;
const ROOT = path.join(__dirname, "..", "..");

function listAutomationTests() {
  return fs
    .readdirSync(SCRIPTS_DIR)
    .filter((file) => file.endsWith(".test.cjs"))
    .sort()
    .map((file) => path.join(SCRIPTS_DIR, file));
}

function run(args) {
  const result = spawnSync(process.execPath, args, {
    stdio: "inherit",
    cwd: ROOT,
  });
  if (result.error) {
    console.error(result.error);
    process.exit(1);
  }
  if (result.status) process.exit(result.status);
}

function main() {
  const tests = listAutomationTests();
  if (tests.length === 0) {
    console.error("No .github/scripts/*.test.cjs files found.");
    process.exit(1);
  }
  run(["--test", ...tests]);
  run([
    "--experimental-strip-types",
    "--test",
    path.join(ROOT, "scripts", "benes-run.test.ts"),
    path.join(ROOT, "scripts", "ci", "cleanup-orphaned-workflows.test.ts"),
  ]);
  run([
    "--experimental-strip-types",
    "--test",
    path.join(ROOT, "scripts", "ci-pr-scope.test.ts"),
    path.join(ROOT, "scripts", "local-pr.forward.test.ts"),
    path.join(ROOT, "scripts", "privacy-scan.test.ts"),
    path.join(ROOT, "scripts", "gui-change-scripts.test.ts"),
    path.join(ROOT, "scripts", "win-exec.test.ts"),
    path.join(ROOT, "scripts", "keyring-smoke.test.ts"),
    path.join(ROOT, "scripts", "lib", "release", "semver.test.ts"),
    path.join(ROOT, "scripts", "lib", "release", "identity.test.ts"),
    path.join(ROOT, "scripts", "lib", "release", "notes.test.ts"),
    path.join(ROOT, "scripts", "lib", "release", "args.test.ts"),
    path.join(ROOT, "scripts", "lib", "release", "plan.test.ts"),
    path.join(ROOT, "scripts", "lib", "release", "execute.test.ts"),
    path.join(ROOT, "scripts", "lib", "release", "security.test.ts"),
  ]);

  const forms = require("./validate-issue-forms.cjs");
  forms.main();
}

module.exports = { listAutomationTests, main };

if (require.main === module) {
  main();
}
