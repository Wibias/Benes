"use strict";

const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const { detectIssueKind, validateIssue } = require("./issue-quality.cjs");

const BUG = [
  "### Client that hit the failure",
  "Codex",
  "### What happened",
  "POST /v1/responses returned 200, then the SSE stream never sent a terminal event.",
  "### Commands that reproduce it",
  "go run ./cmd/benes start then curl -N http://127.0.0.1:23100/v1/responses",
  "### benes version or commit",
  "2.18.0",
  "### Host OS",
  "Windows 11 24H2",
].join("\n");

const FEATURE = [
  "### Job you need done",
  "When the first combo target is down, Codex on loopback should try the next target.",
  "### Why current Benes cannot do this",
  "combo_projection.go skips any strategy other than failover as unsupported_strategy.",
  "### Desired result and interface",
  "A 5xx from the first target continues to the next before any output.",
  "### One concrete interaction",
  "benes combo show cheap prints the ordered failover targets.",
].join("\n");

describe("valid live-form reports", () => {
  it("accepts a filled bug, feature, provider, and docs body", () => {
    assert.equal(detectIssueKind({ title: "SSE hang", body: BUG, labels: ["bug"] }), "bug");
    assert.equal(validateIssue({ title: "SSE hang", body: BUG, labels: ["bug"] }).valid, true);

    assert.equal(detectIssueKind({ title: "Combo hop", body: FEATURE, labels: ["enhancement"] }), "feature");
    assert.equal(validateIssue({ title: "Combo hop", body: FEATURE, labels: ["enhancement"] }).valid, true);

    const provider = [
      "### Upstream provider",
      "anthropic",
      "### Listener path or capability",
      "/v1/messages",
      "### What the listener returned",
      "400: system is required",
      "### Correct behaviour per spec or client",
      "The system field must be forwarded unchanged to the upstream API.",
      "### Smallest redacted request",
      "curl -X POST http://127.0.0.1:23100/v1/messages -d '{\"model\":\"x\"}'",
      "### benes version or commit",
      "2.18.0",
      "### Upstream spec",
      "https://docs.anthropic.com/en/api/messages",
    ].join("\n");
    assert.equal(
      detectIssueKind({ title: "system field dropped", body: provider, labels: ["provider-compatibility"] }),
      "provider-compatibility",
    );
    assert.equal(
      validateIssue({ title: "system field dropped", body: provider, labels: ["provider-compatibility"] }).valid,
      true,
    );

    const docs = [
      "### Page or file",
      "docs/src/content/docs/use/combos.md",
      "### What is wrong",
      "The page says combos use weighted round-robin.",
      "### What it should say",
      "Combos are failover only; any other strategy is skipped as unsupported_strategy.",
    ].join("\n");
    assert.equal(detectIssueKind({ title: "Combo docs", body: docs, labels: ["documentation"] }), "documentation");
    assert.equal(validateIssue({ title: "Combo docs", body: docs, labels: ["documentation"] }).valid, true);
  });
});

describe("title-prefix classification cannot bypass live forms", () => {
  it("rejects [Bug]: plus two arbitrary rich headings", () => {
    const body = [
      "### Notes",
      "The listener dropped the SSE stream after the first combo target and left Codex hanging without a terminal event.",
      "### Extra",
      "I tried several times on Windows 11 with benes 2.18.0 and the same hang returned every time.",
    ].join("\n");
    const result = validateIssue({ title: "[Bug]: SSE hang", body, labels: [] });
    assert.equal(detectIssueKind({ title: "[Bug]: SSE hang", body, labels: [] }), "bug");
    assert.equal(result.valid, false);
    assert.equal(result.softPass, false);
  });

  it("rejects [Feature]: plus arbitrary rich headings", () => {
    const body = [
      "### Context",
      "Operators need combo failover to hop before Codex sees any model tokens on the stream.",
      "### Wish",
      "Benes should try the next combo member after a 5xx from the first target.",
    ].join("\n");
    const result = validateIssue({ title: "[Feature]: combo hop", body, labels: [] });
    assert.equal(detectIssueKind({ title: "[Feature]: combo hop", body, labels: [] }), "feature");
    assert.equal(result.valid, false);
    assert.equal(result.softPass, false);
  });

  it("accepts non-English answers under live Bug headings", () => {
    const body = [
      "### Client that hit the failure",
      "Codex",
      "### What happened",
      "POST /v1/responses hat 200 geliefert, aber der SSE-Strom sendete kein Terminal-Event.",
      "### Commands that reproduce it",
      "go run ./cmd/benes start dann curl -N http://127.0.0.1:23100/v1/responses",
      "### benes version or commit",
      "2.18.0",
      "### Host OS",
      "Windows 11 24H2",
    ].join("\n");
    assert.equal(validateIssue({ title: "SSE hängt", body, labels: ["bug"] }).valid, true);
  });

  it("does not let extra headings fill a missing required slot", () => {
    const body = [
      "### Client that hit the failure",
      "Codex",
      "### What happened",
      "POST /v1/responses hung after the first combo target.",
      "### benes version or commit",
      "2.18.0",
      "### Host OS",
      "Windows 11 24H2",
      "### Extra diary",
      "I also restarted the listener and cleared gui/dist, then tried Codex again with the same model id.",
    ].join("\n");
    const result = validateIssue({ title: "SSE hang", body, labels: ["bug"] });
    assert.equal(result.valid, false);
  });
});

describe("insufficient and echoed reports", () => {
  it("rejects an empty bug and a reproduction that only repeats the failure", () => {
    const empty = validateIssue({ title: "SSE hang", body: "### What happened\n\n### Commands that reproduce it\n", labels: ["bug"] });
    assert.equal(empty.valid, false);
    const echoed = [
      "### Client that hit the failure",
      "Codex",
      "### What happened",
      "The listener dropped the SSE stream.",
      "### Commands that reproduce it",
      "The listener dropped the SSE stream.",
      "### benes version or commit",
      "2.18.0",
      "### Host OS",
      "Windows 11",
    ].join("\n");
    const result = validateIssue({ title: "SSE hang", body: echoed, labels: ["bug"] });
    assert.equal(result.valid, false);
    assert.equal(result.reasons.some((reason) => /restates the observation/i.test(reason)), true);
  });

  it("keeps nested headings and fenced code inside the parent section", () => {
    const body = [
      "### Commands that reproduce it",
      "Run this:",
      "```",
      "# not a section heading",
      "go run ./cmd/benes start",
      "```",
      "#### leftover notes",
      "still part of reproduction",
      "### What happened",
      "POST /v1/responses hung after the first combo target.",
      "### Client that hit the failure",
      "Codex",
      "### benes version or commit",
      "2.18.0",
      "### Host OS",
      "Windows 11 24H2",
    ].join("\n");
    assert.equal(validateIssue({ title: "SSE hang", body, labels: ["bug"] }).valid, true);
  });

  it("rejects placeholder-only, unknown environment, and duplicate sections", () => {
    const placeholder = validateIssue({
      title: "Docs typo",
      body: [
        "### Page or file",
        "docs/src/content/docs/use/combos.md",
        "### What is wrong",
        "TODO",
        "### What it should say",
        "Combos are failover only.",
      ].join("\n"),
      labels: ["documentation"],
    });
    assert.equal(placeholder.valid, false);

    const unknown = validateIssue({
      title: "SSE hang",
      body: [
        "### Client that hit the failure",
        "Codex",
        "### What happened",
        "POST /v1/responses hung after the first combo target.",
        "### Commands that reproduce it",
        "go run ./cmd/benes start then curl -N http://127.0.0.1:23100/v1/responses",
        "### benes version or commit",
        "unknown",
        "### Host OS",
        "Windows 11",
      ].join("\n"),
      labels: ["bug"],
    });
    assert.equal(unknown.valid, false);

    const duplicate = validateIssue({
      title: "Need combo hop",
      body: [
        "### Job you need done",
        "Need combo hop",
        "### Why current Benes cannot do this",
        "Need combo hop",
        "### Desired result and interface",
        "Need combo hop",
        "### One concrete interaction",
        "Need combo hop",
      ].join("\n"),
      labels: ["enhancement"],
    });
    assert.equal(duplicate.valid, false);
  });

  it("requires observed vs required behavior and an upstream reference for provider reports", () => {
    const identical = validateIssue({
      title: "system field dropped",
      body: [
        "### Upstream provider",
        "anthropic",
        "### Listener path or capability",
        "/v1/messages",
        "### What the listener returned",
        "system is required",
        "### Correct behaviour per spec or client",
        "system is required",
        "### Smallest redacted request",
        "curl -X POST http://127.0.0.1:23100/v1/messages",
        "### benes version or commit",
        "2.18.0",
        "### Upstream spec",
        "https://docs.anthropic.com/en/api/messages",
      ].join("\n"),
      labels: ["provider-compatibility"],
    });
    assert.equal(identical.valid, false);

    const noSpec = validateIssue({
      title: "system field dropped",
      body: [
        "### Upstream provider",
        "anthropic",
        "### Listener path or capability",
        "/v1/messages",
        "### What the listener returned",
        "400: system is required",
        "### Correct behaviour per spec or client",
        "The system field must be forwarded unchanged.",
        "### Smallest redacted request",
        "curl -X POST http://127.0.0.1:23100/v1/messages",
        "### benes version or commit",
        "2.18.0",
        "### Upstream spec",
        "",
      ].join("\n"),
      labels: ["provider-compatibility"],
    });
    assert.equal(noSpec.valid, false);

    const declared = validateIssue({
      title: "system field dropped",
      body: [
        "### Upstream provider",
        "anthropic",
        "### Listener path or capability",
        "/v1/messages",
        "### What the listener returned",
        "400: system is required",
        "### Correct behaviour per spec or client",
        "The system field must be forwarded unchanged.",
        "### Smallest redacted request",
        "curl -X POST http://127.0.0.1:23100/v1/messages",
        "### benes version or commit",
        "2.18.0",
        "### Upstream spec",
        "no public spec",
      ].join("\n"),
      labels: ["provider-compatibility"],
    });
    assert.equal(declared.valid, true);
  });

  it("does not soft-pass an untemplated body just because it has two long sections", () => {
    const body = [
      "### Notes",
      "The listener dropped the SSE stream after the first combo target and left Codex hanging.",
      "### Extra",
      "I tried several times on Windows 11 with benes 2.18.0 and the same hang returned.",
    ].join("\n");
    const result = validateIssue({ title: "SSE hang", body, labels: [] });
    assert.equal(result.valid, false);
    assert.equal(result.softPass, false);
  });

  it("still classifies legacy feature headings", () => {
    const text = [
      "### What workflow do you want?",
      "Route loopback traffic to a fallback provider when quota is exhausted.",
      "### What is missing or blocked in Benes now?",
      "Combo members stay on the first candidate after visible output.",
      "### What should the listener or CLI do?",
      "Hop to the next combo target after a 5xx before any output.",
      "### Example command, config, or request",
      "benes combo set cheap --strategy failover --target ollama/llama3",
    ].join("\n");
    assert.equal(detectIssueKind({ title: "Fallback routing", body: text, labels: ["enhancement"] }), "feature");
    assert.equal(validateIssue({ title: "Fallback routing", body: text, labels: ["enhancement"] }).valid, true);
  });
});
