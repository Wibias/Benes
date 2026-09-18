"use strict";

const { describe, it, before, after } = require("node:test");
const assert = require("node:assert/strict");
const { runPrQualityGate, fingerprintsDiffer, prFingerprint } = require("./pr-quality-run.cjs");

const HEAD_A = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
const HEAD_B = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";
const BODY = [
  "## Summary",
  "Keep combo failover on the first visible stream event and hop HTTP 410.",
  "",
  "## Verification",
  "- go test ./internal/combo",
].join("\n");
const READY_BODY = `${BODY}

<!-- pr-quality-readiness-checklist:start -->
## Review readiness checklist
- [x] All CI tests are green on my local testing.
- [x] I pushed my PR to the latest dev commit.
- [x] I resolved all correct Codex and CodeRabbit findings.
- [x] My PR is ready for review.
<!-- pr-quality-readiness-checklist:end -->
`;

const originalPull = process.env.RESOLVED_PULL_NUMBER;

before(() => {
  process.env.RESOLVED_PULL_NUMBER = "158";
});

after(() => {
  if (originalPull === undefined) delete process.env.RESOLVED_PULL_NUMBER;
  else process.env.RESOLVED_PULL_NUMBER = originalPull;
});

function coreSpy() {
  const info = [];
  const failed = [];
  return {
    info: (message) => info.push(String(message)),
    warning: () => {},
    setFailed: (message) => failed.push(String(message)),
    infoMessages: info,
    failedMessages: failed,
  };
}

function mockGithub({ pr, liveFromGet = 4, livePr, files = [], blobs = {} }) {
  let gets = 0;
  const updates = [];
  const comments = [];
  const labels = [];
  const drafts = [];
  const github = {
    paginate: async (fn, args) => {
      if (fn === github.rest.pulls.listFiles) return files;
      if (fn === github.rest.issues.listComments) return [];
      if (fn === github.rest.issues.listEvents) return [];
      if (fn === github.rest.pulls.list) return [];
      return [];
    },
    rest: {
      git: {
        getBlob: async ({ file_sha }) => {
          const text = blobs[file_sha];
          if (text == null) throw new Error(`missing blob ${file_sha}`);
          return {
            data: {
              encoding: "base64",
              content: Buffer.from(text, "utf8").toString("base64"),
            },
          };
        },
      },
      pulls: {
        get: async () => {
          gets += 1;
          const data = gets >= liveFromGet ? livePr : pr;
          return { data };
        },
        update: async (args) => {
          updates.push(args);
          return { data: args };
        },
        listFiles: async () => ({ data: [] }),
        list: async () => ({ data: [] }),
      },
      repos: {
        getCollaboratorPermissionLevel: async () => ({ data: { permission: "read" } }),
        compareCommitsWithBasehead: async () => ({
          data: { behind_by: 0, ahead_by: 1 },
        }),
      },
      issues: {
        listComments: async () => ({ data: [] }),
        listEvents: async () => ({ data: [] }),
        createComment: async (args) => {
          comments.push(args);
          return { data: { id: 9, body: args.body } };
        },
        updateComment: async (args) => {
          comments.push(args);
          return { data: args };
        },
        addLabels: async (args) => {
          labels.push(["add", args]);
          return { data: [] };
        },
        removeLabel: async (args) => {
          labels.push(["remove", args]);
          return { data: [] };
        },
      },
    },
    graphql: async (query) => {
      drafts.push(String(query));
      return {};
    },
  };
  return { github, gets: () => gets, updates, comments, labels, drafts };
}

function contextFor(pr, action = "opened") {
  return {
    eventName: "pull_request_target",
    repo: { owner: "Wibias", repo: "Benes" },
    payload: { action, pull_request: pr },
  };
}

function basePr(extra = {}) {
  return {
    number: 158,
    title: "fix(combo): hop 410",
    body: BODY,
    draft: false,
    user: { login: "contributor" },
    labels: [],
    base: { ref: "dev", repo: { owner: { login: "Wibias" }, name: "Benes" } },
    head: { sha: HEAD_A, ref: "feat/x" },
    changed_files: 0,
    node_id: "PR_1",
    ...extra,
  };
}

describe("mutable PR freshness guard", () => {
  it("detects body, title, head, and draft drift", () => {
    const observed = prFingerprint(basePr());
    assert.equal(fingerprintsDiffer(observed, prFingerprint(basePr({ body: `${BODY}\nedit` }))), true);
    assert.equal(fingerprintsDiffer(observed, prFingerprint(basePr({ title: "other" }))), true);
    assert.equal(fingerprintsDiffer(observed, prFingerprint(basePr({ head: { sha: HEAD_B } }))), true);
    assert.equal(fingerprintsDiffer(observed, prFingerprint(basePr({ draft: true }))), true);
    assert.equal(fingerprintsDiffer(observed, prFingerprint(basePr())), false);
  });

  it("treats base ref and labels as plan inputs, ignoring label order", () => {
    const observed = prFingerprint(basePr({
      labels: [{ name: "review-ready" }, { name: "test-exception-approved" }],
    }));
    assert.equal(
      fingerprintsDiffer(
        observed,
        prFingerprint(basePr({
          base: { ref: "main", repo: { owner: { login: "Wibias" }, name: "Benes" } },
        })),
      ),
      true,
    );
    assert.equal(
      fingerprintsDiffer(
        observed,
        prFingerprint(basePr({ labels: [{ name: "review-ready" }] })),
      ),
      true,
    );
    assert.equal(
      fingerprintsDiffer(
        observed,
        prFingerprint(basePr({
          labels: [{ name: "test-exception-approved" }, { name: "review-ready" }],
        })),
      ),
      false,
    );
  });

  it("does not apply a planned body mutation after an author edit", async () => {
    const pr = basePr();
    const { github, updates, comments } = mockGithub({
      pr,
      livePr: basePr({ body: `${BODY}\n\nAuthor added a paragraph.` }),
    });
    const core = coreSpy();
    const result = await runPrQualityGate({ github, context: contextFor(pr), core });
    assert.equal(result.skipped, true);
    assert.deepEqual(updates, []);
    assert.deepEqual(comments, []);
    assert.match(core.infoMessages.join("\n"), /skipping mutations/);
  });

  it("does not apply a wrong-base title prefix after an author title edit", async () => {
    const pr = basePr({ base: { ref: "main", repo: { owner: { login: "Wibias" }, name: "Benes" } } });
    const { github, updates } = mockGithub({
      pr,
      livePr: {
        ...pr,
        title: "docs: author retitled before the gate wrote",
      },
    });
    const result = await runPrQualityGate({
      github,
      context: contextFor(pr),
      core: coreSpy(),
    });
    assert.equal(result.skipped, true);
    assert.equal(updates.some((item) => item.title), false);
  });

  it("does not apply a READY transition planned for another head", async () => {
    const pr = basePr({ body: READY_BODY, draft: true });
    const { github, updates, labels, drafts } = mockGithub({
      pr,
      livePr: { ...pr, head: { sha: HEAD_B, ref: "feat/x" } },
    });
    const result = await runPrQualityGate({
      github,
      context: contextFor(pr, "synchronize"),
      core: coreSpy(),
    });
    assert.equal(result.skipped, true);
    assert.deepEqual(updates, []);
    assert.deepEqual(labels, []);
    assert.equal(drafts.some((query) => query.includes("markPullRequestReadyForReview")), false);
  });

  it("does not convert draft after a manual draft-state change", async () => {
    const pr = basePr({
      base: { ref: "main", repo: { owner: { login: "Wibias" }, name: "Benes" } },
      draft: false,
    });
    const { github, drafts } = mockGithub({
      pr,
      livePr: { ...pr, draft: true },
    });
    const result = await runPrQualityGate({
      github,
      context: contextFor(pr),
      core: coreSpy(),
    });
    assert.equal(result.skipped, true);
    assert.equal(drafts.some((query) => query.includes("convertPullRequestToDraft")), false);
  });

  it("does not apply READY mutations after the base moves from dev to main", async () => {
    const pr = basePr({ body: READY_BODY, draft: true });
    const { github, updates, labels, drafts } = mockGithub({
      pr,
      livePr: {
        ...pr,
        base: { ref: "main", repo: { owner: { login: "Wibias" }, name: "Benes" } },
      },
    });
    const result = await runPrQualityGate({
      github,
      context: contextFor(pr, "synchronize"),
      core: coreSpy(),
    });
    assert.equal(result.skipped, true);
    assert.deepEqual(updates, []);
    assert.deepEqual(labels, []);
    assert.equal(drafts.some((query) => query.includes("markPullRequestReadyForReview")), false);
  });

  it("does not apply a wrong-base prefix after the base moves from main to dev", async () => {
    const pr = basePr({
      base: { ref: "main", repo: { owner: { login: "Wibias" }, name: "Benes" } },
    });
    const { github, updates, drafts } = mockGithub({
      pr,
      livePr: basePr(),
    });
    const result = await runPrQualityGate({
      github,
      context: contextFor(pr),
      core: coreSpy(),
    });
    assert.equal(result.skipped, true);
    assert.equal(updates.some((item) => item.title), false);
    assert.equal(drafts.some((query) => query.includes("convertPullRequestToDraft")), false);
  });

  it("does not apply a plan after a hygiene exception label is removed", async () => {
    const files = [{ filename: "internal/router/selector.go", patch: "+func pick() {}", status: "modified" }];
    const pr = basePr({
      body: READY_BODY,
      draft: true,
      changed_files: 1,
      labels: [{ name: "test-exception-approved" }],
    });
    const { github, updates, labels, drafts } = mockGithub({
      pr,
      files,
      livePr: { ...pr, labels: [] },
    });
    const result = await runPrQualityGate({
      github,
      context: contextFor(pr, "synchronize"),
      core: coreSpy(),
    });
    assert.equal(result.skipped, true);
    assert.deepEqual(updates, []);
    assert.deepEqual(labels, []);
    assert.equal(drafts.some((query) => query.includes("markPullRequestReadyForReview")), false);
  });

  it("does not apply a stale screenshot waiver decision after the waiver label changes", async () => {
    const files = [
      { filename: "gui/src/pages/Providers.tsx", patch: "+export function Providers() {}", status: "modified" },
    ];
    const pr = basePr({
      body: READY_BODY,
      draft: true,
      changed_files: 1,
      labels: [{ name: "gui-screenshot-waived" }, { name: "test-exception-approved" }],
    });
    const { github, updates } = mockGithub({
      pr,
      files,
      livePr: { ...pr, labels: [{ name: "test-exception-approved" }] },
    });
    const result = await runPrQualityGate({
      github,
      context: contextFor(pr, "labeled"),
      core: coreSpy(),
    });
    assert.equal(result.skipped, true);
    assert.deepEqual(updates, []);
  });

  it("does not treat label reordering as drift", async () => {
    const pr = basePr({
      labels: [{ name: "review-ready" }, { name: "test-exception-approved" }],
    });
    const reordered = {
      ...pr,
      labels: [{ name: "test-exception-approved" }, { name: "review-ready" }],
    };
    assert.equal(fingerprintsDiffer(prFingerprint(pr), prFingerprint(reordered)), false);
    const { github, updates } = mockGithub({ pr, livePr: reordered });
    const result = await runPrQualityGate({
      github,
      context: contextFor(pr),
      core: coreSpy(),
    });
    assert.notEqual(result?.skipped, true);
    assert.equal(updates.some((item) => item.body), true);
  });
});

describe("runPrQualityGate GUI head hydration", () => {
  const ADD_PROVIDER_HEAD = [
    'import { addProviderKeyConnectEnabled } from "../src/lib/add-provider-form-policy.ts";',
    'import { addProviderModalReducer } from "../src/components/add-provider-modal-reducer.ts";',
    'test("connection", () => {});',
  ].join("\n");
  const TEST_SHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";
  const SOURCE_SHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
  const GUI_BODY = `${BODY}\n\n![ui](https://example.com/providers.png)`;

  it("passes hydrated GUI test contents into hygiene even without gui files on disk", async () => {
    const files = [
      {
        filename: "gui/src/lib/add-provider-form-policy.ts",
        status: "modified",
        sha: SOURCE_SHA,
        patch: "+export function addProviderKeyConnectEnabled() {}",
      },
      {
        filename: "gui/scripts/add-provider-connection.test.ts",
        status: "modified",
        sha: TEST_SHA,
        patch: "@@\n+test('connection', () => {})",
      },
    ];
    const pr = basePr({
      body: GUI_BODY,
      changed_files: 2,
    });
    const { github, comments } = mockGithub({
      pr,
      livePr: pr,
      files,
      blobs: { [TEST_SHA]: ADD_PROVIDER_HEAD },
    });
    const core = coreSpy();
    await runPrQualityGate({ github, context: contextFor(pr), core });
    assert.doesNotMatch(core.failedMessages.join("\n"), /missing_regression_test/);
    assert.doesNotMatch(comments.map((item) => item.body || "").join("\n"), /missing_regression_test/);
  });

  it("still reports missing_regression_test when the hydrated test does not import the source", async () => {
    const files = [
      {
        filename: "gui/src/lib/add-provider-form-policy.ts",
        status: "modified",
        sha: SOURCE_SHA,
        patch: "+export function addProviderKeyConnectEnabled() {}",
      },
      {
        filename: "gui/scripts/models-groups.test.ts",
        status: "modified",
        sha: TEST_SHA,
        patch: "@@\n+test('models', () => {})",
      },
    ];
    const pr = basePr({
      body: GUI_BODY,
      changed_files: 2,
    });
    const { github } = mockGithub({
      pr,
      livePr: pr,
      files,
      blobs: {
        [TEST_SHA]: 'import { buildProviderModelGroups } from "../src/models-groups.ts";\n',
      },
    });
    const core = coreSpy();
    await runPrQualityGate({ github, context: contextFor(pr), core });
    assert.match(core.failedMessages.join("\n"), /missing_regression_test/);
  });
});
