"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { stripTranslationBlock } = require("./issue-translation.cjs");

const DUPLICATE_CAP = 5;
const RELATED_CAP = 3;
const TITLE_MAX = 240;
const BODY_MAX = 1200;
const CANDIDATE_CARD_CAP = 40;

const NOMINATE_SYSTEM =
  "Return JSON only. Nominate duplicate and related GitHub issues. duplicates is an array of issue numbers. related is an array of {number, reason}. Closure is decided from the issue records, not from this reason text.";

function visibleLines(issue) {
  const raw = [issue?.title, stripTranslationBlock(issue?.body || "")]
    .filter((value) => typeof value === "string" && value.trim())
    .join("\n")
    .replace(/<!--[\s\S]*?(?:-->|$)/g, " ");
  return raw.split(/\r?\n/).map((line) =>
    line
      .replace(/^\s*(?:>|[-*+]\s+|\d+[.)]\s+)/, "")
      .replace(/`/g, "")
      .replace(/\s+/g, " ")
      .trim(),
  ).filter(Boolean);
}

function locatorsOn(line) {
  const found = [];
  const methodPath = /(?:GET|POST|PUT|PATCH|DELETE|HEAD)\s+\/[^\s]+/gi;
  let match;
  while ((match = methodPath.exec(line)) !== null) found.push(match[0].toUpperCase().replace(/\s+/g, " "));
  const listen = /\b\d{1,3}(?:\.\d{1,3}){3}:\d+\b/g;
  while ((match = listen.exec(line)) !== null) found.push(match[0]);
  const file = /\b[\w.-]+\.(?:go|ts|tsx|js|cjs|mjs|json|toml|yml|yaml)\b/gi;
  while ((match = file.exec(line)) !== null) found.push(match[0].toLowerCase());
  return [...new Set(found)];
}

function outcomesOn(line) {
  const found = [];
  const status = /\b(?:HTTP(?:\s+status)?(?:\s+code)?|status(?:\s+code)?|returns?|returned|returning|got|code)\s+[:=]?\s*([45]\d\d)\b/gi;
  let match;
  while ((match = status.exec(line)) !== null) found.push(`HTTP ${match[1]}`);
  const sys = /\b(ECONNRESET|ECONNREFUSED|ETIMEDOUT|ENOTFOUND|EPIPE|EAI_AGAIN|ECONNABORTED|EHOSTUNREACH|ENETUNREACH|EADDRINUSE)\b/g;
  while ((match = sys.exec(line)) !== null) found.push(match[1]);
  return [...new Set(found)];
}

function evidenceFromIssue(issue) {
  const closeRecords = new Set();
  const relatedPairs = new Set();
  for (const line of visibleLines(issue)) {
    const locators = locatorsOn(line);
    const outcomes = outcomesOn(line);
    if (locators.length === 0 || outcomes.length === 0) continue;
    if (line.length >= 36 && line.length <= 240) closeRecords.add(line);
    for (const locator of locators) {
      for (const outcome of outcomes) relatedPairs.add(`${locator} :: ${outcome}`);
    }
  }
  return { closeRecords, relatedPairs };
}

function sharedCloseRecord(currentIssue, candidateIssue) {
  const current = evidenceFromIssue(currentIssue).closeRecords;
  if (current.size === 0) return null;
  const shared = [];
  for (const record of evidenceFromIssue(candidateIssue).closeRecords) {
    if (current.has(record)) shared.push(record);
  }
  shared.sort((left, right) => right.length - left.length || (left < right ? -1 : left > right ? 1 : 0));
  return shared[0] || null;
}

function sharedRelatedPair(currentIssue, candidateIssue) {
  const current = evidenceFromIssue(currentIssue).relatedPairs;
  if (current.size === 0) return null;
  for (const pair of evidenceFromIssue(candidateIssue).relatedPairs) {
    if (current.has(pair)) return pair;
  }
  return null;
}

function scrubReason(raw) {
  return String(raw || "")
    .replace(/[\u0000-\u001f\u007f]/g, " ")
    .replace(/@/g, "(at)")
    .replace(/[`*_~<>[\]()#|]/g, "")
    .replace(/\s+/g, " ")
    .trim()
    .slice(0, 240);
}

function admitIssueNumber(entry, { currentNumber, knownNumbers }) {
  const match = String(entry ?? "").trim().match(/^#?(\d+)$/);
  if (!match) return "";
  const number = match[1];
  const known = knownNumbers instanceof Set ? knownNumbers : new Set((knownNumbers || []).map(String));
  if (number === String(currentNumber) || !known.has(number)) return "";
  return number;
}

function parseModelJson(raw) {
  const text = String(raw || "").trim();
  if (!text) return null;
  try {
    return JSON.parse(text);
  } catch {
    const unfenced = text.replace(/^```(?:json)?\s*/i, "").replace(/\s*```\s*$/, "").trim();
    try {
      return JSON.parse(unfenced);
    } catch {
      return null;
    }
  }
}

function readNominationNumbers(raw, ctx) {
  const parsed = parseModelJson(raw);
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return null;

  const duplicates = [];
  const seenDup = new Set();
  for (const entry of Array.isArray(parsed.duplicates) ? parsed.duplicates : []) {
    if (duplicates.length >= DUPLICATE_CAP) break;
    const number = admitIssueNumber(entry, ctx);
    if (!number || seenDup.has(number)) continue;
    seenDup.add(number);
    duplicates.push(number);
  }

  const related = [];
  const seenRel = new Set();
  for (const entry of Array.isArray(parsed.related) ? parsed.related : []) {
    if (related.length >= RELATED_CAP) break;
    if (!entry || typeof entry !== "object") continue;
    const number = admitIssueNumber(entry.number ?? entry.issue ?? entry.id, ctx);
    if (!number || seenRel.has(number) || seenDup.has(number)) continue;
    seenRel.add(number);
    related.push({ number, caption: scrubReason(entry.reason ?? entry.why ?? "") });
  }

  if (duplicates.length === 0 && related.length === 0) return null;
  return { duplicates, related };
}

function decideDuplicateClose({ currentIssue, candidateIssues, nominatedIds }) {
  const allowed = new Set((Array.isArray(nominatedIds) ? nominatedIds : []).map(String));
  if (allowed.size === 0) return null;
  const currentNumber = String(currentIssue?.number ?? "");
  const byNumber = new Map(
    (Array.isArray(candidateIssues) ? candidateIssues : [])
      .map((issue) => [String(issue?.number ?? ""), issue])
      .filter(([number]) => /^\d+$/.test(number)),
  );

  const matches = [];
  for (const number of allowed) {
    if (number === currentNumber) continue;
    const candidate = byNumber.get(number);
    if (!candidate) continue;
    const signature = sharedCloseRecord(currentIssue, candidate);
    if (!signature) continue;
    matches.push({ number, signature });
  }
  matches.sort((left, right) =>
    right.signature.length - left.signature.length
    || (left.signature < right.signature ? -1 : left.signature > right.signature ? 1 : 0)
    || Number(left.number) - Number(right.number),
  );
  return matches[0] || null;
}

function decideRelatedReports({ currentIssue, candidateIssues, nominatedRelated }) {
  const currentNumber = String(currentIssue?.number ?? "");
  const byNumber = new Map(
    (Array.isArray(candidateIssues) ? candidateIssues : [])
      .map((issue) => [String(issue?.number ?? ""), issue])
      .filter(([number]) => /^\d+$/.test(number)),
  );
  const rows = [];
  for (const entry of Array.isArray(nominatedRelated) ? nominatedRelated : []) {
    const number = String(entry?.number ?? "");
    if (!number || number === currentNumber) continue;
    const candidate = byNumber.get(number);
    if (!candidate) continue;
    const pair = sharedRelatedPair(currentIssue, candidate);
    if (!pair) continue;
    rows.push({ number, reason: pair });
  }
  return rows;
}

function clipText(text, max) {
  const value = String(text || "").trim();
  if (value.length <= max) return value;
  return `${value.slice(0, max)}\n…`;
}

function toIssueCard(issue) {
  return {
    number: String(issue.number),
    title: clipText(issue.title, TITLE_MAX),
    body: clipText(stripTranslationBlock(issue.body || ""), BODY_MAX),
    state: issue.state,
  };
}

function readIssueArray(cwd, filename) {
  const file = path.join(cwd, filename);
  if (!fs.existsSync(file)) return [];
  try {
    const parsed = JSON.parse(fs.readFileSync(file, "utf8"));
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

function loadKnownIssues(cwd = process.cwd()) {
  return [...readIssueArray(cwd, "open.json"), ...readIssueArray(cwd, "closed.json")];
}

function defaultNominate(args) {
  return require("./issue-ai.cjs").completeJson(args);
}

function issueNumberFrom(context) {
  if (context.eventName === "workflow_dispatch") {
    return Number(context.payload?.inputs?.issue_number);
  }
  return Number(context.payload?.issue?.number);
}

async function runIssueTriage({
  github,
  context,
  core,
  state_reason = "duplicate",
  completeJson,
  cwd = process.cwd(),
  knownIssues,
}) {
  const { owner, repo } = context.repo;
  const issue_number = issueNumberFrom(context);
  if (!Number.isSafeInteger(issue_number) || issue_number <= 0) {
    core.setFailed(`Invalid issue number: ${issue_number}`);
    return;
  }

  const { data: issue } = await github.rest.issues.get({ owner, repo, issue_number });
  if (issue.pull_request) {
    core.info(`#${issue_number} is a pull request; skipping triage.`);
    return;
  }

  const catalog = Array.isArray(knownIssues) ? knownIssues : loadKnownIssues(cwd);
  const candidates = catalog.filter((candidate) => Number(candidate.number) !== issue_number);
  const knownNumbers = candidates.map((candidate) => String(candidate.number));
  const token = process.env.BENES_ISSUE_AI_TOKEN || "";
  if (!token) {
    core.info("BENES_ISSUE_AI_TOKEN is not set; skipping AI duplicate nomination.");
    return;
  }

  const nominate = completeJson || defaultNominate;
  const completion = await nominate({
    token,
    baseUrl: process.env.BENES_ISSUE_AI_BASE_URL,
    model: process.env.BENES_ISSUE_AI_MODEL,
    system: NOMINATE_SYSTEM,
    user: JSON.stringify({
      current: toIssueCard(issue),
      candidates: candidates.slice(0, CANDIDATE_CARD_CAP).map(toIssueCard),
    }),
  });
  if (!completion.ok) {
    core.info(`Issue triage model skipped: ${completion.reason}`);
    return;
  }

  const nomination = readNominationNumbers(
    completion.text,
    { currentNumber: String(issue_number), knownNumbers },
  );
  if (!nomination) {
    core.info("No usable nomination numbers.");
    return;
  }

  const duplicate = decideDuplicateClose({
    currentIssue: issue,
    candidateIssues: candidates,
    nominatedIds: nomination.duplicates,
  });
  if (duplicate) {
    await github.rest.issues.createComment({
      owner,
      repo,
      issue_number,
      body: `Duplicate of #${duplicate.number}.\n\nShared failure: \`${duplicate.signature}\``,
    });
    await github.rest.issues.update({
      owner,
      repo,
      issue_number,
      state: "closed",
      state_reason,
    });
    return;
  }

  const related = decideRelatedReports({
    currentIssue: issue,
    candidateIssues: candidates,
    nominatedRelated: nomination.related,
  });
  if (related.length) {
    await github.rest.issues.createComment({
      owner,
      repo,
      issue_number,
      body: [
        "Related reports (issue stays open):",
        "",
        ...related.map((entry) => `- #${entry.number}: ${entry.reason}`),
      ].join("\n"),
    });
  }
}

module.exports = {
  evidenceFromIssue,
  sharedCloseRecord,
  sharedRelatedPair,
  decideDuplicateClose,
  decideRelatedReports,
  readNominationNumbers,
  scrubReason,
  admitIssueNumber,
  parseModelJson,
  toIssueCard,
  loadKnownIssues,
  runIssueTriage,
  chooseCloseTarget: decideDuplicateClose,
  admitNomination: readNominationNumbers,
};
