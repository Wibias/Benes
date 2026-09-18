"use strict";

const catalog = require("./issue-intake-catalog.cjs");
const parse = require("./issue-intake-parse.cjs");
const evaluate = require("./issue-intake-evaluate.cjs");
const areas = require("./issue-intake-areas.cjs");
const session = require("./issue-intake-session.cjs");
const bot = require("./issue-intake-bot.cjs");

function detectIssueKind(issue) {
  return evaluate.recognizeKind(issue);
}

function validateIssue(issue) {
  return evaluate.evaluateIssue(issue);
}

module.exports = {
  detectIssueKind,
  validateIssue,
  mapAreaFieldToLabels: areas.mapAreaFieldToLabels,
  labelForKind: evaluate.labelForKind,
  KIND_TO_LABEL: catalog.KIND_TO_LABEL,
  AREA_LABELS: catalog.AREA_LABEL_META,
  AREA_FIELD_TO_LABELS: catalog.AREA_DROPDOWN_TO_LABEL,
  looksLikeUntemplatedBugReport: evaluate.looksLikeUntemplatedBugReport,
  shouldReopen: (botState, issue, maintainerReopen) => {
    if (maintainerReopen) return false;
    const record = botState?.active
      ? { kind: "bot-close", state: botState }
      : { kind: "absent", state: botState };
    return bot.botOwnsClose(record, issue);
  },
  shouldEnforceClosure: (botState) => !botState?.maintainerOverride,
  rejectsWorkflowDispatchPullRequest: (issue, issueNumber, eventName) =>
    session.inspectDispatchIssue(issue, issueNumber, eventName).detail,
  rejectsWorkflowDispatchNonDefaultBranch: (eventName, ref, defaultBranch) =>
    session.inspectDispatchTrust(eventName, ref, defaultBranch).detail,
  detectAreaLabels: areas.detectAreaLabels,
  heuristicAreaLabels: areas.heuristicAreaLabels,
  bodyForAreaHeuristics: areas.narrativeForHeuristics,
  isPlaceholder: parse.isPlaceholder,
  isPlaceholderOnlyValue: parse.isPlaceholderOnlyValue,
  isRawPlaceholder: parse.isRawPlaceholder,
  isUnusableVersion: parse.isUnusableVersion,
  countWords: parse.countWords,
  hasConcreteDetail: parse.hasConcreteDetail,
  extractSection: (body, heading) => parse.resolveSection(body, [heading]),
  resolveSection: parse.resolveSection,
  canonicalise: parse.canonicalise,
};
