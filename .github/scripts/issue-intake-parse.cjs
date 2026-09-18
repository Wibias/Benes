"use strict";

const PLACEHOLDER =
  /^(?:no\s+response|n\/?a|not\s+applicable|not\s+available|none|todo|tbd)$/i;
// Live Bug/Provider forms ask for a version or Host OS. "unknown" and "?"
// are the only non-empty answers that mean the reporter declined to say.
const UNUSABLE_ENV = new Set(["unknown", "?"]);
const PRODUCT_CUE =
  /\b(benes|config|api|cli|dashboard|provider|proxy|route|endpoint|workflow|command|combo|listener)\b/i;

function asText(value) {
  return typeof value === "string" ? value : "";
}

function fold(text) {
  return asText(text).replace(/\s+/g, " ").trim().toLowerCase();
}

function isPlaceholder(text) {
  const token = fold(text).replace(/[*_~`]/g, "").replace(/[.!?]+$/, "").trim();
  return token !== "" && PLACEHOLDER.test(token);
}

function isEmpty(text) {
  const value = asText(text).replace(/<!--[\s\S]*?(?:-->|$)/g, "").trim();
  if (!value) return true;
  return isPlaceholder(value);
}

function isUnknownEnv(text) {
  const token = fold(text).replace(/[*_~`]/g, "").replace(/[.!?]+$/, "").trim();
  return UNUSABLE_ENV.has(token);
}

function countWords(text) {
  const cjk = asText(text).match(/[\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Hangul}]/gu) || [];
  const words = asText(text).match(/[\p{L}\p{N}']+/gu) || [];
  return Math.max(words.length, cjk.length);
}

function hasConcreteDetail(text) {
  return PRODUCT_CUE.test(asText(text)) || /[./\\:`]/.test(asText(text)) || /```/.test(asText(text));
}

function fenceOpener(line) {
  const match = /^( {0,3})(`{3,}|~{3,})(.*)$/.exec(line);
  if (!match) return null;
  const marker = match[2][0];
  const length = match[2].length;
  if (marker === "`" && match[3].includes("`")) return null;
  return { marker, length };
}

function fenceCloser(line, opener) {
  const match = /^( {0,3})(`{3,}|~{3,})\s*$/.exec(line);
  if (!match) return false;
  if (match[2][0] !== opener.marker) return false;
  return match[2].length >= opener.length;
}

function headingKey(title) {
  return fold(title).replace(/[:?]+$/, "");
}

function parseSections(body) {
  const lines = asText(body).split(/\r?\n/);
  const sections = [];
  let current = null;
  let fence = null;
  for (const line of lines) {
    if (fence) {
      if (fenceCloser(line, fence)) fence = null;
      if (current) current.body.push(line);
      continue;
    }
    const opener = fenceOpener(line);
    if (opener) {
      fence = opener;
      if (current) current.body.push(line);
      continue;
    }
    const heading = /^(#{2,3})[ \t]+(.+?)\s*$/.exec(line);
    if (heading && heading[1].length === 3) {
      if (current) sections.push(current);
      current = { title: heading[2].trim(), body: [] };
      continue;
    }
    if (current) current.body.push(line);
  }
  if (current) sections.push(current);
  return sections.map((section) => ({
    title: section.title,
    key: headingKey(section.title),
    text: section.body.join("\n").trim(),
  }));
}

function sectionMap(body) {
  const map = new Map();
  for (const section of parseSections(body)) {
    if (!map.has(section.key)) map.set(section.key, section.text);
  }
  return map;
}

function resolveSection(body, headings) {
  const map = sectionMap(body);
  for (const heading of headings || []) {
    const key = headingKey(heading);
    if (map.has(key)) return map.get(key);
  }
  return null;
}

function hasHeading(body, heading) {
  return sectionMap(body).has(headingKey(heading));
}

function countPresentHeadings(body, headings) {
  const map = sectionMap(body);
  let count = 0;
  const seen = new Set();
  for (const heading of headings || []) {
    const key = headingKey(heading);
    if (seen.has(key) || !map.has(key)) continue;
    seen.add(key);
    count += 1;
  }
  return count;
}

function sameCanonical(values) {
  const folds = values.map(fold).filter(Boolean);
  if (folds.length < 2) return false;
  return folds.every((value) => value === folds[0]);
}

function echoesTitle(text, title) {
  const body = fold(text);
  const head = fold(title);
  return body !== "" && head !== "" && (body === head || (body.includes(head) && head.length >= 12));
}

function stripMedia(text) {
  return asText(text)
    .replace(/!\[[^\]]*\]\([^)]+\)/g, " ")
    .replace(/<img\b[^>]*>/gi, " ")
    .replace(/\[([^\]]*)\]\([^)]+\)/g, "$1")
    .trim();
}

function isMediaOnly(text) {
  return !isEmpty(asText(text)) && isEmpty(stripMedia(text));
}

function canonicalise(text) {
  return fold(text);
}

module.exports = {
  asText,
  fold,
  isPlaceholder,
  isEmpty,
  isUnknownEnv,
  countWords,
  hasConcreteDetail,
  parseSections,
  resolveSection,
  hasHeading,
  countPresentHeadings,
  sameCanonical,
  echoesTitle,
  isMediaOnly,
  canonicalise,
  isPlaceholderOnlyValue: isPlaceholder,
  isRawPlaceholder: isPlaceholder,
  isUnusableVersion: isUnknownEnv,
};
