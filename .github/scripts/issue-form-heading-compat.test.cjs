"use strict";

const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const {
  detectIssueKind,
  validateIssue,
  mapAreaFieldToLabels,
} = require("./issue-quality.cjs");
const catalog = require("./issue-intake-catalog.cjs");
const {
  extractIssueForm,
  inspectTemplateDir,
  FORM_CONTRACTS,
} = require("./validate-issue-forms.cjs");

const FORMS_DIR = path.join(__dirname, "..", "ISSUE_TEMPLATE");

const INTAKE_ROLES = {
  "bug_report.yml": {
    "talking-client": catalog.BUG.client,
    happened: catalog.BUG.failure,
    rerun: catalog.BUG.reproduction,
    "benes-build": catalog.BUG.version,
    "host-os": catalog.BUG.os,
    "product-surface": catalog.AREA_FIELD_HEADINGS,
  },
  "feature_request.yml": {
    job: catalog.FEATURE.goal,
    "blocked-by": catalog.FEATURE.limitation,
    "ship-shape": catalog.FEATURE.behaviour,
    "concrete-interaction": catalog.FEATURE.example,
    landing: catalog.AREA_FIELD_HEADINGS,
  },
  "provider_compatibility.yml": {
    vendor: catalog.PROVIDER.providerName,
    "listener-path": catalog.PROVIDER.endpoint,
    "benes-build": catalog.PROVIDER.benesVersion,
    "spec-says": catalog.PROVIDER.expectedBehaviour,
    "listener-returned": catalog.PROVIDER.currentBehaviour,
    "smallest-request": catalog.PROVIDER.request,
    "spec-url": catalog.PROVIDER.upstreamDocs,
  },
  "documentation.yml": {
    "doc-path": catalog.DOCS.location,
    inaccuracy: catalog.DOCS.problem,
    accurate: catalog.DOCS.expected,
  },
};

const KIND_BY_FILE = {
  "bug_report.yml": "bug",
  "feature_request.yml": "feature",
  "provider_compatibility.yml": "provider-compatibility",
  "documentation.yml": "documentation",
};

const SAMPLE_BY_ID = {
  "product-surface": "Loopback proxy",
  "talking-client": "Codex",
  happened:
    "POST /v1/responses returned 200, then the SSE stream never sent a terminal event.",
  "expected-result": "The stream should close after the last output delta.",
  rerun: "go run ./cmd/benes start then curl -N http://127.0.0.1:23100/v1/responses",
  "benes-build": "0.1.0-preview.0",
  "host-os": "Windows 11 24H2",
  job: "When the first combo target is down, Codex on loopback should try the next target.",
  "blocked-by":
    "combo_projection.go skips any strategy other than failover as unsupported_strategy.",
  "ship-shape": "A 5xx from the first target continues to the next before any output.",
  "concrete-interaction": "benes combo show cheap prints the ordered failover targets.",
  landing: "Loopback proxy",
  vendor: "anthropic",
  "listener-path": "/v1/messages",
  "spec-says": "The system field must be forwarded unchanged to the upstream API.",
  "listener-returned": "400: system is required",
  "smallest-request":
    "curl -X POST http://127.0.0.1:23100/v1/messages -d '{\"model\":\"x\"}'",
  "spec-url": "https://docs.anthropic.com/en/api/messages",
  "doc-path": "docs/src/content/docs/use/combos.md",
  inaccuracy: "The page says combos use weighted round-robin.",
  accurate: "Combos are failover only; any other strategy is skipped as unsupported_strategy.",
};

function liveForm(file) {
  return extractIssueForm(fs.readFileSync(path.join(FORMS_DIR, file), "utf8"));
}

function fieldById(doc, id) {
  return doc.body.find((el) => el.id === id) || null;
}

function renderedBody(file, omitIds = new Set()) {
  const doc = liveForm(file);
  const rows = [];
  for (const slot of FORM_CONTRACTS[file].slots) {
    if (omitIds.has(slot.id)) continue;
    const field = fieldById(doc, slot.id);
    assert.ok(field, `${file} is missing slot ${slot.id}`);
    rows.push(`### ${field.label}`);
    rows.push(SAMPLE_BY_ID[slot.id] || "filled");
  }
  return rows.join("\n");
}

describe("live chooser labels stay in the intake catalog", () => {
  it("maps every automation-consumed live label onto its catalog role", () => {
    for (const [file, roles] of Object.entries(INTAKE_ROLES)) {
      const doc = liveForm(file);
      for (const [id, headings] of Object.entries(roles)) {
        const field = fieldById(doc, id);
        assert.ok(field, `${file} lost field ${id}`);
        assert.ok(
          headings.includes(field.label),
          `${file} field ${id} label ${JSON.stringify(field.label)} is not in its catalog role`,
        );
      }
    }
  });

  it("maps every live area-dropdown option", () => {
    for (const file of ["bug_report.yml", "feature_request.yml"]) {
      const doc = liveForm(file);
      const areaId = file === "bug_report.yml" ? "product-surface" : "landing";
      const field = fieldById(doc, areaId);
      assert.ok(field, `${file} lost ${areaId}`);
      for (const option of field.options) {
        const key = String(option).trim().toLowerCase();
        assert.ok(
          Object.hasOwn(catalog.AREA_DROPDOWN_TO_LABEL, key),
          `${file} option ${JSON.stringify(option)} is not in AREA_DROPDOWN_TO_LABEL`,
        );
      }
    }
  });
});

describe("bodies rendered from live labels still classify", () => {
  it("accepts a filled body for each chooser form", () => {
    const titles = {
      "bug_report.yml": "SSE hang",
      "feature_request.yml": "Combo hop",
      "provider_compatibility.yml": "system field dropped",
      "documentation.yml": "Combo docs",
    };
    const labelSets = {
      "bug_report.yml": ["bug"],
      "feature_request.yml": ["enhancement"],
      "provider_compatibility.yml": ["provider-compatibility"],
      "documentation.yml": ["documentation"],
    };
    for (const file of Object.keys(FORM_CONTRACTS)) {
      const text = renderedBody(file);
      const kind = KIND_BY_FILE[file];
      assert.equal(
        detectIssueKind({ title: titles[file], body: text, labels: labelSets[file] }),
        kind,
        file,
      );
      const result = validateIssue({
        title: titles[file],
        body: text,
        labels: labelSets[file],
      });
      assert.equal(result.valid, true, `${file}: ${result.reasons.join("; ")}`);
    }
  });

  it("requires desired behaviour and a concrete interaction independently", () => {
    const title = "Combo hop";
    const labels = ["enhancement"];
    const full = renderedBody("feature_request.yml");
    assert.equal(validateIssue({ title, body: full, labels }).valid, true);

    const withoutExample = renderedBody("feature_request.yml", new Set(["concrete-interaction"]));
    const missingExample = validateIssue({ title, body: withoutExample, labels });
    assert.equal(missingExample.valid, false);
    assert.ok(
      missingExample.reasons.some((reason) => /concrete interaction/i.test(reason)),
      missingExample.reasons.join("; "),
    );

    const withoutBehaviour = renderedBody("feature_request.yml", new Set(["ship-shape"]));
    const missingBehaviour = validateIssue({ title, body: withoutBehaviour, labels });
    assert.equal(missingBehaviour.valid, false);
    assert.ok(
      missingBehaviour.reasons.some((reason) => /desired behavior/i.test(reason)),
      missingBehaviour.reasons.join("; "),
    );
  });

  it("still classifies a historical Feature request that used legacy headings", () => {
    const text = [
      "### Area",
      "CLI",
      "### What workflow do you want?",
      "Route loopback traffic to a fallback provider when quota is exhausted.",
      "### What is missing or blocked in Benes now?",
      "Combo members stay on the first candidate after visible output.",
      "### What should the listener or CLI do?",
      "Hop to the next combo target after a 5xx before any output.",
      "### Example command, config, or request",
      "benes combo set cheap --strategy failover --target ollama/llama3",
    ].join("\n");
    assert.equal(
      detectIssueKind({ title: "Fallback routing", body: text, labels: ["enhancement"] }),
      "feature",
    );
    const result = validateIssue({
      title: "Fallback routing",
      body: text,
      labels: ["enhancement"],
    });
    assert.equal(result.valid, true, result.reasons.join("; "));
  });

  it("maps the live surface dropdown values", () => {
    assert.deepEqual(mapAreaFieldToLabels("Loopback proxy"), ["proxy"]);
    assert.deepEqual(mapAreaFieldToLabels("Credentials and OAuth"), ["account-pool"]);
    assert.deepEqual(mapAreaFieldToLabels("Model catalog"), ["catalog"]);
    assert.deepEqual(mapAreaFieldToLabels("Install and packaging"), ["install"]);
    assert.deepEqual(mapAreaFieldToLabels("Docs"), []);
    assert.deepEqual(mapAreaFieldToLabels("Unsure"), []);
  });

  it("does not treat help-copy edits as an intake break", () => {
    const { forms } = inspectTemplateDir(FORMS_DIR);
    const live = forms.find((result) => result.file === "bug_report.yml");
    assert.deepEqual(live.errors, []);
    const mutated = fs
      .readFileSync(path.join(FORMS_DIR, "bug_report.yml"), "utf8")
      .replace("Name the build and the steps.", "Name the build, the bind, and the steps.");
    const { validateIssueForm } = require("./validate-issue-forms.cjs");
    assert.deepEqual(validateIssueForm("bug_report.yml", mutated).errors, []);
  });
});
