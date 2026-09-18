"use strict";

const catalog = require("./issue-intake-catalog.cjs");
const parse = require("./issue-intake-parse.cjs");

const AREA_LABELS = catalog.AREA_LABEL_META;

function mapAreaFieldToLabels(value) {
  const key = String(value || "").trim().toLowerCase();
  if (!key) return [];
  if (Object.prototype.hasOwnProperty.call(catalog.AREA_DROPDOWN_TO_LABEL, key)) {
    return catalog.AREA_DROPDOWN_TO_LABEL[key];
  }
  return [];
}

function issueLabelNames(issue) {
  return (issue?.labels || []).map((label) => (typeof label === "string" ? label : label?.name)).filter(Boolean);
}

function translationPlainText(block) {
  return parse.asText(block).replace(/<[^>]+>/g, " ");
}

function narrativeForHeuristics(body) {
  return parse.asText(body);
}

function heuristicAreaLabels({ title = "", body = "", translationText = "" }) {
  const text = `${title}\n${body}\n${translationText}`;
  const found = [];
  const add = (label) => {
    if (!found.includes(label)) found.push(label);
  };
  const areaField = parse.resolveSection(body, catalog.AREA_FIELD_HEADINGS);
  for (const label of mapAreaFieldToLabels(areaField)) add(label);
  if (/\b(oauth|account pool|quota window|chatgpt login)\b/i.test(text)) add("account-pool");
  if (/\b(model catalog|\/v1\/models|exposed models)\b/i.test(text)) add("catalog");
  if (/\b(dashboard|\bgui\b)\b/i.test(title)) add("gui");
  if (/\b(loopback|:23100|management api)\b/i.test(text)) add("proxy");
  if (/\b(sse|event-stream)\b/i.test(text)) add("streaming");
  if (/\b(tool_calls?|\bmcp\b)\b/i.test(text)) add("tools");
  if (/\b(npm pack|gui\/dist|npx benes)\b/i.test(text)) add("install");
  return found.filter((label) => AREA_LABELS[label]);
}

function detectAreaLabels(input) {
  return heuristicAreaLabels(input);
}

module.exports = {
  AREA_LABELS,
  mapAreaFieldToLabels,
  issueLabelNames,
  translationPlainText,
  narrativeForHeuristics,
  heuristicAreaLabels,
  detectAreaLabels,
};
