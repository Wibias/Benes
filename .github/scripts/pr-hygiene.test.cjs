"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const {
  evaluatePatchHygiene,
  evaluateHygiene,
  hygieneFailures,
  isCommentOnlyPatch,
  HYGIENE_HINTS,
  WAKE_LABELS,
} = require("./pr-hygiene.cjs");
const {
  decodeGitBlob,
  isSurvivingGuiTestFile,
  loadGuiTestContentsAtHead,
  readFileFromContents,
} = require("./pr-hygiene-head-files.cjs");

function codes(files, extra = {}) {
  return evaluatePatchHygiene({ files, ...extra }).map((failure) => failure.code);
}

describe("regression coverage for Benes behavior trees", () => {
  it("requires a surviving test next to a Go or GUI change", () => {
    assert.deepEqual(
      codes([{ filename: "internal/router/selector.go", patch: "+func pick() {}" }]),
      ["missing_regression_test"],
    );
    assert.deepEqual(
      codes([{ filename: "cmd/benes/combo.go", patch: "+func set() {}" }]),
      ["missing_regression_test"],
    );
    assert.deepEqual(
      codes([{ filename: "gui/src/pages/Providers.tsx", patch: "+export function Providers() {}" }]),
      ["missing_regression_test"],
    );
  });

  it("passes when a matching test file is added or the exception label is present", () => {
    assert.deepEqual(
      codes([
        { filename: "internal/router/selector.go", patch: "+func pick() {}" },
        { filename: "internal/router/selector_test.go", patch: "+func TestPick(t *testing.T) {}" },
      ]),
      [],
    );
    assert.deepEqual(
      codes([
        { filename: "gui/src/pages/Providers.tsx", patch: "+export function Providers() {}" },
        { filename: "gui/scripts/providers-fleet.test.ts", patch: "+test('fleet', () => {})" },
      ]),
      [],
    );
    assert.deepEqual(
      codes([
        { filename: "cmd/benes/combo.go", patch: "+func set() {}" },
        { filename: "cmd/benes/combo_test.go", patch: "+func TestSet(t *testing.T) {}" },
      ]),
      [],
    );
    assert.deepEqual(
      evaluatePatchHygiene({
        files: [{ filename: "cmd/benes/combo.go", patch: "+func set() {}" }],
        labels: ["test-exception-approved"],
      }),
      [],
    );
  });

  it("does not let an unrelated test satisfy another behavior domain", () => {
    assert.equal(
      codes([
        { filename: "internal/router/selector.go", patch: "+func pick() {}" },
        { filename: "gui/scripts/providers-fleet.test.ts", patch: "+test('fleet', () => {})" },
      ])[0],
      "missing_regression_test",
    );
    assert.equal(
      codes([
        { filename: "internal/router/selector.go", patch: "+func pick() {}" },
        { filename: "internal/combo/walker_test.go", patch: "+func TestWalk(t *testing.T) {}" },
      ])[0],
      "missing_regression_test",
    );
    assert.equal(
      codes([
        { filename: "gui/src/pages/Providers.tsx", patch: "+export function Providers() {}" },
        { filename: "internal/router/selector_test.go", patch: "+func TestPick(t *testing.T) {}" },
      ])[0],
      "missing_regression_test",
    );
    assert.equal(
      codes([
        { filename: "gui/src/pages/Providers.tsx", patch: "+export function Providers() {}" },
        { filename: "gui/scripts/models-groups.test.ts", patch: "+test('models', () => {})" },
      ])[0],
      "missing_regression_test",
    );
    assert.equal(
      codes([
        { filename: "cmd/benes/combo.go", patch: "+func set() {}" },
        { filename: "internal/router/selector_test.go", patch: "+func TestPick(t *testing.T) {}" },
      ])[0],
      "missing_regression_test",
    );
  });

  it("covers GUI sources imported by a changed GUI test", () => {
    assert.deepEqual(
      codes([
        { filename: "gui/src/lib/add-provider-form-policy.ts", patch: "+export function addProviderKeyConnectEnabled() {}" },
        { filename: "gui/scripts/add-provider-connection.test.ts", patch: "+test('connection', () => {})" },
      ]),
      [],
    );
    assert.deepEqual(
      codes([
        { filename: "gui/src/components/add-provider-session.ts", patch: "+export function createAddProviderSession() {}" },
        { filename: "gui/scripts/add-provider-connection.test.ts", patch: "+test('connection', () => {})" },
      ]),
      [],
    );
    assert.equal(
      codes([
        { filename: "gui/src/lib/add-provider-form-policy.ts", patch: "+export function addProviderKeyConnectEnabled() {}" },
        { filename: "gui/scripts/models-groups.test.ts", patch: "+test('models', () => {})" },
      ])[0],
      "missing_regression_test",
    );
    assert.deepEqual(
      codes([
        { filename: "gui/src/lib/add-provider-form-policy.ts", patch: "+export function addProviderKeyConnectEnabled() {}" },
        { filename: "gui/src/lib/add-provider-form-policy.test.ts", patch: "+test('policy', () => {})" },
      ]),
      [],
    );
    assert.deepEqual(
      codes([
        { filename: "gui/src/lib/add-provider-form-policy.ts", patch: "+export function addProviderKeyConnectEnabled() {}" },
        { filename: "gui/scripts/add-provider-form-policy.test.ts", patch: "+test('inspects policy copy', () => {})" },
      ]),
      [],
    );
  });

  const ADD_PROVIDER_HEAD = [
    'import { addProviderKeyConnectEnabled } from "../src/lib/add-provider-form-policy.ts";',
    'import { createAddProviderSession } from "../src/components/add-provider-session.ts";',
    'test("connection", () => {});',
  ].join("\n");
  const MODELS_HEAD = 'import { buildProviderModelGroups } from "../src/models-groups.ts";\ntest("models", () => {});\n';
  const HUNK_WITHOUT_IMPORTS = "@@\n+test('connection', () => {})";
  const SHA_TEST = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";

  function sparseCodes(files, contents = {}) {
    return evaluatePatchHygiene({
      files,
      readFile: readFileFromContents(contents),
    }).map((failure) => failure.code);
  }

  it("covers GUI imports from hydrated PR-head test contents in a sparse checkout", () => {
    const sourceAndTest = (source, testFile, contents) =>
      sparseCodes(
        [
          { filename: source, patch: "+export function changed() {}" },
          { filename: testFile, patch: HUNK_WITHOUT_IMPORTS, sha: SHA_TEST, status: "modified" },
        ],
        contents,
      );

    assert.deepEqual(
      sourceAndTest("gui/src/lib/add-provider-form-policy.ts", "gui/scripts/add-provider-connection.test.ts", {
        "gui/scripts/add-provider-connection.test.ts": ADD_PROVIDER_HEAD,
      }),
      [],
    );
    assert.deepEqual(
      sourceAndTest(
        "gui/src/components/add-provider-session.ts",
        "gui/scripts/add-provider-connection.test.ts",
        { "gui/scripts/add-provider-connection.test.ts": ADD_PROVIDER_HEAD },
      ),
      [],
    );
    assert.equal(
      sourceAndTest("gui/src/lib/add-provider-form-policy.ts", "gui/scripts/models-groups.test.ts", {
        "gui/scripts/models-groups.test.ts": MODELS_HEAD,
      })[0],
      "missing_regression_test",
    );
    assert.equal(
      sourceAndTest("gui/src/lib/add-provider-form-policy.ts", "gui/scripts/add-provider-connection.test.ts", {})[0],
      "missing_regression_test",
    );
  });

  it("uses the surviving PR-head test text, not a removed import or deleted test", () => {
    assert.equal(
      sparseCodes(
        [
          { filename: "gui/src/lib/add-provider-form-policy.ts", patch: "+export function changed() {}" },
          {
            filename: "gui/scripts/add-provider-connection.test.ts",
            patch: HUNK_WITHOUT_IMPORTS,
            status: "modified",
          },
        ],
        {
          "gui/scripts/add-provider-connection.test.ts":
            'import { createAddProviderSession } from "../src/components/add-provider-session.ts";\n',
        },
      )[0],
      "missing_regression_test",
    );
    assert.deepEqual(
      sparseCodes(
        [
          { filename: "gui/src/lib/add-provider-form-policy.ts", patch: "+export function changed() {}" },
          {
            filename: "gui/scripts/new-form-policy.test.ts",
            status: "added",
            patch: HUNK_WITHOUT_IMPORTS,
          },
        ],
        { "gui/scripts/new-form-policy.test.ts": ADD_PROVIDER_HEAD },
      ),
      [],
    );
    assert.deepEqual(
      sparseCodes(
        [
          { filename: "gui/src/lib/add-provider-form-policy.ts", patch: "+export function changed() {}" },
          {
            filename: "gui/scripts/add-provider-connection.test.ts",
            previous_filename: "gui/scripts/old-connection.test.ts",
            status: "renamed",
            patch: HUNK_WITHOUT_IMPORTS,
          },
        ],
        { "gui/scripts/add-provider-connection.test.ts": ADD_PROVIDER_HEAD },
      ),
      [],
    );
    assert.equal(
      sparseCodes(
        [
          { filename: "gui/src/lib/add-provider-form-policy.ts", patch: "+export function changed() {}" },
          {
            filename: "gui/scripts/add-provider-connection.test.ts",
            status: "removed",
            patch: "-import { addProviderKeyConnectEnabled } from \"../src/lib/add-provider-form-policy.ts\";",
          },
        ],
        { "gui/scripts/add-provider-connection.test.ts": ADD_PROVIDER_HEAD },
      )[0],
      "missing_regression_test",
    );
  });

  it("does not invent GUI import coverage from an unreadable head blob", () => {
    assert.equal(
      sparseCodes(
        [
          { filename: "gui/src/lib/add-provider-form-policy.ts", patch: "+export function changed() {}" },
          { filename: "gui/scripts/add-provider-connection.test.ts", patch: HUNK_WITHOUT_IMPORTS },
        ],
        {},
      )[0],
      "missing_regression_test",
    );
  });

describe("GUI test head hydration", () => {
  it("decodes only base64 git blobs and skips removed tests", () => {
    assert.equal(
      decodeGitBlob({ encoding: "base64", content: Buffer.from("ok", "utf8").toString("base64") }),
      "ok",
    );
    assert.equal(decodeGitBlob({ encoding: "utf-8", content: "nope" }), null);
    assert.equal(
      isSurvivingGuiTestFile({
        filename: "gui/scripts/add-provider-connection.test.ts",
        status: "removed",
      }),
      false,
    );
    assert.equal(
      isSurvivingGuiTestFile({
        filename: "gui/scripts/add-provider-connection.test.ts",
        status: "modified",
      }),
      true,
    );
  });

  it("loads surviving GUI tests from the listed head blob sha", async () => {
    const sha = "cccccccccccccccccccccccccccccccccccccccc";
    const text = 'import { addProviderKeyConnectEnabled } from "../src/lib/add-provider-form-policy.ts";\n';
    const github = {
      rest: {
        git: {
          getBlob: async ({ file_sha }) => {
            assert.equal(file_sha, sha);
            return {
              data: {
                encoding: "base64",
                content: Buffer.from(text, "utf8").toString("base64"),
              },
            };
          },
        },
      },
    };
    const contents = await loadGuiTestContentsAtHead({
      github,
      owner: "Wibias",
      repo: "Benes",
      files: [
        {
          filename: "gui/src/lib/add-provider-form-policy.ts",
          status: "modified",
          sha: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        },
        {
          filename: "gui/scripts/add-provider-connection.test.ts",
          status: "modified",
          sha,
        },
        {
          filename: "gui/scripts/gone.test.ts",
          status: "removed",
          sha: "dddddddddddddddddddddddddddddddddddddddd",
        },
      ],
    });
    assert.equal(contents["gui/scripts/add-provider-connection.test.ts"], text);
    assert.equal(contents["gui/src/lib/add-provider-form-policy.ts"], undefined);
    assert.equal(contents["gui/scripts/gone.test.ts"], undefined);
  });
});

  it("does not treat docs, root scripts, or a deleted test as coverage", () => {
    assert.deepEqual(
      codes([{ filename: "docs/src/content/docs/use/combos.md", patch: "+failover only" }]),
      [],
    );
    assert.deepEqual(
      codes([
        { filename: "internal/router/selector.go", patch: "+func pick() {}" },
        { filename: "internal/router/old_test.go", status: "removed", patch: "-func TestOld() {}" },
      ]),
      ["missing_regression_test"],
    );
  });

  it("ignores comment-only behavior edits and still flags mixed comment+code", () => {
    assert.deepEqual(
      codes([
        {
          filename: "internal/router/selector.go",
          patch: "@@\n+// fail closed when the selector is empty\n-// old wording",
        },
      ]),
      [],
    );
    assert.equal(
      codes([
        {
          filename: "internal/router/selector.go",
          patch: "@@\n+// note\n+func pick() {}",
        },
      ])[0],
      "missing_regression_test",
    );
  });

  it("treats pointer-dereference lines as code, not block-comment continuations", () => {
    assert.equal(
      isCommentOnlyPatch("@@\n+// keep the hop on the committed member", "internal/combo/walker.go"),
      true,
    );
    assert.equal(
      isCommentOnlyPatch(
        ["@@", " /*", "+ * still documenting the walker", " */"].join("\n"),
        "internal/combo/walker.go",
      ),
      true,
    );
    assert.equal(
      isCommentOnlyPatch("@@\n+*dst = next", "internal/combo/walker.go"),
      false,
    );
    assert.equal(
      isCommentOnlyPatch("@@\n+# listener bind is loopback", "internal/combo/walker.go"),
      false,
    );
    assert.equal(
      isCommentOnlyPatch("@@\n+# default integration branch", "scripts/ci/local-pr.sh"),
      true,
    );
    assert.equal(
      codes([
        {
          filename: "internal/combo/walker.go",
          patch: "@@\n+*dst = next",
        },
      ])[0],
      "missing_regression_test",
    );
  });

  it("does not carry block-comment state from one hunk into the next", () => {
    const patch = [
      "@@ -1,3 +1,4 @@",
      "+/* documenting the walker",
      "@@ -80,2 +81,3 @@",
      "+func hop() {}",
    ].join("\n");
    assert.equal(isCommentOnlyPatch(patch, "internal/combo/walker.go"), false);
    assert.equal(
      codes([{ filename: "internal/combo/walker.go", patch }])[0],
      "missing_regression_test",
    );
  });

  it("classifies renamed behavior files on both sides", () => {
    assert.equal(
      codes([
        {
          filename: "docs/moved.md",
          previous_filename: "internal/router/selector.go",
          patch: "",
        },
      ])[0],
      "missing_regression_test",
    );
    assert.deepEqual(
      codes([
        {
          filename: "docs/moved.md",
          previous_filename: "internal/router/selector.go",
          patch: "",
        },
        { filename: "internal/router/selector_test.go", patch: "+func TestPick(t *testing.T) {}" },
      ]),
      [],
    );
  });
});

describe("generated output and lockfile consistency", () => {
  it("fails committed dashboard build output and orphan lockfiles", () => {
    assert.deepEqual(
      codes([
        { filename: "gui/dist/index.js", patch: "+built" },
        { filename: "package-lock.json", patch: "+package" },
      ]),
      ["generated_output", "orphan_lockfile"],
    );
  });

  it("allows generated removals and a lockfile paired with package.json", () => {
    assert.deepEqual(
      codes([{ filename: "gui/dist/index.js", status: "removed", patch: "-built" }]),
      [],
    );
    assert.deepEqual(
      codes([
        { filename: "package-lock.json", patch: "+package" },
        { filename: "package.json", patch: "+name" },
      ]),
      [],
    );
  });

  it("does not treat a deleted lockfile as an orphan", () => {
    assert.deepEqual(
      codes([{ filename: "package-lock.json", status: "removed", patch: "@@\n-x" }]),
      [],
    );
  });

  it("still flags a lockfile renamed away from package.json", () => {
    assert.equal(
      codes([
        {
          filename: "lock/package-lock.json",
          previous_filename: "package-lock.json",
          status: "renamed",
          patch: "@@\n+x",
        },
      ])[0],
      "orphan_lockfile",
    );
  });
});

describe("added suppressions, skipped tests, and empty catches", () => {
  it("flags oxlint and eslint suppressions plus focused tests", () => {
    assert.equal(
      codes([
        {
          filename: "gui/scripts/providers-fleet.test.ts",
          patch: "+// oxlint-disable-next-line react/react-compiler\n+test('fleet', () => {});",
        },
      ])[0],
      "new_suppression",
    );
    assert.equal(
      codes([
        {
          filename: "gui/scripts/providers-fleet.test.ts",
          patch: "+test.only('fleet', () => {});",
        },
      ])[0],
      "focused_or_skipped_test",
    );
  });

  it("flags an empty catch, including a body deleted inside one hunk", () => {
    assert.equal(
      codes([
        {
          filename: "gui/scripts/providers-fleet.test.ts",
          patch: "+try { run(); } catch (error) {}",
        },
      ])[0],
      "empty_catch",
    );
    assert.equal(
      codes([
        {
          filename: "gui/scripts/providers-fleet.test.ts",
          patch: " catch (error) {\n-  report(error);\n }",
        },
      ])[0],
      "empty_catch",
    );
  });

  it("does not invent an empty catch across a hunk boundary", () => {
    const cross = [
      "@@ -10,2 +10,3 @@",
      "+  const a = 1;",
      "   } catch (error) {",
      "@@ -90,2 +90,3 @@",
      "   }",
      "+  const b = 2;",
    ].join("\n");
    assert.deepEqual(
      codes([{ filename: "gui/scripts/providers-fleet.test.ts", patch: cross }]),
      [],
    );
  });

  it("honours exception labels but never an empty-catch waiver", () => {
    assert.deepEqual(
      evaluatePatchHygiene({
        files: [
          {
            filename: "gui/src/pages/Providers.tsx",
            patch: "+// eslint-disable-next-line local-i18n/no-hardcoded-ui-strings\n+const id = 'x';",
          },
          { filename: "gui/dist/index.js", patch: "+built" },
          { filename: "package-lock.json", patch: "+package" },
        ],
        labels: [
          "test-exception-approved",
          "suppression-approved",
          "generated-change-approved",
          "dependency-change-approved",
        ],
      }),
      [],
    );
    assert.equal(
      evaluatePatchHygiene({
        files: [
          {
            filename: "gui/scripts/providers-fleet.test.ts",
            patch: "+try {} catch (error) {}",
          },
        ],
        labels: ["test-exception-approved", "suppression-approved"],
      })[0].code,
      "empty_catch",
    );
  });
});

describe("hygiene plus sponsorship", () => {
  it("combines missing tests and unsponsored restricted paths", () => {
    const failures = hygieneFailures({
      files: [{ filename: "internal/oauth/chatgpt/login.go", patch: "+change" }],
      authorHasPushPermission: false,
    });
    assert.deepEqual(
      failures.map((failure) => failure.code).sort(),
      ["missing_regression_test", "unsponsored_surface"],
    );
  });

  it("skips sponsorship when the author can push and tests are present", () => {
    assert.deepEqual(
      hygieneFailures({
        files: [
          { filename: "internal/oauth/chatgpt/login.go", patch: "+change" },
          { filename: "internal/oauth/chatgpt/login_test.go", patch: "+test" },
        ],
        authorHasPushPermission: true,
      }),
      [],
    );
  });

  it("still requires sponsorship of a rename source leaving a restricted path", () => {
    const failures = hygieneFailures({
      files: [
        {
          filename: "docs/moved-release.yml",
          previous_filename: ".github/workflows/release.yml",
          status: "renamed",
          patch: "+moved",
        },
      ],
      authorHasPushPermission: false,
    });
    const unsponsored = failures.find((failure) => failure.code === "unsponsored_surface");
    assert.ok(unsponsored);
    assert.deepEqual(unsponsored.paths, [".github/workflows/release.yml"]);
  });

  it("exposes hints and wake labels used by Ready coupling", () => {
    assert.equal(typeof HYGIENE_HINTS.unsponsored_surface, "string");
    assert.equal(WAKE_LABELS.includes("maintainer-sponsored"), true);
    assert.equal(WAKE_LABELS.includes("intake: hygiene-blocked"), true);
  });
});

describe("hygiene workflow trust boundary", () => {
  const workflow = [
    fs.readFileSync(path.join(__dirname, "../workflows/pr-hygiene.yml"), "utf8"),
    fs.readFileSync(path.join(__dirname, "pr-hygiene-run.cjs"), "utf8"),
  ].join("\n");

  const HYGIENE_REF =
    "${{ github.event.pull_request.base.ref == 'main' && 'main' || 'dev' }}";

  it("checks out trusted scripts from main or dev, never a PR-controlled ref", () => {
    const checkoutStep = workflow
      .split("- name: Checkout trusted hygiene script")[1]
      .split(/\n {6}- name:/)[0];
    const ref = checkoutStep.match(/^\s*ref:\s*(.+)$/m)?.[1];
    assert.ok(ref);
    assert.equal(ref.replace(/\s+/g, " ").trim(), HYGIENE_REF);
    assert.doesNotMatch(ref, /base\.sha|head\.(?:sha|ref)/);
    assert.match(checkoutStep, /persist-credentials:\s*false/);
  });

  it("never lets executable steps name the PR head or acquire PR code", () => {
    assert.doesNotMatch(workflow, /pull_request\.head\.(?:sha|ref|repo)/);
    assert.doesNotMatch(workflow, /refs\/pull\//);
    const checkouts =
      workflow.match(/uses:\s*actions\/checkout@[\s\S]*?(?=\n {6}- name:|$)/g) ?? [];
    for (const step of checkouts) {
      const stepRef = (step.match(/^\s*ref:\s*(.+)$/m)?.[1] ?? "")
        .replace(/\s+/g, " ")
        .trim();
      assert.equal(stepRef, HYGIENE_REF);
      assert.doesNotMatch(step, /repository:/);
    }
    const executable = workflow
      .split("\n")
      .filter((line) => !/^\s*#/.test(line))
      .join("\n");
    assert.doesNotMatch(
      executable,
      /github\.head_ref|pull_request(?:\[['"]head['"]\]|\.head)\s*(?:\[|\.)?\s*['"]?repo/,
    );
    assert.doesNotMatch(
      workflow,
      /gh\s+pr\s+checkout|git\s+(?:fetch|checkout|clone|switch)|refs\/pull/,
    );
  });

  it("uses collaborator permission level, not association arrays", () => {
    assert.match(workflow, /getCollaboratorPermissionLevel/);
    assert.match(workflow, /hasRepoPush\(/);
    assert.doesNotMatch(
      workflow,
      /hasRepoPush:\s*\["OWNER",\s*"MEMBER",\s*"COLLABORATOR"\]\.includes\(\s*pr\.author_association/,
    );
  });
});
