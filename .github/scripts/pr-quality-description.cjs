"use strict";

const { READINESS_OPEN, READINESS_CLOSE } = require("./pr-quality-frozen.cjs");

function dropReadiness(body) {
  if (typeof body !== "string") return body;
  const openAt = body.indexOf(READINESS_OPEN);
  const closeAt = body.indexOf(READINESS_CLOSE);
  if (openAt === -1 || closeAt === -1 || closeAt <= openAt) return body;
  if (body.split(READINESS_OPEN).length !== 2 || body.split(READINESS_CLOSE).length !== 2) {
    return body;
  }
  return (body.slice(0, openAt) + body.slice(closeAt + READINESS_CLOSE.length))
    .replace(/\n{3,}/g, "\n\n")
    .trimEnd();
}

const RICH_SECTION_CHARS = 40;
const RICH_SECTION_COUNT = 2;
const LOOSE_BODY_CHARS = 120;
const LOOSE_BODY_BLOCKS = 2;

const TEMPLATE_CHROME = new Set([
  "what changed for a benes user or maintainer (go cli, loopback listener on 127.0.0.1:23100, dashboard, or docs).",
  "if this touches combos, say the strategy is still failover-only unless the projector actually gained a new one.",
  "paste commands and their output. default set: `go test ./...`, `go vet ./...`, `npm run privacy:scan`.",
  "gui: `cd gui && npm run lint && npm run build` (and `npm run lint:i18n` after copy). attach a screenshot unless a maintainer applied `gui-screenshot-waived`.",
  "docs: `cd docs && npm run build`.",
  "hosted actions on this org may not start because of billing. local logs are the evidence. \"ci is green\" with no log is not a plan.",
  "targets `dev` (not `main`).",
  "diff stays on the stated problem.",
  "user-facing behaviour has a matching `docs/` or readme update.",
  "auth, secrets, workflows, and `scripts/release.ts` had a second look.",
  "did not rename `opencode.svg` or treat opencode as this project.",
]);

const TEMPLATE_HEADING = /^(summary|verification|checklist)$/;

function literalEscapes(text) {
  const escaped = (text.match(/\\n/g) || []).length;
  if (escaped < 2) return false;
  const real = (text.match(/\n/g) || []).length;
  return escaped > real;
}

function foldChrome(line) {
  return line
    .replace(/^\s*[-*+]\s+/, "")
    .replace(/^\s*\[[ xX]\]\s+/, "")
    .replace(/^\s*#{1,6}\s+/, "")
    .replace(/[“”]/g, '"')
    .replace(/[‘’]/g, "'")
    .trim()
    .toLowerCase();
}

function dropTemplateChrome(text) {
  return String(text || "")
    .split("\n")
    .filter((line) => {
      const folded = foldChrome(line);
      if (!folded) return true;
      if (TEMPLATE_CHROME.has(folded)) return false;
      return !TEMPLATE_HEADING.test(folded);
    })
    .join("\n");
}

function afterComments(text) {
  return String(text || "").replace(/<!--[\s\S]*?(?:-->|$)/g, "").trim();
}

function isPlaceholder(text) {
  return /^(?:n\/?a|none|todo|tbd|no response)$/i.test(text.trim());
}

function hasRichSections(text) {
  const sections = [];
  let current = [];
  for (const line of String(text || "").split("\n")) {
    if (/^#{2,3}\s+\S/.test(line)) {
      if (current.length) sections.push(current.join("\n").trim());
      current = [];
      continue;
    }
    current.push(line);
  }
  if (current.length) sections.push(current.join("\n").trim());
  return sections.filter((section) => section.replace(/\s+/g, "").length >= RICH_SECTION_CHARS).length >= RICH_SECTION_COUNT;
}

function blockCount(text) {
  const paragraphs = text.split(/\n\s*\n/).map((block) => block.trim()).filter(Boolean);
  if (paragraphs.length >= LOOSE_BODY_BLOCKS) return paragraphs.length;
  const bullets = text.split("\n").map((line) => line.trim()).filter((line) => /^[-*+]\s+\S/.test(line));
  return Math.max(paragraphs.length, bullets.length);
}

function scoreBody(body) {
  const authorBody = dropReadiness(body);
  if (typeof authorBody !== "string" || !authorBody.trim()) return { ok: false, reason: "empty" };
  if (literalEscapes(authorBody)) return { ok: false, reason: "escaped_newlines" };
  const withoutChrome = dropTemplateChrome(authorBody);
  const cleaned = afterComments(withoutChrome);
  if (!cleaned) return { ok: false, reason: "empty" };
  if (isPlaceholder(cleaned)) return { ok: false, reason: "placeholder" };
  if (hasRichSections(cleaned) || (cleaned.length >= LOOSE_BODY_CHARS && blockCount(cleaned) >= LOOSE_BODY_BLOCKS)) {
    return { ok: true };
  }
  return { ok: false, reason: "thin" };
}

module.exports = {
  RICH_SECTION_CHARS,
  RICH_SECTION_COUNT,
  literalEscapes,
  dropTemplateChrome,
  afterComments,
  scoreBody,
};
