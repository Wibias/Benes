"use strict";

const { GATE_RECORD_NAME } = require("./pr-quality-frozen.cjs");

const GATE_RECORD = /<!-- benes-pr-gate-state:([\s\S]*?) -->/;
const LEGACY_ENFORCER =
  /<!-- (?:wrong-branch-enforcer|pr-quality-enforcer)-state:([\s\S]*?) -->/;
const LEGACY_READINESS = /<!-- pr-quality-readiness-state:([\s\S]*?) -->/;

function decodeJsonMarker(body, pattern, warn, label) {
  const match = body?.match(pattern);
  if (!match) return null;
  try {
    return JSON.parse(match[1]);
  } catch (error) {
    warn(`Could not parse stored ${label}: ${error.message}`);
    return null;
  }
}

function encodeGateRecord(state) {
  return `<!-- ${GATE_RECORD_NAME}:${JSON.stringify(state)} -->`;
}

function decodeGateRecord(body, warn = () => {}) {
  return decodeJsonMarker(body, GATE_RECORD, warn, "gate state");
}

function decodeEnforcerRecord(body, warn = () => {}) {
  return decodeJsonMarker(body, LEGACY_ENFORCER, warn, "workflow state");
}

function decodeReadinessRecord(body, warn = () => {}) {
  return decodeJsonMarker(body, LEGACY_READINESS, warn, "readiness state");
}

function blankGateRecord() {
  return {
    version: 1,
    active: false,
    autoDraftedByBot: false,
    titlePrefixedByBot: false,
    maintainersPinged: false,
    completedAtHeadSha: null,
    reviewReadyLabeled: false,
  };
}

function blendLegacyRecords(enforcerState, readinessState) {
  const gate = blankGateRecord();
  if (enforcerState) {
    gate.active = Boolean(enforcerState.active);
    gate.autoDraftedByBot = Boolean(enforcerState.autoDraftedByBot);
    gate.titlePrefixedByBot = Boolean(enforcerState.titlePrefixedByBot);
  }
  if (readinessState) {
    gate.autoDraftedByBot = gate.autoDraftedByBot || Boolean(readinessState.autoDraftedByBot);
    gate.maintainersPinged = Boolean(readinessState.maintainersPinged);
    gate.completedAtHeadSha = readinessState.completedAtHeadSha ?? null;
  }
  return gate;
}

function completionDrifted({
  checklistRequired,
  checklistComplete,
  readinessPresent,
  completionHeadSha,
  eventHeadSha,
  liveHeadSha,
  eventAction,
}) {
  if (!checklistRequired || !readinessPresent) return false;
  if (completionHeadSha !== null) return completionHeadSha !== liveHeadSha;
  if (!checklistComplete) return false;
  if (eventHeadSha !== liveHeadSha) return true;
  return eventAction === "synchronize";
}

module.exports = {
  encodeGateRecord,
  decodeGateRecord,
  decodeEnforcerRecord,
  decodeReadinessRecord,
  blankGateRecord,
  blendLegacyRecords,
  completionDrifted,
};
