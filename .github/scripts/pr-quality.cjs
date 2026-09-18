"use strict";

const {
  READINESS_OPEN,
  READINESS_CLOSE,
  READINESS_HEADING,
  READINESS_BOXES,
  CLAIM_BOX,
} = require("./pr-quality-frozen.cjs");
const { scoreBody, literalEscapes, dropTemplateChrome } = require("./pr-quality-description.cjs");

const INTEGRATION_BASES = ["dev"];
const DEFAULT_INTEGRATION = "dev";
const FAR_BEHIND_BASE = 20;
const LONG_LIVED_AHEAD_OF_MAIN = 5;
const PUSH_LEVELS = new Set(["admin", "maintain", "write"]);
const GUI_WORD = /\bgui\b/i;
const GUI_LOCKFILE = new Set(["gui/package-lock.json"]);
const FENCE =
  /(?:^|\n)[ \t]*(`{3,}|~{3,})[^\n]*\n[\s\S]*?^[ \t]*\1[ \t]*(?=\n|$)/gm;
const HTML_COMMENT = /<!--[\s\S]*?(?:-->|$)/g;
const MD_IMAGE = /!\[[^\]]*\]\([^)]+\)/;
const REF_IMAGE = /!\[([^\]]*)\]\[([^\]]*)\]/g;
const REF_DEF = /^\s*\[([^\]]+)\]:\s*\S+/gm;
const HTML_IMAGE = /<img\b[^>]*\bsrc\s*=\s*(?:"[^"]+"|'[^']+'|[^\s>"']+)[^>]*>/i;
const MAINTAINER_ASSOC = new Set(["OWNER", "COLLABORATOR", "MEMBER"]);
const GUI_NEGATION =
  /\b(?:no|not|doesn'?t|does not|never|without)\b[^.!?\n]{0,40}?\bgui\b/i;
const ABSENT_READINESS = Object.freeze({
  present: false,
  complete: false,
  checked: 0,
  total: 0,
  items: [],
});

function hasRepoPush(level) {
  return PUSH_LEVELS.has(level);
}

function trustedPolicyRef(baseRef) {
  return baseRef === "main" ? "main" : DEFAULT_INTEGRATION;
}

function sitsOnReleasedMainTip({
  behindMain,
  behindBase,
  aheadMain = 0,
  farBehind = FAR_BEHIND_BASE,
  aheadCap = LONG_LIVED_AHEAD_OF_MAIN,
}) {
  return behindMain === 0 && behindBase >= farBehind && aheadMain <= aheadCap;
}

function branchProblems({
  baseRef,
  allowedBases = INTEGRATION_BASES,
  stackedOnParent = false,
  behindMain,
  behindBase,
  aheadMain,
  authorPermission,
  permissionLookupFailed = false,
  ancestryLookupFailed = false,
}) {
  const problems = [];
  if (!allowedBases.includes(baseRef) && !stackedOnParent) {
    problems.push({ code: "wrong_base" });
    return problems;
  }
  const skipAncestry =
    stackedOnParent ||
    ancestryLookupFailed ||
    (!permissionLookupFailed && hasRepoPush(authorPermission));
  if (!skipAncestry && sitsOnReleasedMainTip({ behindMain, behindBase, aheadMain })) {
    problems.push({ code: "wrong_ancestry" });
  }
  return problems;
}

function isGuiPath(file) {
  return file === "gui" || file.startsWith("gui/");
}

function guiPathTouched(files) {
  return files.some(isGuiPath);
}

function visualGuiTouched(files) {
  return files.some((file) => isGuiPath(file) && !GUI_LOCKFILE.has(file));
}

function incompleteFileList(changedFilesCount, listedLength, headMatches = true) {
  if (!headMatches) return true;
  if (!Number.isInteger(changedFilesCount) || changedFilesCount < 0) return true;
  return changedFilesCount > listedLength;
}

function segmentMentionsGui(text) {
  if (typeof text !== "string" || !text.trim()) return false;
  return text.split(/(?<=[.!?\n])/).some((segment) => {
    if (!GUI_WORD.test(segment)) return false;
    return GUI_WORD.test(segment.replace(GUI_NEGATION, ""));
  });
}

function mentionsGui(title, body) {
  return segmentMentionsGui(title) || segmentMentionsGui(body);
}

function maintainerNegatedGui({ comments = [] }) {
  return comments.some(
    (comment) =>
      MAINTAINER_ASSOC.has(comment?.author_association) &&
      typeof comment?.body === "string" &&
      GUI_NEGATION.test(comment.body),
  );
}

function dropUnrendered(body) {
  return body.replace(FENCE, "").replace(HTML_COMMENT, "");
}

function definedRefImage(visible) {
  const ids = new Set();
  for (const match of visible.matchAll(REF_DEF)) {
    ids.add(match[1].trim().toLowerCase());
  }
  if (ids.size === 0) return false;
  for (const match of visible.matchAll(REF_IMAGE)) {
    const id = (match[2] || match[1]).trim().toLowerCase();
    if (id && ids.has(id)) return true;
  }
  return false;
}

function hasRenderableImage(body) {
  if (typeof body !== "string") return false;
  const visible = dropUnrendered(body);
  if (MD_IMAGE.test(visible)) return true;
  if (HTML_IMAGE.test(visible)) return true;
  return definedRefImage(visible);
}

function screenshotProblems({
  body,
  changedFilePaths = [],
  filesTruncated = false,
  guiOverrideComments = [],
}) {
  const visualUnknown = visualGuiTouched(changedFilePaths) || filesTruncated;
  if (!visualUnknown) return [];
  if (hasRenderableImage(body)) return [];
  if (maintainerNegatedGui({ comments: guiOverrideComments })) return [];
  return [{ code: "missing_ui_screenshot" }];
}

function countMarker(body, marker) {
  return String(body).split(marker).length - 1;
}

function locateReadiness(body) {
  if (typeof body !== "string") return { kind: "missing" };
  const openAt = body.indexOf(READINESS_OPEN);
  const closeAt = body.indexOf(READINESS_CLOSE);
  if (openAt === -1 && closeAt === -1) return { kind: "missing" };
  const unique =
    openAt !== -1 &&
    closeAt !== -1 &&
    closeAt > openAt &&
    countMarker(body, READINESS_OPEN) === 1 &&
    countMarker(body, READINESS_CLOSE) === 1;
  if (!unique) return { kind: "broken" };
  return {
    kind: "ok",
    openAt,
    closeAt,
    innerFrom: openAt + READINESS_OPEN.length,
    innerTo: closeAt,
  };
}

function parseTicks(section) {
  const matches = [...section.matchAll(/^\s*[-*]\s+\[([ xX])\]\s+/gm)];
  const items = matches.map((match) => ({ checked: match[1] !== " " }));
  const checked = items.reduce((n, item) => n + (item.checked ? 1 : 0), 0);
  return { items, checked, total: items.length };
}

function incompletePresent() {
  return { present: true, complete: false, checked: 0, total: 0, items: [] };
}

function canonicalReadinessBlock() {
  const boxes = READINESS_BOXES.map((label) => `- [ ] ${label}`);
  const closing = boxes.pop();
  return [
    READINESS_OPEN,
    READINESS_HEADING,
    "",
    "This PR stays in draft until every box below is ticked. Tick all four boxes once the requirements are met:",
    "",
    ...boxes,
    "",
    closing,
    READINESS_CLOSE,
  ].join("\n");
}

function inspectReadiness(body) {
  const span = locateReadiness(body);
  if (span.kind === "missing") return { ...ABSENT_READINESS };
  if (span.kind !== "ok") return incompletePresent();
  const parsed = parseTicks(body.slice(span.innerFrom, span.innerTo));
  return {
    present: true,
    complete: parsed.total === READINESS_BOXES.length && parsed.checked === parsed.total,
    checked: parsed.checked,
    total: parsed.total,
    items: parsed.items,
  };
}

function ensureReadiness(body) {
  if (inspectReadiness(body).present) return body;
  const block = canonicalReadinessBlock();
  if (typeof body !== "string" || !body.trim()) return `${block}\n`;
  return `${body.trimEnd()}\n\n${block}\n`;
}

function dropReadiness(body) {
  const span = locateReadiness(body);
  if (span.kind !== "ok") return body;
  const stripped =
    body.slice(0, span.openAt) + body.slice(span.closeAt + READINESS_CLOSE.length);
  return stripped.replace(/\n{3,}/g, "\n\n").trimEnd();
}

function restoreBlankReadiness(body) {
  const span = locateReadiness(body);
  if (span.kind !== "ok") return body;
  return (
    body.slice(0, span.openAt) +
    canonicalReadinessBlock() +
    body.slice(span.closeAt + READINESS_CLOSE.length)
  );
}

function clearClaimTicks(body, indexes) {
  const span = locateReadiness(body);
  if (span.kind !== "ok") return body;
  const wanted = new Set(indexes);
  let boxIndex = 0;
  const section = body.slice(span.innerFrom, span.innerTo);
  const updated = section.replace(
    /^([ \t]*[-*]\s+)\[([ xX])\](?=\s)/gm,
    (match, lead, mark) => {
      const current = boxIndex;
      boxIndex += 1;
      if (wanted.has(current) && mark !== " ") return `${lead}[ ]`;
      return match;
    },
  );
  if (updated === section) return body;
  return body.slice(0, span.innerFrom) + updated + body.slice(span.innerTo);
}

function gatherQualityProblems({
  baseRef,
  allowedBases,
  body,
  behindMain,
  behindBase,
  aheadMain = 0,
  authorPermission,
  permissionLookupFailed = false,
  ancestryLookupFailed = false,
  stackedOnParent = false,
  guiOverrideComments = [],
  changedFilePaths = [],
  filesTruncated = false,
}) {
  const problems = [
    ...branchProblems({
      baseRef,
      allowedBases,
      stackedOnParent,
      behindMain,
      behindBase,
      aheadMain,
      authorPermission,
      permissionLookupFailed,
      ancestryLookupFailed,
    }),
  ];
  const description = scoreBody(body);
  if (!description.ok) {
    problems.push({ code: "bad_description", reason: description.reason });
  }
  problems.push(
    ...screenshotProblems({
      body,
      changedFilePaths,
      filesTruncated,
      guiOverrideComments,
    }),
  );
  return problems;
}

module.exports = {
  INTEGRATION_BASES,
  DEFAULT_INTEGRATION,
  FAR_BEHIND_BASE,
  LONG_LIVED_AHEAD_OF_MAIN,
  READINESS_BOXES,
  READINESS_OPEN,
  READINESS_CLOSE,
  CLAIM_BOX,
  sitsOnReleasedMainTip,
  hasRepoPush,
  trustedPolicyRef,
  scoreBody,
  guiPathTouched,
  visualGuiTouched,
  incompleteFileList,
  mentionsGui,
  maintainerNegatedGui,
  hasRenderableImage,
  canonicalReadinessBlock,
  inspectReadiness,
  ensureReadiness,
  dropReadiness,
  restoreBlankReadiness,
  clearClaimTicks,
  gatherQualityProblems,
  literalEscapes,
  dropTemplateChrome,
};
