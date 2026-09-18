"use strict";

const fs = require("node:fs");
const crypto = require("node:crypto");
const { parseModelJson, sanitizeLanguageTag } = require("./issue-translation.cjs");

function appendLine(key, value) {
  fs.appendFileSync(process.env.GITHUB_OUTPUT, `${key}=${value}\n`);
}

function appendHeredoc(key, value) {
  const delim = `${key.toUpperCase()}_${crypto.randomBytes(16).toString("hex")}`;
  fs.appendFileSync(process.env.GITHUB_OUTPUT, `${key}<<${delim}\n${value}\n${delim}\n`);
}

function writeIncomplete() {
  appendLine("requires_translation", "false");
  appendLine("detected_language", "unknown");
  appendLine("source_complete", "false");
}

function main() {
  if (!process.env.GITHUB_OUTPUT) {
    console.error("GITHUB_OUTPUT path is missing");
    process.exit(1);
  }
  const parsed = parseModelJson(process.env.AI_RESPONSE);
  if (!parsed) {
    console.warn("::warning::Model output was empty or not a JSON object.");
    writeIncomplete();
    return;
  }
  if (parsed.requires_translation === false) {
    appendLine("requires_translation", "false");
    appendLine("detected_language", sanitizeLanguageTag(parsed.detected_language || "English"));
    appendLine("source_complete", "true");
    return;
  }
  if (parsed.requires_translation !== true) {
    console.warn("::warning::Model output had a non-boolean requires_translation field.");
    writeIncomplete();
    return;
  }
  appendLine("requires_translation", "true");
  appendLine("detected_language", sanitizeLanguageTag(parsed.detected_language || "non-English"));
  appendLine("translated_title", String(parsed.translated_title || "").replace(/\s+/g, " ").trim().slice(0, 256));
  appendHeredoc("translated_body", String(parsed.translated_body || ""));
  appendLine("source_complete", "false");
}

module.exports = { main, parseAiResponse: parseModelJson };

if (require.main === module) {
  main();
}
