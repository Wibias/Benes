"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const {
  classifyTitle,
  classifyCommitMessages,
  humanLockedTypeLabels,
  planManagedTypeLabels,
  MANAGED_TYPE_LABELS,
} = require("./pr-labeler.cjs");

describe("title intents used by Benes", () => {
  it("maps conventional prefixes this repository actually ships", () => {
    assert.equal(classifyTitle("feat(gui): add the Harnesses board"), "enhancement");
    assert.equal(classifyTitle("feature: extra catalog filter"), "enhancement");
    assert.equal(classifyTitle("fix(kiro): rotate 429 toward quota headroom"), "bug");
    assert.equal(classifyTitle("bugfix: restore listener bind"), "bug");
    assert.equal(classifyTitle("hotfix: close leaked SSE stream"), "bug");
    assert.equal(classifyTitle("docs: describe combo failover"), "documentation");
    assert.equal(classifyTitle("doc: note the default bind"), "documentation");
    assert.equal(classifyTitle("chore: pin checkout SHA"), "chore");
    assert.equal(classifyTitle("chore!: drop leftover inject path"), "chore");
    assert.equal(classifyTitle("refactor: split quota window math"), "chore");
    assert.equal(classifyTitle("test: cover kiro 429 rotation"), "chore");
    assert.equal(classifyTitle("ci: keep local-pr gate"), "chore");
    assert.equal(classifyTitle("build: bump go directive"), "chore");
  });

  it("does not invent a type from a sentence or an unknown prefix", () => {
    assert.equal(classifyTitle("Fix Kiro 429 rotation in the listener"), null);
    assert.equal(classifyTitle("Routing leftover hygiene"), null);
    assert.equal(classifyTitle("constructor: drop unused helper"), null);
    assert.equal(classifyTitle("perf: poll quotas less often"), null);
    assert.equal(classifyTitle("style: align dashboard dividers"), null);
    assert.equal(classifyTitle("revert: undo nested inject"), null);
    assert.equal(classifyTitle("Fix"), null);
    assert.equal(classifyTitle(""), null);
  });
});

describe("commit fallback only when the title is unclassified", () => {
  it("uses a unanimous commit type", () => {
    assert.equal(
      classifyCommitMessages([
        "fix(kiro): rotate 429 toward quota headroom",
        "fix(kiro): keep the exhausted account paused",
      ]),
      "bug",
    );
  });

  it("treats test/ci/build/refactor/chore commits as supporting a single product intent", () => {
    assert.equal(
      classifyCommitMessages([
        "fix(kiro): rotate 429 toward quota headroom",
        "test(kiro): assert the hop order",
      ]),
      "bug",
    );
    assert.equal(
      classifyCommitMessages([
        "feat(gui): add Harnesses",
        "test(gui): cover the board",
      ]),
      "enhancement",
    );
  });

  it("abstains when product intents conflict", () => {
    assert.equal(
      classifyCommitMessages([
        "fix(kiro): rotate 429",
        "feat(gui): add Harnesses",
      ]),
      null,
    );
  });

  it("keeps chore when that is the only classified type", () => {
    assert.equal(classifyCommitMessages(["ci: pin an action", "test: add a case"]), "chore");
  });

  it("reads only the first line of each commit", () => {
    assert.equal(
      classifyCommitMessages(["fix(kiro): rotate 429\n\nfeat: this body is not a type"]),
      "bug",
    );
  });
});

describe("human freeze of managed type labels", () => {
  it("stays automated when only github-actions[bot] touched type labels", () => {
    assert.equal(
      humanLockedTypeLabels([
        {
          event: "labeled",
          label: { name: "bug" },
          actor: { login: "github-actions[bot]" },
        },
      ]),
      false,
    );
  });

  it("freezes after any human add or remove of a managed type", () => {
    assert.equal(
      humanLockedTypeLabels([
        {
          event: "unlabeled",
          label: { name: "bug" },
          actor: { login: "Wibias" },
        },
      ]),
      true,
    );
  });

  it("ignores human edits of labels this module does not manage", () => {
    assert.equal(
      humanLockedTypeLabels([
        {
          event: "labeled",
          label: { name: "needs-triage" },
          actor: { login: "Wibias" },
        },
      ]),
      false,
    );
  });
});

describe("label plan", () => {
  it("adds the title type and drops competing managed labels", () => {
    const plan = planManagedTypeLabels({
      title: "fix(kiro): rotate 429 toward quota headroom",
      currentLabels: ["enhancement", "needs-triage"],
      events: [
        {
          event: "labeled",
          label: { name: "enhancement" },
          actor: { login: "github-actions[bot]" },
        },
      ],
    });
    assert.deepEqual(plan, {
      apply: true,
      type: "bug",
      add: "bug",
      remove: ["enhancement"],
    });
    assert.equal(MANAGED_TYPE_LABELS.has("bug"), true);
  });

  it("does not let commits override a classified title", () => {
    const plan = planManagedTypeLabels({
      title: "feat(gui): add the Harnesses board",
      currentLabels: [],
      events: [],
      commitMessages: ["fix(kiro): rotate 429", "fix(kiro): pause exhausted"],
    });
    assert.equal(plan.type, "enhancement");
  });

  it("labels an unclassified title from unanimous commits", () => {
    const plan = planManagedTypeLabels({
      title: "follow-up 2/3: leftover listener hops",
      currentLabels: [],
      events: [],
      commitMessages: [
        "fix(kiro): rotate 429 toward quota headroom",
        "fix(responses): close passthrough streams at terminal events",
      ],
    });
    assert.deepEqual(plan, { apply: true, type: "bug", add: "bug", remove: [] });
  });

  it("skips when a human froze a managed type", () => {
    const plan = planManagedTypeLabels({
      title: "fix(kiro): rotate 429 toward quota headroom",
      currentLabels: ["enhancement"],
      events: [
        {
          event: "labeled",
          label: { name: "enhancement" },
          actor: { login: "Wibias" },
        },
      ],
    });
    assert.deepEqual(plan, { apply: false, reason: "human-override" });
  });
});

describe("labeler workflow contract", () => {
  const workflow = fs.readFileSync(
    path.join(__dirname, "../workflows/pr-labeler.yml"),
    "utf8",
  );

  it("wakes on labeled, unlabeled, and synchronize", () => {
    const match = workflow.match(
      /pull_request_target:\s*\n(?:[ \t].*\n)*?[ \t]+types:\s*\[([^\]]+)\]/,
    );
    assert.ok(match);
    const types = match[1].split(",").map((type) => type.trim());
    assert.equal(types.includes("labeled"), true);
    assert.equal(types.includes("unlabeled"), true);
    assert.equal(types.includes("synchronize"), true);
  });

  it("checks out the trusted integration branch, cancels in-flight runs, and stays contents:read", () => {
    assert.match(
      workflow,
      /ref:\s*\$\{\{\s*github\.event\.pull_request\.base\.ref == 'main' && 'main' \|\| 'dev'\s*\}\}/,
    );
    assert.doesNotMatch(workflow, /default_branch/);
    assert.doesNotMatch(
      workflow,
      /ref:\s*\$\{\{\s*github\.event\.pull_request\.base\.ref\s*\}\}/,
    );
    assert.match(workflow, /cancel-in-progress:\s*true/);
    assert.match(workflow, /issues:\s*write/);
    assert.match(workflow, /pull-requests:\s*write/);
    assert.match(workflow, /contents:\s*read/);
    assert.doesNotMatch(workflow, /contents:\s*write/);
    assert.match(workflow, /persist-credentials:\s*false/);
    assert.match(workflow, /runPrLabeler/);
  });
});
