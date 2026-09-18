"use strict";

const { describe, it, afterEach } = require("node:test");
const assert = require("node:assert/strict");
const { runIssueTranslation } = require("./issue-translation-run.cjs");
const { MARKER } = require("./issue-translation.cjs");

const originalToken = process.env.BENES_ISSUE_AI_TOKEN;

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

describe("runIssueTranslation admission", () => {
  it("returns before any GitHub call when the model token is unset", async () => {
    delete process.env.BENES_ISSUE_AI_TOKEN;
    const calls = [];
    await runIssueTranslation({
      github: { rest: { issues: { get: async () => { calls.push("get"); } } } },
      context: { eventName: "issues", repo: { owner: "Wibias", repo: "Benes" }, payload: { issue: { number: 8 } } },
      core: mockCore(),
    });
    assert.deepEqual(calls, []);
  });

  it("fails dispatch when the issue number is not a positive safe integer", async () => {
    process.env.BENES_ISSUE_AI_TOKEN = "token";
    const core = mockCore();
    await runIssueTranslation({
      github: { rest: { issues: {} } },
      context: {
        eventName: "workflow_dispatch",
        repo: { owner: "Wibias", repo: "Benes" },
        payload: { inputs: { issue_number: "nope" } },
      },
      core,
    });
    assert.match(core.failedMessages.join("\n"), /Invalid issue number/);
  });
});

describe("runIssueTranslation apply policy", () => {
  it("rewrites the issue body only after a successful translation, never the title", async () => {
    process.env.BENES_ISSUE_AI_TOKEN = "token";
    const calls = [];
    const issue = {
      number: 8,
      title: "Combo failover",
      body: "Wenn das erste Combo-Ziel fehlt, sollte Codex den nächsten nehmen bevor Output sichtbar ist.",
    };
    const github = {
      paginate: async () => [],
      rest: {
        issues: {
          get: async () => {
            calls.push("get");
            return { data: issue };
          },
          update: async (args) => {
            calls.push(["update", args]);
            return { data: args };
          },
          createComment: async (args) => {
            calls.push(["createComment", args]);
            return { data: { id: 1 } };
          },
        },
      },
    };
    const originalFetch = globalThis.fetch;
    globalThis.fetch = async () => ({
      ok: true,
      json: async () => ({
        choices: [{
          message: {
            content: JSON.stringify({
              requires_translation: true,
              detected_language: "German",
              translated_title: "should not be applied",
              translated_body: "When the first combo target is gone, Codex should hop before visible output.",
            }),
          },
        }],
      }),
    });
    try {
      await runIssueTranslation({
        github,
        context: {
          eventName: "issues",
          repo: { owner: "Wibias", repo: "Benes" },
          payload: { issue: { number: 8 } },
        },
        core: mockCore(),
      });
    } finally {
      globalThis.fetch = originalFetch;
    }
    const update = calls.find((entry) => Array.isArray(entry) && entry[0] === "update");
    assert.ok(update);
    assert.equal(update[1].title, undefined);
    assert.equal(update[1].body.includes(MARKER), true);
    assert.equal(update[1].body.includes("Wenn das erste Combo-Ziel fehlt"), true);
  });

  it("does not write when a historical translation marker is unclosed", async () => {
    process.env.BENES_ISSUE_AI_TOKEN = "token";
    const before = "Author text before the marker.";
    const after = "Author text after the unclosed marker.";
    const body = `${before}\n${MARKER}\nunclosed historical content\n${after}`;
    const issue = { number: 8, title: "Combo failover hung after the first target.", body };
    const calls = [];
    const github = {
      paginate: async () => [],
      rest: {
        issues: {
          get: async () => {
            calls.push("get");
            return { data: issue };
          },
          update: async (args) => {
            calls.push(["update", args]);
            return { data: args };
          },
          createComment: async (args) => {
            calls.push(["createComment", args]);
            return { data: { id: 1 } };
          },
        },
      },
    };
    const originalFetch = globalThis.fetch;
    let fetched = 0;
    globalThis.fetch = async () => {
      fetched += 1;
      return { ok: true, json: async () => ({ choices: [{ message: { content: "{}" } }] }) };
    };
    const core = mockCore();
    try {
      await runIssueTranslation({
        github,
        context: {
          eventName: "issues",
          repo: { owner: "Wibias", repo: "Benes" },
          payload: { issue: { number: 8 } },
        },
        core,
      });
    } finally {
      globalThis.fetch = originalFetch;
    }
    assert.equal(fetched, 0);
    assert.equal(calls.includes("get"), true);
    assert.equal(calls.some((entry) => Array.isArray(entry) && entry[0] === "update"), false);
    assert.equal(calls.some((entry) => Array.isArray(entry) && entry[0] === "createComment"), false);
    assert.match(core.infoMessages.join("\n"), /malformed_translation_markup/);
    assert.equal(issue.body, body);
    assert.equal(issue.body.includes(after), true);
  });
});
