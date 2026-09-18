"use strict";

const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const parse = require("./issue-intake-parse.cjs");

describe("fenced section parsing", () => {
  it("keeps ### fake inside triple backticks from becoming a section", () => {
    const body = [
      "### What happened",
      "The SSE stream hung.",
      "```",
      "### fake",
      "go run ./cmd/benes start",
      "```",
      "### Commands that reproduce it",
      "curl -N http://127.0.0.1:23100/v1/responses",
    ].join("\n");
    const sections = parse.parseSections(body);
    assert.deepEqual(
      sections.map((section) => section.title),
      ["What happened", "Commands that reproduce it"],
    );
    assert.match(sections[0].text, /### fake/);
  });

  it("keeps a triple-backtick sequence inside quadruple backticks from closing early", () => {
    const body = [
      "### What happened",
      "````markdown",
      "```",
      "### fake",
      "```",
      "````",
      "### Host OS",
      "Windows 11",
    ].join("\n");
    const sections = parse.parseSections(body);
    assert.deepEqual(
      sections.map((section) => section.title),
      ["What happened", "Host OS"],
    );
    assert.match(sections[0].text, /### fake/);
  });

  it("treats tilde fences the same way as backtick fences", () => {
    const triple = [
      "### What happened",
      "~~~",
      "### fake",
      "~~~",
      "### Host OS",
      "Windows 11",
    ].join("\n");
    const four = [
      "### What happened",
      "~~~~",
      "### fake",
      "~~~~",
      "### Host OS",
      "Windows 11",
    ].join("\n");
    assert.deepEqual(
      parse.parseSections(triple).map((section) => section.title),
      ["What happened", "Host OS"],
    );
    assert.deepEqual(
      parse.parseSections(four).map((section) => section.title),
      ["What happened", "Host OS"],
    );
  });

  it("starts a real ### heading only after the fence closes", () => {
    const body = [
      "```",
      "### fake",
      "```",
      "### What happened",
      "POST /v1/responses hung.",
    ].join("\n");
    assert.deepEqual(
      parse.parseSections(body).map((section) => section.title),
      ["What happened"],
    );
  });

  it("keeps nested #### headings inside the parent ### section", () => {
    const body = [
      "### Commands that reproduce it",
      "Run this:",
      "#### leftover notes",
      "still part of reproduction",
      "### What happened",
      "The walker stopped after the first target.",
    ].join("\n");
    const sections = parse.parseSections(body);
    assert.match(sections[0].text, /leftover notes/);
    assert.equal(sections[1].title, "What happened");
  });

  it("does not manufacture headings from an unclosed fence", () => {
    const body = [
      "### What happened",
      "```",
      "### fake",
      "still fenced",
    ].join("\n");
    const sections = parse.parseSections(body);
    assert.deepEqual(
      sections.map((section) => section.title),
      ["What happened"],
    );
    assert.match(sections[0].text, /### fake/);
  });

  it("extracts live-form headings after fences", () => {
    const body = [
      "### Client that hit the failure",
      "Codex",
      "### What happened",
      "```",
      "POST /v1/responses",
      "```",
      "hung after the first combo target.",
    ].join("\n");
    assert.equal(parse.resolveSection(body, ["What happened"]).includes("hung after"), true);
    assert.equal(parse.hasHeading(body, "Client that hit the failure"), true);
  });

  it("does not treat HTML comments as usable evidence", () => {
    assert.equal(parse.isEmpty("<!-- looks like a reproduction -->"), true);
    assert.equal(parse.isEmpty("curl http://127.0.0.1:23100/v1/responses"), false);
  });
});
