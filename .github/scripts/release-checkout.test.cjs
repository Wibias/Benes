const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");

const workflowPath = path.join(__dirname, "..", "workflows", "release.yml");

function readWorkflow() {
  return fs.readFileSync(workflowPath, "utf8");
}

test("write-capable release checkout does not persist GitHub credentials", () => {
  const workflow = readWorkflow();
  const publish = workflow.split(/^  publish:/m)[1];
  assert.ok(publish, "publish job missing");
  const checkout = publish.split(/- name: Checkout/)[1];
  assert.ok(checkout, "publish Checkout step missing");
  const step = checkout.split(/- name:/)[0];
  assert.match(step, /persist-credentials:\s*false/);
  assert.match(step, /fetch-depth:\s*0/);
});

test("release workflow is dispatch-only with a serial concurrency group", () => {
  const workflow = readWorkflow();
  assert.match(workflow, /^on:\s*$/m);
  assert.match(workflow, /^\s+workflow_dispatch:\s*$/m);
  assert.doesNotMatch(workflow, /^\s+push:\s*$/m);
  assert.doesNotMatch(workflow, /^\s+pull_request:\s*$/m);
  assert.match(workflow, /^permissions:\s*\{\}\s*$/m);
  assert.match(workflow, /group:\s*release/);
  assert.match(workflow, /cancel-in-progress:\s*false/);
});

test("validate-dispatch loads the default-branch guard and inspects the trigger", () => {
  const workflow = readWorkflow();
  const job = workflow.split(/^  validate-dispatch:/m)[1];
  assert.ok(job, "validate-dispatch job missing");
  const header = job.split(/^  [A-Za-z]/m)[0] + job.split(/^  publish:/m)[0];
  assert.match(header, /ref:\s*\$\{\{\s*github\.event\.repository\.default_branch\s*\}\}/);
  assert.match(header, /persist-credentials:\s*false/);
  assert.match(header, /inspectReleaseTrigger/);
  assert.match(header, /event:\s*process\.env\.GITHUB_EVENT_NAME/);
  assert.match(header, /ref:\s*process\.env\.GITHUB_REF/);
  assert.match(header, /auditedSha:\s*process\.env\.EXPECTED_SHA/);
  assert.match(header, /observedSha:\s*process\.env\.GITHUB_SHA/);
});

test("release uses stage-only OIDC with a pinned staged-publishing-capable npm", () => {
  const workflow = readWorkflow();
  const publish = workflow.split(/^  publish:/m)[1];
  assert.ok(publish, "publish job missing");
  assert.match(publish, /id-token:\s*write/);
  assert.match(publish, /registry-url:\s*"https:\/\/registry\.npmjs\.org"/);
  assert.match(publish, /npm install --global npm@12\.0\.2/);
  assert.doesNotMatch(publish, /npm install --global npm@(latest|\^)/);
  assert.doesNotMatch(publish, /NPM_TOKEN/);
  assert.match(publish, /unset NODE_AUTH_TOKEN/);
  assert.match(publish, /npm ci/);
  assert.match(workflow, /^\s{6}dry-run:\s*$/m);
  assert.match(workflow, /inputs\.dry-run/);
  assert.match(workflow, /default:\s*true/);
  assert.match(publish, /scripts\/release\.ts publish/);
  assert.doesNotMatch(publish, /npm publish/);
});
