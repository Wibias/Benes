"use strict";

// Frozen bytes already stored on live PRs / GitHub identities. Do not rename.

const GATE_HTML = "<!-- benes-pr-gate -->";
const GATE_RECORD_NAME = "benes-pr-gate-state";
const LEGACY_ENFORCER_HTML = "<!-- pr-quality-enforcer -->";
const LEGACY_WRONG_BRANCH_HTML = "<!-- wrong-branch-enforcer -->";
const LEGACY_READINESS_HTML = "<!-- pr-quality-readiness -->";
const READINESS_OPEN = "<!-- pr-quality-readiness-checklist:start -->";
const READINESS_CLOSE = "<!-- pr-quality-readiness-checklist:end -->";
const HYGIENE_HTML = "<!-- pr-hygiene -->";
const HYGIENE_OPEN = "<!-- pr-hygiene-block:start -->";
const HYGIENE_CLOSE = "<!-- pr-hygiene-block:end -->";
const WRONG_BASE_PREFIX = "[WRONG BRANCH] ";
const REVIEW_READY = "review-ready";
const GUI_WAIVER_LABEL = "gui-screenshot-waived";
const HYGIENE_BLOCKED_LABEL = "intake: hygiene-blocked";
const SPONSOR_LABEL = "maintainer-sponsored";
const TEST_EXCEPTION_LABEL = "test-exception-approved";
const SUPPRESSION_LABEL = "suppression-approved";
const GENERATED_LABEL = "generated-change-approved";
const DEPENDENCY_LABEL = "dependency-change-approved";
const ACTIONS_BOT = "github-actions[bot]";
const CODEX_BOT = "chatgpt-codex-connector[bot]";
const CODE_RABBIT_BOT = "coderabbitai[bot]";
const CODE_RABBIT_APP_ID = 136622811;
const CODE_RABBIT_STATUS = "CodeRabbit";
const MAINTAINERS_HEADING = "## Current maintainers";
const READINESS_HEADING = "## Review readiness checklist";

const READINESS_BOXES = [
  "All CI tests are green on my local testing.",
  "I pushed my PR to the latest dev commit.",
  "I resolved all correct Codex and CodeRabbit findings.",
  "My PR is ready for review.",
];

const CLAIM_BOX = Object.freeze({
  latest_dev: 1,
  review_findings: 2,
});

const FRESHNESS_BEHIND_MAX = 10;

const HEAD_SCOPED_EXCEPTION_LABELS = [
  TEST_EXCEPTION_LABEL,
  SUPPRESSION_LABEL,
  GENERATED_LABEL,
  DEPENDENCY_LABEL,
];

const QUALITY_WAKE_LABELS = [
  GUI_WAIVER_LABEL,
  HYGIENE_BLOCKED_LABEL,
  SPONSOR_LABEL,
  ...HEAD_SCOPED_EXCEPTION_LABELS,
];

module.exports = {
  GATE_HTML,
  GATE_RECORD_NAME,
  LEGACY_ENFORCER_HTML,
  LEGACY_WRONG_BRANCH_HTML,
  LEGACY_READINESS_HTML,
  READINESS_OPEN,
  READINESS_CLOSE,
  HYGIENE_HTML,
  HYGIENE_OPEN,
  HYGIENE_CLOSE,
  WRONG_BASE_PREFIX,
  REVIEW_READY,
  GUI_WAIVER_LABEL,
  HYGIENE_BLOCKED_LABEL,
  SPONSOR_LABEL,
  TEST_EXCEPTION_LABEL,
  SUPPRESSION_LABEL,
  GENERATED_LABEL,
  DEPENDENCY_LABEL,
  ACTIONS_BOT,
  CODEX_BOT,
  CODE_RABBIT_BOT,
  CODE_RABBIT_APP_ID,
  CODE_RABBIT_STATUS,
  MAINTAINERS_HEADING,
  READINESS_HEADING,
  READINESS_BOXES,
  CLAIM_BOX,
  FRESHNESS_BEHIND_MAX,
  HEAD_SCOPED_EXCEPTION_LABELS,
  QUALITY_WAKE_LABELS,
};
