"use strict";

const { describe, it, afterEach } = require("node:test");
const assert = require("node:assert/strict");
const { runIssueTriage } = require("./issue-triage-run.cjs");

const originalToken = process.env.BENES_ISSUE_AI_TOKEN;
const SECRET = "triage-secret-token-do-not-log";
const SHARED_LINE =
  "POST /v1/responses returned HTTP 410 after the first combo target and before any output.";
const RELATED_REASON =
  "Both reports return HTTP 410 from POST /v1/responses during combo failover.";

afterEach(() => {
  if (originalToken === undefined) delete process.env.BENES_ISSUE_AI_TOKEN;
  else process.env.BENES_ISSUE_AI_TOKEN = originalToken;
});

function mockCore() {
  const info = [];
  const failed = [];
  return {
    info: (message) => info.push(String(message)),
    setFailed: (message) => failed.push(String(message)),
    infoMessages: info,
    failedMessages: failed,
  };
}

function mockGithub(issue) {
  const calls = [];
  return {
    calls,
    rest: {
      issues: {
        get: async (args) => {
          calls.push(["get", args]);
          return { data: issue };
        },
        createComment: async (args) => {
          calls.push(["createComment", args]);
          return { data: { id: 1 } };
        },
        update: async (args) => {
          calls.push(["update", args]);
          return { data: args };
        },
      },
    },
  };
}

function issueContext(number, eventName = "issues") {
  return {
    repo: { owner: "Wibias", repo: "Benes" },
    eventName,
    payload:
      eventName === "workflow_dispatch"
        ? { inputs: { issue_number: number } }
        : { issue: { number } },
  };
}

describe("triage session skip paths", () => {
  it("fails closed on a non-numeric dispatch issue number with no GitHub calls", async () => {
    const github = mockGithub({ number: 0 });
    const core = mockCore();
    await runIssueTriage({
      github,
      context: issueContext("nope", "workflow_dispatch"),
      core,
    });
    assert.equal(github.calls.length, 0);
    assert.match(core.failedMessages.join("\n"), /Invalid issue number/);
  });

  it("fetches a pull request then skips with no writes", async () => {
    const github = mockGithub({ number: 22, pull_request: { url: "https://example.test" } });
    const core = mockCore();
    await runIssueTriage({
      github,
      context: issueContext(22),
      core,
    });
    assert.deepEqual(github.calls.map((entry) => entry[0]), ["get"]);
    assert.match(core.infoMessages.join("\n"), /pull request/);
  });

  it("skips nomination when the dedicated AI token is unset", async () => {
    delete process.env.BENES_ISSUE_AI_TOKEN;
    const github = mockGithub({ number: 22, title: "Listener bind failed", body: "EADDRINUSE" });
    const core = mockCore();
    await runIssueTriage({
      github,
      context: issueContext(22),
      core,
    });
    assert.deepEqual(github.calls.map((entry) => entry[0]), ["get"]);
    assert.match(core.infoMessages.join("\n"), /BENES_ISSUE_AI_TOKEN is not set/);
  });

  it("skips a model HTTP failure with a coarse reason and never echoes the token", async () => {
    process.env.BENES_ISSUE_AI_TOKEN = SECRET;
    const github = mockGithub({ number: 22, title: "bind failed", body: "EADDRINUSE on the listener" });
    const core = mockCore();
    await runIssueTriage({
      github,
      context: issueContext(22),
      core,
      knownIssues: [],
      completeJson: async () => ({ ok: false, reason: "http_401" }),
    });
    assert.deepEqual(github.calls.map((entry) => entry[0]), ["get"]);
    const logs = core.infoMessages.join("\n");
    assert.match(logs, /http_401/);
    assert.equal(logs.includes(SECRET), false);
  });

  it("writes nothing when duplicate proof is missing", async () => {
    process.env.BENES_ISSUE_AI_TOKEN = SECRET;
    const github = mockGithub({
      number: 22,
      title: "combo hop",
      body: "Failover still feels wrong on the first target.",
    });
    const core = mockCore();
    await runIssueTriage({
      github,
      context: issueContext(22),
      core,
      knownIssues: [{ number: 12, title: "catalog", body: SHARED_LINE, state: "open" }],
      completeJson: async () => ({
        ok: true,
        text: JSON.stringify({ duplicates: ["12"], related: [] }),
      }),
    });
    assert.deepEqual(github.calls.map((entry) => entry[0]), ["get"]);
  });
});

describe("triage session GitHub effects", () => {
  it("comments then closes with duplicate as the only state reason", async () => {
    process.env.BENES_ISSUE_AI_TOKEN = SECRET;
    const github = mockGithub({
      number: 22,
      title: "combo hop",
      body: SHARED_LINE,
    });
    const core = mockCore();
    await runIssueTriage({
      github,
      context: issueContext(22),
      core,
      knownIssues: [{ number: 12, title: "earlier combo hop", body: SHARED_LINE, state: "closed" }],
      completeJson: async () => ({
        ok: true,
        text: JSON.stringify({
          duplicates: ["12"],
          related: [{ number: "13", reason: RELATED_REASON }],
        }),
      }),
    });
    assert.deepEqual(
      github.calls.map((entry) => entry[0]),
      ["get", "createComment", "update"],
    );
    const comment = github.calls[1][1];
    assert.match(comment.body, /Duplicate of #12\./);
    assert.match(comment.body, new RegExp(`Shared failure: \`${SHARED_LINE}\``));
    assert.doesNotMatch(comment.body, /Related reports/);
    assert.deepEqual(github.calls[2][1], {
      owner: "Wibias",
      repo: "Benes",
      issue_number: 22,
      state: "closed",
      state_reason: "duplicate",
    });
  });

  it("posts a related-only comment and leaves the issue open", async () => {
    process.env.BENES_ISSUE_AI_TOKEN = SECRET;
    const github = mockGithub({
      number: 22,
      title: "combo hop wording",
      body: "Codex saw HTTP 410 from POST /v1/responses when the first combo member was gone.",
    });
    const core = mockCore();
    await runIssueTriage({
      github,
      context: issueContext(22),
      core,
      knownIssues: [{ number: 12, title: "earlier", body: SHARED_LINE, state: "open" }],
      completeJson: async () => ({
        ok: true,
        text: JSON.stringify({
          duplicates: [],
          related: [{ number: "12", reason: RELATED_REASON }],
          reason: "do not publish this overall reason",
        }),
      }),
    });
    assert.deepEqual(
      github.calls.map((entry) => entry[0]),
      ["get", "createComment"],
    );
    const comment = github.calls[1][1];
    assert.match(comment.body, /Related reports \(issue stays open\):/);
    assert.match(comment.body, /#12:/);
    assert.doesNotMatch(comment.body, /do not publish this overall reason/);
    assert.doesNotMatch(comment.body, /Duplicate of/);
  });
});
