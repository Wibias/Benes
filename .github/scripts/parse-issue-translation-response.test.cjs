"use strict";

const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { parseAiResponse, main } = require("./parse-issue-translation-response.cjs");

describe("translation response parse", () => {
  it("reads a JSON object and writes GitHub output without applying a title rewrite", () => {
    assert.equal(parseAiResponse('{"requires_translation":false}').requires_translation, false);
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "benes-translation-out-"));
    const out = path.join(dir, "github-output");
    fs.writeFileSync(out, "");
    const prevOut = process.env.GITHUB_OUTPUT;
    const prevAi = process.env.AI_RESPONSE;
    process.env.GITHUB_OUTPUT = out;
    process.env.AI_RESPONSE = JSON.stringify({
      requires_translation: true,
      detected_language: "German",
      translated_title: "Combo hop",
      translated_body: "Hop before visible output.",
    });
    try {
      main();
    } finally {
      process.env.GITHUB_OUTPUT = prevOut;
      process.env.AI_RESPONSE = prevAi;
    }
    const text = fs.readFileSync(out, "utf8");
    assert.match(text, /requires_translation=true/);
    assert.match(text, /translated_title=Combo hop/);
    assert.match(text, /source_complete=false/);
  });
});
