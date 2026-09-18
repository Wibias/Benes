"use strict";

const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const { runIssueQualityGate, inspectDispatchTrust, decideTransition } = require("./issue-intake-session.cjs");
const bot = require("./issue-intake-bot.cjs");

function mockGithub(issue, comments = [], options = {}) {
  const calls = [];
  const labels = [...(issue.labels || [])];
  const fail = options.fail || {};
  return {
    calls,
    paginate: async (_fn, args) => {
      calls.push(["paginate", args]);
      return comments;
    },
    rest: {
      issues: {
        get: async (args) => {
          calls.push(["get", args]);
          if (fail.get) throw new Error("get failed");
          return { data: { ...issue, labels } };
        },
        addLabels: async (args) => {
          calls.push(["addLabels", args]);
          labels.push(...args.labels);
          return { data: args };
        },
        update: async (args) => {
          calls.push(["update", args]);
          if (fail.update) throw new Error("update failed");
          if (args.state) issue.state = args.state;
          if (args.state_reason) issue.state_reason = args.state_reason;
          issue.closed_at = args.state === "closed" ? "2026-09-02T00:00:00Z" : null;
          issue.closed_by = args.state === "closed" ? { login: "github-actions[bot]" } : null;
          return { data: { ...issue, ...args } };
        },
        getLabel: async () => ({ data: { name: "ok" } }),
        createComment: async (args) => {
          calls.push(["createComment", args]);
          if (fail.comment) throw new Error("comment failed");
          comments.push({
            id: 9,
            user: { login: "github-actions[bot]" },
            body: args.body,
          });
          return { data: { id: 9 } };
        },
        updateComment: async (args) => {
          calls.push(["updateComment", args]);
          if (fail.comment) throw new Error("comment failed");
          const existing = comments.find((comment) => comment.id === args.comment_id);
          if (existing) existing.body = args.body;
          return { data: args };
        },
        listComments: async () => ({ data: comments }),
      },
      repos: {
        getCollaboratorPermissionLevel: async () => ({
          data: { permission: options.permission || "read" },
        }),
      },
    },
  };
}

function coreSink() {
  const failed = [];
  return {
    failed,
    info() {},
    warning() {},
    setFailed(message) {
      failed.push(message);
    },
  };
}

const INVALID = {
  number: 8,
  title: "help",
  body: "why is combo failover broken",
  labels: [],
  state: "open",
  author_association: "NONE",
};

describe("workflow_dispatch trust boundary", () => {
  it("is irrelevant for issues events", () => {
    assert.deepEqual(inspectDispatchTrust("issues", undefined, undefined), {
      ok: true,
      detail: null,
    });
  });

  it("rejects a missing or invalid default branch", () => {
    assert.equal(inspectDispatchTrust("workflow_dispatch", "refs/heads/main", undefined).ok, false);
    assert.equal(inspectDispatchTrust("workflow_dispatch", "refs/heads/main", "").ok, false);
    assert.equal(inspectDispatchTrust("workflow_dispatch", "refs/heads/main", "main\n").ok, false);
  });

  it("rejects a missing or empty selected ref", () => {
    assert.equal(inspectDispatchTrust("workflow_dispatch", undefined, "dev").ok, false);
    assert.equal(inspectDispatchTrust("workflow_dispatch", "", "dev").ok, false);
  });

  it("allows only the exact default-branch ref", () => {
    assert.deepEqual(inspectDispatchTrust("workflow_dispatch", "refs/heads/dev", "dev"), {
      ok: true,
      detail: null,
    });
    assert.equal(inspectDispatchTrust("workflow_dispatch", "refs/heads/main", "dev").ok, false);
    assert.equal(inspectDispatchTrust("workflow_dispatch", "dev", "dev").ok, false);
  });
});

describe("issue quality transitions", () => {
  it("applies labels then closes an invalid untrusted issue", async () => {
    const issue = { ...INVALID };
    const github = mockGithub(issue);
    await runIssueQualityGate({
      github,
      context: {
        eventName: "issues",
        actor: "reporter",
        repo: { owner: "Wibias", repo: "Benes" },
        payload: { action: "opened", issue },
      },
      core: coreSink(),
    });
    const names = github.calls.map((entry) => entry[0]);
    assert.ok(names.indexOf("addLabels") < names.indexOf("update"));
    const close = github.calls.find((entry) => entry[0] === "update" && entry[1].state === "closed");
    assert.equal(close[1].state_reason, "not_planned");
    assert.equal(github.calls.some((entry) => entry[0] === "createComment"), true);
  });

  it("does not auto-close a trusted author", async () => {
    const issue = { ...INVALID, author_association: "MEMBER" };
    const github = mockGithub(issue);
    await runIssueQualityGate({
      github,
      context: {
        eventName: "issues",
        actor: "Wibias",
        repo: { owner: "Wibias", repo: "Benes" },
        payload: { action: "opened", issue },
      },
      core: coreSink(),
    });
    assert.equal(github.calls.some((entry) => entry[0] === "update" && entry[1].state === "closed"), false);
  });

  it("reopens a bot-owned close after a valid edit", () => {
    const state = bot.activeCloseState({
      kind: "bug",
      closedAt: "2026-09-02T00:00:00Z",
      stateReason: "not_planned",
    });
    const transition = decideTransition({
      issue: {
        state: "closed",
        closed_at: "2026-09-02T00:00:00Z",
        state_reason: "not_planned",
        closed_by: "github-actions[bot]",
      },
      botRecord: { kind: "bot-close", state },
      trustedAuthor: false,
      maintainerActor: false,
      eventAction: "edited",
      verdict: { valid: true, softPass: false },
    });
    assert.equal(transition.type, "reopen");
  });

  it("does not reopen a human-owned close", () => {
    const transition = decideTransition({
      issue: {
        state: "closed",
        closed_at: "2026-09-02T00:00:00Z",
        state_reason: "not_planned",
        closed_by: "Wibias",
      },
      botRecord: {
        kind: "bot-close",
        state: bot.activeCloseState({
          kind: "bug",
          closedAt: "2026-09-01T00:00:00Z",
          stateReason: "not_planned",
        }),
      },
      trustedAuthor: false,
      maintainerActor: false,
      eventAction: "edited",
      verdict: { valid: true, softPass: false },
    });
    assert.equal(transition.type, "idle");
    assert.notEqual(transition.type, "reopen");
  });

  it("records a sticky override when a maintainer reopens", () => {
    const transition = decideTransition({
      issue: { state: "open", closed_at: null, state_reason: null, closed_by: null },
      botRecord: { kind: "bot-close", state: bot.activeCloseState({ kind: "bug" }) },
      trustedAuthor: false,
      maintainerActor: true,
      eventAction: "reopened",
      verdict: { valid: false, softPass: false },
    });
    assert.equal(transition.type, "override");
  });

  it("does not re-close after a maintainer override", () => {
    const transition = decideTransition({
      issue: { state: "open", closed_at: null, state_reason: null, closed_by: null },
      botRecord: {
        kind: "override",
        state: bot.maintainerOverrideState({ kind: "bug" }, "bug"),
      },
      trustedAuthor: false,
      maintainerActor: false,
      eventAction: "edited",
      verdict: { valid: false, softPass: false },
    });
    assert.equal(transition.skip, "maintainer-override");
  });

  it("fails safe on a malformed bot marker", () => {
    const transition = decideTransition({
      issue: {
        state: "closed",
        closed_at: "2026-09-02T00:00:00Z",
        state_reason: "not_planned",
        closed_by: "github-actions[bot]",
      },
      botRecord: { kind: "unreadable", state: null },
      trustedAuthor: false,
      maintainerActor: false,
      eventAction: "edited",
      verdict: { valid: true, softPass: false },
    });
    assert.equal(transition.type, "idle");
  });

  it("skips an identical bot comment rewrite", async () => {
    const body = bot.closeCommentBody(
      bot.activeCloseState({
        kind: null,
        closedAt: "2026-09-02T00:00:00Z",
        stateReason: "not_planned",
      }),
      { missingTemplate: true, reasons: ["x"], guidance: ["y"] },
    );
    const comments = [{ id: 9, user: { login: "github-actions[bot]" }, body }];
    const result = await bot.upsertBotComment({
      github: mockGithub(INVALID, comments),
      owner: "Wibias",
      repo: "Benes",
      issueNumber: 8,
      botComment: comments[0],
      body,
    });
    assert.equal(result.wrote, false);
  });

  it("does not write a close-owned comment when the close API fails", async () => {
    const issue = { ...INVALID };
    const github = mockGithub(issue, [], { fail: { update: true } });
    await runIssueQualityGate({
      github,
      context: {
        eventName: "issues",
        actor: "reporter",
        repo: { owner: "Wibias", repo: "Benes" },
        payload: { action: "opened", issue },
      },
      core: coreSink(),
    });
    assert.equal(github.calls.some((entry) => entry[0] === "createComment"), false);
    assert.equal(issue.state, "open");
  });
});
