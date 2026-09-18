"use strict";

const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const ROOT = path.join(__dirname, "..", "..");
const SETUP_NODE_SHA = "48b55a011bda9f5d6aeb4c2d9c7362e8dae4041e";
const COPILOT_VERSION = "1.0.86";

function readWorkflow(name) {
  return fs.readFileSync(path.join(ROOT, ".github", "workflows", name), "utf8");
}

describe("Copilot issue automation workflows", () => {
  for (const name of ["issue-translation.yml", "issue-triage.yml"]) {
    it(`${name} installs pinned Copilot CLI and exposes only the dedicated token`, () => {
      const workflow = readWorkflow(name);
      assert.match(workflow, new RegExp(`actions/setup-node@${SETUP_NODE_SHA}`));
      assert.match(
        workflow,
        new RegExp(`npm install --global @github/copilot@${COPILOT_VERSION.replaceAll(".", "\\.")}`),
      );
      assert.match(
        workflow,
        /COPILOT_GITHUB_TOKEN:\s*\$\{\{\s*secrets\.COPILOT_GITHUB_TOKEN\s*\}\}/,
      );
      assert.equal((workflow.match(/COPILOT_GITHUB_TOKEN:/g) || []).length, 1);
      assert.doesNotMatch(workflow, /BENES_ISSUE_AI_TOKEN/);
      assert.doesNotMatch(workflow, /BENES_ISSUE_AI_BASE_URL/);
      assert.doesNotMatch(workflow, /BENES_ISSUE_AI_MODEL/);
    });
  }
});
