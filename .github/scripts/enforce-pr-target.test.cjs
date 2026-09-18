"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { after, before, describe, it } = require("node:test");
const {
  CODE_RABBIT_APP_ID,
  CODE_RABBIT_BOT,
  CODE_RABBIT_STATUS,
  GATE_HTML,
  QUALITY_WAKE_LABELS,
  REVIEW_READY,
} = require("./pr-quality-frozen.cjs");
const {
  publishResolvedPull,
  resolveTrustedPullNumber,
  stackedOnOpenParent,
} = require("./pr-quality-facts.cjs");
const { trustedPolicyRef } = require("./pr-quality.cjs");
const { planQualityGate } = require("./pr-quality-plan.cjs");
const { pickCodeRabbitReview, unreadFindings } = require("./pr-quality-reviews.cjs");
const {
  dropStaleBotNotes,
  pageReviewThreads,
  putIssueComment,
  REVIEW_THREADS,
} = require("./pr-quality-github.cjs");
const { renderRevalidationNotice } = require("./pr-quality-render.cjs");
const { runPrQualityGate } = require("./pr-quality-run.cjs");
const { resolveTrustedPullNumber: resolveFromFacade } = require("./pr-quality-resolve.cjs");

const ROOT = path.join(__dirname, "..", "..");
const WORKFLOW = fs.readFileSync(
  path.join(ROOT, ".github", "workflows", "enforce-pr-target.yml"),
  "utf8",
);
const HEAD = "b7a1c9e2d4f6081a3b5c7d9e0f1234567890abcd";

function jobBody(name) {
  const header = `  ${name}:\n`;
  const start = WORKFLOW.indexOf(header);
  assert.ok(start >= 0, `missing job ${name}`);
  const rest = WORKFLOW.slice(start + header.length);
  const next = rest.search(/^  [a-z][a-z0-9-]*:/m);
  return next === -1 ? rest : rest.slice(0, next);
}

function checkoutSteps(job) {
  return [...job.matchAll(/uses:\s+actions\/checkout@[^\s]+[\s\S]*?(?=\n {6}- name:|\n {2}\S|$)/g)].map(
    (match) => match[0],
  );
}

const RESOLVE_BOOTSTRAP_REF =
  "${{ github.event_name == 'pull_request_target' && (github.event.pull_request.base.ref == 'main' && 'main' || 'dev') || 'dev' }}";
const ENFORCE_TRUSTED_REF = "${{ needs.resolve.outputs.trusted-ref }}";

const ACQUISITION_RULES = [
  { id: "github.head_ref", re: /github\.head_ref/ },
  { id: "pull_request.head", re: /github\.event\.pull_request\.head/ },
  { id: "refs/pull", re: /refs\/pull\// },
  { id: "gh pr checkout", re: /gh\s+pr\s+checkout/ },
  { id: "git fetch", re: /\bgit\s+fetch\b/ },
  { id: "git checkout", re: /\bgit\s+checkout\b/ },
  { id: "git switch", re: /\bgit\s+switch\b/ },
  { id: "git clone", re: /\bgit\s+clone\b/ },
];

function withoutYamlComments(text) {
  return text
    .split("\n")
    .filter((line) => !/^\s*#/.test(line))
    .join("\n");
}

function acquisitionHits(text) {
  const executable = withoutYamlComments(text);
  return ACQUISITION_RULES.filter((rule) => rule.re.test(executable)).map((rule) => rule.id);
}

function checkoutRef(step) {
  return step.match(/^\s*ref:\s*(.+)$/m)?.[1]?.replace(/\s+/g, " ").trim() ?? "";
}

function checkoutPolicyHits(job, expectedRef) {
  const hits = [];
  for (const step of checkoutSteps(job)) {
    if (/(?:^|\n)\s+repository:/.test(step)) hits.push("repository override");
    if (!/persist-credentials:\s*false/.test(step)) hits.push("credential persistence");
    if (checkoutRef(step) !== expectedRef) hits.push(`untrusted ref:${checkoutRef(step)}`);
    hits.push(...acquisitionHits(step));
  }
  return hits;
}

function extraCheckout(ref) {
  return [
    "      - name: Extra checkout",
    "        uses: actions/checkout@9c091bb21b7c1c1d1991bb908d89e4e9dddfe3e0",
    "        with:",
    `          ref: ${ref}`,
    "          persist-credentials: false",
  ].join("\n");
}

function quotedLabels(text) {
  return [...text.matchAll(/github\.event\.label\.name == '([^']+)'/g)].map((match) => match[1]);
}

describe("workflow document", () => {
  const resolve = jobBody("resolve");
  const enforce = jobBody("enforce-target");

  it("loads only the two runner entrypoints from YAML", () => {
    assert.match(WORKFLOW, /require\("\.\/\.github\/scripts\/pr-quality-resolve\.cjs"\)/);
    assert.match(WORKFLOW, /require\("\.\/\.github\/scripts\/pr-quality-run\.cjs"\)/);
    assert.match(WORKFLOW, /resolveTrustedPullNumber/);
    assert.match(WORKFLOW, /runPrQualityGate/);
    assert.doesNotMatch(WORKFLOW, /gatherQualityProblems/);
    assert.doesNotMatch(WORKFLOW, /pr-quality-facts\.cjs/);
    assert.doesNotMatch(WORKFLOW, /pr-quality-branch\.cjs/);
  });

  it("defaults the workflow token to no scopes", () => {
    assert.match(WORKFLOW, /^permissions:\s*\{\}\s*$/m);
  });

  it("keeps the resolver read-only and confines write tokens to enforce-target", () => {
    assert.match(resolve, /contents:\s*read/);
    assert.match(resolve, /pull-requests:\s*read/);
    assert.doesNotMatch(resolve, /contents:\s*write/);
    assert.doesNotMatch(resolve, /pull-requests:\s*write/);
    assert.match(enforce, /contents:\s*write/);
    assert.match(enforce, /pull-requests:\s*write/);
  });

  it("serializes comment mutation per pull number", () => {
    assert.match(enforce, /group:\s*pr-gate-comment-\$\{\{\s*needs\.resolve\.outputs\.pull-number\s*\}\}/);
    assert.match(enforce, /cancel-in-progress:\s*false/);
  });

  it("pins third-party actions to full SHAs", () => {
    const pins = [...WORKFLOW.matchAll(/uses:\s+(?!.*\/\.github\/)(\S+?)@([0-9a-f]{40}|[^\s]+)/g)];
    assert.ok(pins.length >= 2);
    for (const pin of pins) {
      assert.match(pin[2], /^[0-9a-f]{40}$/, `${pin[1]} must be SHA-pinned`);
    }
  });
});

describe("trusted checkout", () => {
  const resolve = jobBody("resolve");
  const enforce = jobBody("enforce-target");
  const resolveCheckouts = checkoutSteps(resolve);
  const enforceCheckouts = checkoutSteps(enforce);

  it("keeps one checkout in each privileged job", () => {
    assert.equal(resolveCheckouts.length, 1);
    assert.equal(enforceCheckouts.length, 1);
  });

  it("binds every resolve checkout to the main-or-dev bootstrap ref", () => {
    assert.deepEqual(checkoutPolicyHits(resolve, RESOLVE_BOOTSTRAP_REF), []);
    for (const step of resolveCheckouts) {
      assert.equal(checkoutRef(step), RESOLVE_BOOTSTRAP_REF);
      assert.match(step, /sparse-checkout:\s*\|\s*\n\s*\.github\/scripts\n\s*MAINTAINERS\.md/);
    }
  });

  it("binds every enforce-target checkout to the published trusted-ref", () => {
    assert.deepEqual(checkoutPolicyHits(enforce, ENFORCE_TRUSTED_REF), []);
    assert.match(enforce, /trusted-ref == 'main'/);
    assert.match(enforce, /trusted-ref == 'dev'/);
    for (const step of enforceCheckouts) {
      assert.match(step, /sparse-checkout:\s*\|\s*\n\s*\.github\/scripts\n\s*MAINTAINERS\.md/);
    }
  });

  it("still accepts another checkout when it repeats the same trusted ref", () => {
    const job = `${resolve}\n${extraCheckout(RESOLVE_BOOTSTRAP_REF)}`;
    assert.equal(checkoutSteps(job).length, 2);
    assert.deepEqual(checkoutPolicyHits(job, RESOLVE_BOOTSTRAP_REF), []);
  });

  it("fails when an added checkout names github.head_ref", () => {
    const job = `${resolve}\n${extraCheckout("${{ github.head_ref }}")}`;
    assert.equal(checkoutSteps(job).length, 2);
    const hits = checkoutPolicyHits(job, RESOLVE_BOOTSTRAP_REF);
    assert.ok(hits.some((hit) => hit.startsWith("untrusted ref:")));
    assert.ok(hits.includes("github.head_ref"));
  });
});

describe("code acquisition outside checkout", () => {
  const resolve = jobBody("resolve");

  it("forbids executable pull-request code acquisition in the live workflow", () => {
    assert.deepEqual(acquisitionHits(WORKFLOW), []);
    assert.doesNotMatch(withoutYamlComments(WORKFLOW), /default_branch/);
  });

  it("fails when a run step fetches git objects", () => {
    const job = `${resolve}\n      - name: Pull objects\n        run: git fetch origin pull/44/head\n`;
    assert.ok(acquisitionHits(job).includes("git fetch"));
  });

  it("does not treat a comment that names git fetch as executable acquisition", () => {
    const text = `${WORKFLOW}\n  # git fetch origin pull/44/head\n`;
    assert.deepEqual(acquisitionHits(text), []);
  });
});

describe("event surface", () => {
  it("wakes on pull_request_target mutations plus a CodeRabbit success status", () => {
    assert.match(WORKFLOW, /^on:\n {2}pull_request_target:/m);
    assert.match(WORKFLOW, /^ {2}status:\s*$/m);
    for (const action of [
      "opened",
      "reopened",
      "edited",
      "labeled",
      "unlabeled",
      "ready_for_review",
      "synchronize",
    ]) {
      assert.match(WORKFLOW, new RegExp(`- ${action}\\b`));
    }
  });

  it("ignores comment and review events that would execute untrusted workflow YAML", () => {
    assert.doesNotMatch(WORKFLOW, /^ {2}issue_comment:/m);
    assert.doesNotMatch(WORKFLOW, /^ {2}pull_request_review:/m);
    assert.doesNotMatch(WORKFLOW, /^ {2}pull_request_review_comment:/m);
  });

  it("filters labeled wakes through the frozen quality-label set", () => {
    assert.deepEqual(quotedLabels(WORKFLOW).sort(), [...QUALITY_WAKE_LABELS].sort());
  });

  it("accepts status events only from the frozen CodeRabbit GitHub App identity", () => {
    const resolveIf = jobBody("resolve");
    assert.ok(resolveIf.includes(`github.event.context == '${CODE_RABBIT_STATUS}'`));
    assert.ok(resolveIf.includes("github.event.state == 'success'"));
    assert.ok(resolveIf.includes(`github.event.sender.login == '${CODE_RABBIT_BOT}'`));
    assert.ok(resolveIf.includes(`github.event.sender.id == ${CODE_RABBIT_APP_ID}`));
  });
});

describe("trusted-ref publication", () => {
  it("maps executable policy onto dev except for a main promotion", () => {
    assert.equal(trustedPolicyRef("main"), "main");
    assert.equal(trustedPolicyRef("dev"), "dev");
    assert.equal(trustedPolicyRef("feat/parent"), "dev");
    assert.equal(trustedPolicyRef(undefined), "dev");
  });

  it("writes pull-number and trusted-ref together", () => {
    const outputs = {};
    assert.equal(
      publishResolvedPull(
        { setOutput(name, value) { outputs[name] = value; } },
        { number: 44, base: { ref: "dev" } },
      ),
      true,
    );
    assert.deepEqual(outputs, { "pull-number": "44", "trusted-ref": "dev" });

    const stacked = {};
    publishResolvedPull(
      { setOutput(name, value) { stacked[name] = value; } },
      { number: 81, base: { ref: "feat/parent" } },
    );
    assert.equal(stacked["trusted-ref"], "dev");
  });
});

describe("resolveTrustedPullNumber", () => {
  function openPull(number, base, sha = HEAD) {
    return { number, state: "open", head: { sha }, base: { ref: base } };
  }

  async function run({ eventName, payload, associated = [], open = [] }) {
    const outputs = {};
    const info = [];
    const github = {
      paginate: async (fn) => {
        if (fn === github.rest.repos.listPullRequestsAssociatedWithCommit) return associated;
        if (fn === github.rest.pulls.list) return open;
        return [];
      },
      rest: {
        repos: { listPullRequestsAssociatedWithCommit: async () => ({ data: associated }) },
        pulls: { list: async () => ({ data: open }) },
      },
    };
    await resolveTrustedPullNumber({
      github,
      context: {
        repo: { owner: "Wibias", repo: "Benes" },
        eventName,
        payload,
      },
      core: {
        setOutput(name, value) { outputs[name] = value; },
        info(message) { info.push(String(message)); },
        warning() {},
      },
    });
    return { outputs, info };
  }

  it("publishes a pull_request_target payload targeting dev", async () => {
    const { outputs } = await run({
      eventName: "pull_request_target",
      payload: { pull_request: openPull(44, "dev") },
    });
    assert.deepEqual(outputs, { "pull-number": "44", "trusted-ref": "dev" });
  });

  it("publishes a main-base promotion as trusted-ref main", async () => {
    const { outputs } = await run({
      eventName: "pull_request_target",
      payload: { pull_request: openPull(205, "main") },
    });
    assert.deepEqual(outputs, { "pull-number": "205", "trusted-ref": "main" });
  });

  it("still executes stacked children from the dev policy ref", async () => {
    const { outputs } = await run({
      eventName: "pull_request_target",
      payload: { pull_request: openPull(81, "feat/parent") },
    });
    assert.deepEqual(outputs, { "pull-number": "81", "trusted-ref": "dev" });
  });

  it("binds a unique current-head CodeRabbit status to that pull", async () => {
    const payload = {
      context: CODE_RABBIT_STATUS,
      state: "success",
      sha: HEAD,
      sender: { login: CODE_RABBIT_BOT, id: CODE_RABBIT_APP_ID },
    };
    const { outputs } = await run({
      eventName: "status",
      payload,
      associated: [openPull(44, "dev")],
    });
    assert.deepEqual(outputs, { "pull-number": "44", "trusted-ref": "dev" });
  });

  it("publishes trusted-ref main when that unique status belongs to a main-base pull", async () => {
    const { outputs } = await run({
      eventName: "status",
      payload: {
        context: CODE_RABBIT_STATUS,
        state: "success",
        sha: HEAD,
        sender: { login: CODE_RABBIT_BOT, id: CODE_RABBIT_APP_ID },
      },
      associated: [openPull(205, "main")],
    });
    assert.deepEqual(outputs, { "pull-number": "205", "trusted-ref": "main" });
  });

  it("skips status events that are missing, duplicated, or from another producer", async () => {
    const payload = {
      context: CODE_RABBIT_STATUS,
      state: "success",
      sha: HEAD,
      sender: { login: CODE_RABBIT_BOT, id: CODE_RABBIT_APP_ID },
    };
    const missing = await run({ eventName: "status", payload, associated: [], open: [] });
    assert.deepEqual(missing.outputs, {});
    assert.match(missing.info.join("\n"), /skipping ambiguous\/stale revalidation/);

    const duplicated = await run({
      eventName: "status",
      payload,
      associated: [openPull(44, "dev"), openPull(81, "dev")],
      open: [openPull(44, "dev"), openPull(81, "dev")],
    });
    assert.deepEqual(duplicated.outputs, {});

    const foreign = await run({
      eventName: "status",
      payload: { ...payload, sender: { login: "other[bot]", id: 1 } },
      associated: [openPull(44, "dev")],
    });
    assert.deepEqual(foreign.outputs, {});
    assert.match(foreign.info.join("\n"), /not the CodeRabbit GitHub App/);
  });
});

describe("draft conversion is fail-closed; ready conversion is not", () => {
  const previous = process.env.RESOLVED_PULL_NUMBER;

  before(() => {
    process.env.RESOLVED_PULL_NUMBER = "44";
  });

  after(() => {
    if (previous === undefined) delete process.env.RESOLVED_PULL_NUMBER;
    else process.env.RESOLVED_PULL_NUMBER = previous;
  });

  function failingPr() {
    return {
      number: 44,
      node_id: "PR_FAIL",
      title: "x",
      body: "",
      draft: false,
      user: { login: "fork-author" },
      labels: [],
      changed_files: 0,
      base: { ref: "main", repo: { owner: { login: "Wibias" }, name: "Benes" } },
      head: { sha: HEAD, ref: "feat/x" },
    };
  }

  function readyPr() {
    return {
      number: 44,
      node_id: "PR_READY",
      title: "fix: hop 410 after visible output",
      body: [
        "## Summary",
        "Keep combo failover on the first visible stream event and hop HTTP 410 like other gone statuses.",
        "Clients stay on the committed member after model-visible output.",
        "",
        "## Verification",
        "- go test ./internal/combo",
        "- go vet ./internal/combo",
        "",
        "<!-- pr-quality-readiness-checklist:start -->",
        "## Review readiness checklist",
        "- [x] All CI tests are green on my local testing.",
        "- [x] I pushed my PR to the latest dev commit.",
        "- [x] I resolved all correct Codex and CodeRabbit findings.",
        "- [x] My PR is ready for review.",
        "<!-- pr-quality-readiness-checklist:end -->",
      ].join("\n"),
      draft: true,
      user: { login: "fork-author" },
      labels: [],
      changed_files: 0,
      base: { ref: "dev", repo: { owner: { login: "Wibias" }, name: "Benes" } },
      head: { sha: HEAD, ref: "feat/x" },
    };
  }

  function githubFor(pr, graphqlImpl) {
    const github = {
      paginate: async (fn) => {
        if (fn === github.rest.issues.listComments) return [];
        if (fn === github.rest.issues.listEvents) return [];
        if (fn === github.rest.pulls.listFiles) return [];
        if (fn === github.rest.pulls.list) return [];
        if (fn === github.rest.pulls.listReviews) return [];
        return [];
      },
      rest: {
        pulls: {
          get: async () => ({ data: pr }),
          update: async () => ({ data: pr }),
          listFiles: async () => ({ data: [] }),
          list: async () => ({ data: [] }),
          listReviews: async () => ({ data: [] }),
        },
        repos: {
          getCollaboratorPermissionLevel: async () => ({ data: { permission: "read" } }),
          compareCommitsWithBasehead: async ({ basehead }) => ({
            data: basehead.startsWith("main...")
              ? { behind_by: 40, ahead_by: 1 }
              : { behind_by: 0, ahead_by: 1 },
          }),
        },
        issues: {
          listComments: async () => ({ data: [] }),
          listEvents: async () => ({ data: [] }),
          createComment: async (args) => ({ data: { id: 17, body: args.body } }),
          updateComment: async (args) => ({ data: args }),
          addLabels: async () => ({ data: [] }),
          removeLabel: async () => ({ data: [] }),
          deleteComment: async () => ({}),
        },
      },
      graphql: graphqlImpl,
    };
    return github;
  }

  it("still fails the job when convertPullRequestToDraft throws", async () => {
    const failed = [];
    const pr = failingPr();
    await runPrQualityGate({
      github: githubFor(pr, async () => {
        throw new Error("draft graphql down");
      }),
      context: {
        eventName: "pull_request_target",
        repo: { owner: "Wibias", repo: "Benes" },
        payload: { action: "opened", pull_request: pr },
      },
      core: {
        info() {},
        warning() {},
        setFailed(message) { failed.push(String(message)); },
      },
    });
    assert.ok(failed.some((message) => message.startsWith("PR quality gate failed:")));
  });

  it("does not fail the job when markPullRequestReadyForReview throws", async () => {
    const failed = [];
    const pr = readyPr();
    await runPrQualityGate({
      github: githubFor(pr, async () => {
        throw new Error("ready graphql down");
      }),
      context: {
        eventName: "pull_request_target",
        repo: { owner: "Wibias", repo: "Benes" },
        payload: { action: "edited", pull_request: pr },
      },
      core: {
        info() {},
        warning() {},
        setFailed(message) { failed.push(String(message)); },
      },
    });
    assert.deepEqual(failed, []);
  });
});

describe("review evidence", () => {
  it("pages GraphQL review threads until hasNextPage is false", async () => {
    assert.match(REVIEW_THREADS, /reviewThreads\(first: 100, after: \$cursor\)/);
    assert.match(REVIEW_THREADS, /hasNextPage/);
    const pages = [];
    const nodes = await pageReviewThreads(
      {
        graphql: async (_query, args) => {
          pages.push(args.cursor);
          if (args.cursor == null) {
            return {
              repository: {
                pullRequest: {
                  reviewThreads: {
                    pageInfo: { hasNextPage: true, endCursor: "c1" },
                    nodes: [{ isResolved: false, comments: { nodes: [{ author: { login: CODE_RABBIT_BOT } }] } }],
                  },
                },
              },
            };
          }
          return {
            repository: {
              pullRequest: {
                reviewThreads: {
                  pageInfo: { hasNextPage: false, endCursor: "c2" },
                  nodes: [{ isResolved: true, comments: { nodes: [{ author: { login: CODE_RABBIT_BOT } }] } }],
                },
              },
            },
          };
        },
      },
      { owner: "Wibias", repo: "Benes", number: 44 },
    );
    assert.deepEqual(pages, [null, "c1"]);
    assert.equal(nodes.length, 2);
  });

  it("picks the later CodeRabbit review id when submitted_at is absent", () => {
    const chosen = pickCodeRabbitReview({
      liveHeadSha: HEAD,
      reviews: [
        { id: 8, commit_id: HEAD, user: { login: CODE_RABBIT_BOT }, body: "first" },
        { id: 19, commit_id: HEAD, user: { login: CODE_RABBIT_BOT }, body: "second" },
      ],
    });
    assert.equal(chosen?.id, 19);
  });

  it("holds a completed contributor checklist when review threads cannot be read", () => {
    assert.deepEqual(unreadFindings(), { code: "review_findings", unresolved: 0, byBot: {} });
    const plan = planQualityGate({
      authorHasWrite: false,
      draft: true,
      title: "fix: hop 410 after visible output",
      headSha: HEAD,
      eventHeadSha: HEAD,
      failures: [],
      readiness: { present: true, complete: true, checked: 4, total: 4, items: [] },
      stored: { completedAtHeadSha: HEAD },
      findings: unreadFindings(),
      threadsUnreadable: true,
    });
    assert.equal(plan.kind, "hold");
    assert.equal(plan.wantReadyLabel, false);
    assert.equal(plan.markReady, false);
    assert.ok(plan.freshness.includes("review_findings"));
    const notice = renderRevalidationNotice({
      freshness: plan.freshness,
      threadsUnreadable: true,
    }).join("\n");
    assert.match(notice, /findings claim could not be verified/);
    assert.match(notice, /stays a draft until review threads are readable again/);
  });
});

describe("comment and label mutation", () => {
  it("identifies the gate comment with the frozen HTML marker", () => {
    assert.equal(GATE_HTML, "<!-- benes-pr-gate -->");
  });

  it("creates a gate comment once and patches that same id afterward", async () => {
    const created = [];
    const updated = [];
    const github = {
      rest: {
        issues: {
          async createComment(args) {
            created.push(args);
            return { data: { id: 31, body: args.body } };
          },
          async updateComment(args) {
            updated.push(args);
            return { data: args };
          },
        },
      },
    };
    const first = await putIssueComment({
      github,
      owner: "Wibias",
      repo: "Benes",
      issue_number: 44,
      body: `${GATE_HTML}\nfirst`,
    });
    assert.equal(first.id, 31);
    assert.equal(created.length, 1);
    assert.deepEqual(updated, []);
    const second = await putIssueComment({
      github,
      owner: "Wibias",
      repo: "Benes",
      issue_number: 44,
      comment_id: 31,
      body: `${GATE_HTML}\nsecond`,
    });
    assert.equal(second.id, 31);
    assert.equal(created.length, 1);
    assert.equal(updated[0].comment_id, 31);
  });

  it("compares the composed gate body to the live comment before writing", () => {
    const source = fs.readFileSync(path.join(__dirname, "pr-quality-run.cjs"), "utf8");
    assert.match(source, /if \(gateCommentId && gateComment\?\.body === body\)/);
    assert.doesNotMatch(source, /upsertReadinessComment/);
    assert.doesNotMatch(source, /buildReadinessCommentBody/);
  });

  it("deletes leftover bot notes except the live gate comment", async () => {
    const deleted = [];
    await dropStaleBotNotes({
      owner: "Wibias",
      repo: "Benes",
      ids: [11, 17, 22],
      gateCommentId: 17,
      deleteComment: async ({ comment_id }) => {
        deleted.push(comment_id);
      },
      core: { warning() {} },
    });
    assert.deepEqual(deleted, [11, 22]);
  });

  it("plans the review-ready label without emitting a CodeRabbit slash command", () => {
    const ready = planQualityGate({
      authorHasWrite: false,
      draft: true,
      title: "fix: hop 410 after visible output",
      headSha: HEAD,
      eventHeadSha: HEAD,
      failures: [],
      readiness: { present: true, complete: true, checked: 4, total: 4, items: [] },
      stored: { completedAtHeadSha: HEAD },
      findings: { code: null },
      threadsUnreadable: false,
    });
    assert.equal(ready.wantReadyLabel, true);
    assert.equal(ready.reviewReadyLabel, REVIEW_READY);
    const sources = [
      fs.readFileSync(path.join(__dirname, "pr-quality-run.cjs"), "utf8"),
      fs.readFileSync(path.join(__dirname, "pr-quality-github.cjs"), "utf8"),
      fs.readFileSync(path.join(__dirname, "pr-quality-plan.cjs"), "utf8"),
    ].join("\n");
    assert.doesNotMatch(sources, /coderabbitai review/);
    assert.match(sources, /issues\.addLabels/);
    assert.match(sources, /issues\.removeLabel/);
  });
});

describe("stacking and owned title prefix", () => {
  it("treats an open parent head as a stacked base", () => {
    const pr = {
      number: 81,
      base: { ref: "feat/parent", repo: { owner: { login: "Wibias" }, name: "Benes" } },
    };
    assert.equal(
      stackedOnOpenParent({
        pull_number: 81,
        pr,
        owner: "Wibias",
        repo: "Benes",
        openPrs: [
          {
            number: 44,
            head: { ref: "feat/parent" },
            base: { repo: { owner: { login: "Wibias" }, name: "Benes" } },
          },
        ],
      }),
      true,
    );
  });

  it("strips a bot-owned wrong-base prefix after the base is corrected", () => {
    const plan = planQualityGate({
      authorHasWrite: true,
      draft: false,
      title: "[WRONG BRANCH] fix: hop 410 after visible output",
      headSha: HEAD,
      eventHeadSha: HEAD,
      failures: [],
      readiness: { present: false, complete: false, checked: 0, total: 0, items: [] },
      stored: { titlePrefixedByBot: true, autoDraftedByBot: false, active: true },
      findings: { code: null },
      threadsUnreadable: false,
    });
    assert.equal(plan.stripOwnedPrefix, true);
    assert.equal(plan.prefixTitle, false);
  });
});

describe("module boundary", () => {
  it("does not ship the deleted one-purpose branch-policy file", () => {
    assert.equal(fs.existsSync(path.join(__dirname, "pr-quality-branch.cjs")), false);
  });

  it("keeps pr-quality-resolve as a re-export of resolveTrustedPullNumber", () => {
    const source = fs.readFileSync(path.join(__dirname, "pr-quality-resolve.cjs"), "utf8");
    assert.match(source, /require\("\.\/pr-quality-facts\.cjs"\)/);
    assert.doesNotMatch(source, /github\.paginate/);
    assert.equal(resolveFromFacade, resolveTrustedPullNumber);
    assert.equal(require("./pr-quality-resolve.cjs").publishResolvedPull, undefined);
  });
});

describe("CodeRabbit auto-review eligibility", () => {
  it("does not restrict automatic reviews to a label allow-list", () => {
    const text = fs.readFileSync(path.join(ROOT, ".coderabbit.yaml"), "utf8");
    const autoReview = text.match(/auto_review:[\s\S]*?(?=\n[a-z_]+:|\npath_instructions:|$)/);
    assert.ok(autoReview);
    assert.doesNotMatch(autoReview[0], /^\s+labels:\s*$/m);
  });
});
