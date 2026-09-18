"use strict";

const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const { runPrHygieneGate } = require("./pr-hygiene-run.cjs");

const ADD_PROVIDER_HEAD = [
  'import { addProviderKeyConnectEnabled } from "../src/lib/add-provider-form-policy.ts";',
  'import { addProviderModalReducer } from "../src/components/add-provider-modal-reducer.ts";',
  'test("connection", () => {});',
].join("\n");
const MODELS_HEAD = 'import { buildProviderModelGroups } from "../src/models-groups.ts";\ntest("models", () => {});\n';
const TEST_SHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";
const SOURCE_SHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";

function mockCore() {
  const info = [];
  const warnings = [];
  const failed = [];
  return {
    info: (message) => info.push(String(message)),
    warning: (message) => warnings.push(String(message)),
    setFailed: (message) => failed.push(String(message)),
    infoMessages: info,
    warnings,
    failedMessages: failed,
  };
}

function mockHygieneGithub({ files, blobs }) {
  const comments = [];
  const github = {
    paginate: async (fn) => {
      if (fn === github.rest.pulls.listFiles) return files;
      if (fn === github.rest.issues.listComments) return [];
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
        get: async () => ({
          data: {
            number: 158,
            user: { login: "contributor" },
            labels: [],
          },
        }),
        listFiles: async () => ({ data: files }),
      },
      repos: {
        getCollaboratorPermissionLevel: async () => ({ data: { permission: "read" } }),
      },
      issues: {
        getLabel: async () => ({ data: { name: "x" } }),
        createLabel: async () => ({ data: {} }),
        addLabels: async () => ({ data: [] }),
        removeLabel: async () => ({ data: [] }),
        listComments: async () => ({ data: [] }),
        createComment: async (args) => {
          comments.push(args.body);
          return { data: { id: 1, body: args.body } };
        },
        updateComment: async (args) => {
          comments.push(args.body);
          return { data: args };
        },
      },
    },
  };
  return { github, comments };
}

function contextFor() {
  return {
    repo: { owner: "Wibias", repo: "Benes" },
    payload: { action: "opened", pull_request: { number: 158 } },
  };
}

describe("runPrHygieneGate GUI head hydration", () => {
  it("covers add-provider sources from the hydrated test blob, not the checkout", async () => {
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
    const { github, comments } = mockHygieneGithub({
      files,
      blobs: { [TEST_SHA]: ADD_PROVIDER_HEAD },
    });
    const core = mockCore();
    await runPrHygieneGate({ github, context: contextFor(), core });
    assert.deepEqual(core.failedMessages, []);
    assert.match(comments.join("\n"), /passed/);
  });

  it("covers the modal reducer from the same hydrated test blob", async () => {
    const files = [
      {
        filename: "gui/src/components/add-provider-modal-reducer.ts",
        status: "modified",
        sha: SOURCE_SHA,
        patch: "+export function addProviderModalReducer() {}",
      },
      {
        filename: "gui/scripts/add-provider-connection.test.ts",
        status: "modified",
        sha: TEST_SHA,
        patch: "@@\n+test('connection', () => {})",
      },
    ];
    const { github, comments } = mockHygieneGithub({
      files,
      blobs: { [TEST_SHA]: ADD_PROVIDER_HEAD },
    });
    const core = mockCore();
    await runPrHygieneGate({ github, context: contextFor(), core });
    assert.deepEqual(core.failedMessages, []);
    assert.match(comments.join("\n"), /passed/);
  });

  it("does not treat an unrelated models-groups test as coverage", async () => {
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
    const { github } = mockHygieneGithub({
      files,
      blobs: { [TEST_SHA]: MODELS_HEAD },
    });
    const core = mockCore();
    await runPrHygieneGate({ github, context: contextFor(), core });
    assert.match(core.failedMessages.join("\n"), /missing_regression_test/);
  });
});
