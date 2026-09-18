"use strict";

const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const {
  extractIssueForm,
  validateIssueForm,
  validateIssueForms,
  validateConfigYml,
  inspectTemplateDir,
  FORM_CONTRACTS,
} = require("./validate-issue-forms.cjs");

const ROOT = path.join(__dirname, "..", "..");
const FORMS_DIR = path.join(ROOT, ".github", "ISSUE_TEMPLATE");
const VALIDATOR_SRC = fs.readFileSync(path.join(__dirname, "validate-issue-forms.cjs"), "utf8");

function defectSkeleton(overrides = {}) {
  const body = [
    {
      type: "markdown",
      attributes: { value: "Do not paste credentials." },
    },
    {
      type: "dropdown",
      id: "product-surface",
      attributes: { label: "Which Benes surface failed", options: ["CLI", "Other"] },
      validations: { required: true },
    },
    {
      type: "dropdown",
      id: "talking-client",
      attributes: { label: "Client that hit the failure", options: ["Codex", "Other"] },
      validations: { required: true },
    },
    {
      type: "textarea",
      id: "happened",
      attributes: { label: "What happened" },
      validations: { required: true },
    },
    {
      type: "textarea",
      id: "expected-result",
      attributes: { label: "What should have happened" },
      validations: { required: true },
    },
    {
      type: "textarea",
      id: "rerun",
      attributes: { label: "Commands that reproduce it" },
      validations: { required: true },
    },
    {
      type: "input",
      id: "benes-build",
      attributes: { label: "benes version or commit" },
      validations: { required: true },
    },
    {
      type: "input",
      id: "host-os",
      attributes: { label: "Host OS" },
      validations: { required: true },
    },
    {
      type: "checkboxes",
      id: "secret-screen",
      attributes: {
        label: "Redaction",
        options: [
          {
            label: "This report contains no tokens, API keys, cookies, or Authorization headers.",
            required: true,
          },
        ],
      },
    },
  ];
  return {
    name: "Bug report",
    description: "A reproducible Benes failure.",
    labels: ["bug"],
    body,
    ...overrides,
  };
}

function toYaml(doc) {
  const lines = [];
  lines.push(`name: ${doc.name}`);
  lines.push(`description: ${doc.description}`);
  if (doc.labels) {
    lines.push("labels:");
    for (const label of doc.labels) lines.push(`  - ${label}`);
  }
  lines.push("body:");
  for (const el of doc.body) {
    lines.push(`  - type: ${el.type}`);
    if (el.id) lines.push(`    id: ${el.id}`);
    if (el.attributes) {
      lines.push("    attributes:");
      if (el.attributes.label) lines.push(`      label: ${el.attributes.label}`);
      if (el.attributes.value) {
        lines.push("      value: |");
        for (const line of String(el.attributes.value).split("\n")) {
          lines.push(`        ${line}`);
        }
      }
      if (el.attributes.options) {
        lines.push("      options:");
        for (const opt of el.attributes.options) {
          if (typeof opt === "string") {
            lines.push(`        - ${opt}`);
          } else {
            lines.push(`        - label: ${opt.label}`);
            if (opt.required) lines.push("          required: true");
          }
        }
      }
    }
    if (el.validations?.required) {
      lines.push("    validations:");
      lines.push("      required: true");
    }
  }
  return `${lines.join("\n")}\n`;
}

describe("issue-form extractor", () => {
  it("reads chooser fields and skips markdown blocks", () => {
    const doc = extractIssueForm(
      [
        "name: Bug report",
        "description: A failure",
        "labels:",
        "  - bug",
        "body:",
        "  - type: markdown",
        "    attributes:",
        "      value: |",
        "        ignored help copy",
        "        still ignored",
        "  - type: textarea",
        "    id: happened",
        "    attributes:",
        "      label: What happened",
        "    validations:",
        "      required: true",
        "  - type: checkboxes",
        "    id: secret-screen",
        "    attributes:",
        "      label: Redaction",
        "      options:",
        "        - label: This report contains no tokens or API keys.",
        "          required: true",
      ].join("\n"),
    );
    assert.equal(doc.name, "Bug report");
    assert.deepEqual(doc.labels, ["bug"]);
    assert.equal(doc.body[0].type, "markdown");
    assert.equal(doc.body[1].id, "happened");
    assert.equal(doc.body[1].label, "What happened");
    assert.equal(doc.body[1].required, true);
    assert.equal(doc.body[2].checkboxOptions[0].required, true);
  });
});

describe("form contract", () => {
  it("accepts a complete Bug report document", () => {
    const result = validateIssueForm("bug_report.yml", toYaml(defectSkeleton()));
    assert.deepEqual(result.errors, []);
    assert.ok(result.ids >= 7);
  });

  it("rejects missing structure, unknown field types, and repeated ids", () => {
    const raw = [
      "body:",
      "  - type: widget",
      "    id: happened",
      "  - type: textarea",
      "    id: happened",
    ].join("\n");
    const result = validateIssueForm("bug_report.yml", raw);
    assert.ok(result.errors.includes("form is missing a name"));
    assert.ok(result.errors.includes("form is missing a description"));
    assert.ok(result.errors.some((error) => error.includes("unsupported field type 'widget'")));
    assert.ok(result.errors.some((error) => error.includes("duplicate field ids: happened")));
  });

  it("rejects a Bug report whose credential acknowledgement disappeared", () => {
    const doc = defectSkeleton();
    doc.body = doc.body.filter((el) => el.id !== "secret-screen");
    const result = validateIssueForm("bug_report.yml", toYaml(doc));
    assert.ok(
      result.errors.includes("missing required credential-redaction acknowledgement"),
    );
  });

  it("rejects a Feature request that drops the concrete-interaction slot", () => {
    const raw = fs.readFileSync(path.join(FORMS_DIR, "feature_request.yml"), "utf8");
    const stripped = raw
      .split("\n")
      .filter((line) => !line.includes("concrete-interaction") && !line.includes("One concrete interaction"))
      .join("\n");
    const result = validateIssueForm("feature_request.yml", stripped);
    assert.ok(result.errors.some((error) => error.includes("concrete-interaction")));
  });

  it("does not fail when help copy changes without dropping a required slot", () => {
    const doc = defectSkeleton();
    doc.body[3].attributes.label = "Observed failure";
    doc.body[0].attributes.value = "rewritten help text that is not part of the contract";
    const result = validateIssueForm("bug_report.yml", toYaml(doc));
    assert.deepEqual(result.errors, []);
  });

  it("does not freeze field order", () => {
    const doc = defectSkeleton();
    const [intro, surface, client, ...rest] = doc.body;
    doc.body = [intro, client, surface, ...rest];
    const result = validateIssueForm("bug_report.yml", toYaml(doc));
    assert.deepEqual(result.errors, []);
  });

  it("ignores config.yml when scanning form files", () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "benes-templates-"));
    try {
      fs.writeFileSync(path.join(dir, "config.yml"), "blank_issues_enabled: false\n");
      fs.writeFileSync(path.join(dir, "bug_report.yml"), toYaml(defectSkeleton()));
      const results = validateIssueForms(dir);
      assert.equal(results.length, 1);
      assert.equal(results[0].file, "bug_report.yml");
      assert.deepEqual(results[0].errors, []);
    } finally {
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });
});

describe("repository templates", () => {
  it("does not import the issue-intake catalog or implement a generic YAML parser", () => {
    assert.doesNotMatch(VALIDATOR_SRC, /issue-intake-catalog/);
    assert.doesNotMatch(VALIDATOR_SRC, /function parseYaml/);
    assert.doesNotMatch(VALIDATOR_SRC, /function parseMap/);
    assert.doesNotMatch(VALIDATOR_SRC, /function parseSeq/);
  });

  it("keeps blank issues disabled and a private advisory contact", () => {
    assert.deepEqual(validateConfigYml(FORMS_DIR), []);
  });

  it("accepts every shipped chooser form against the Benes contract", () => {
    const { forms, config } = inspectTemplateDir(FORMS_DIR);
    assert.deepEqual(config.errors, []);
    const names = forms.map((result) => result.file).sort();
    assert.deepEqual(names, Object.keys(FORM_CONTRACTS).sort());
    for (const result of forms) {
      assert.deepEqual(result.errors, [], `${result.file}: ${result.errors.join("; ")}`);
    }
  });
});
