"use strict";

const ACTIONS_BOT = "github-actions[bot]";

const MANAGED_TYPE_LABELS = new Set(["bug", "enhancement", "documentation", "chore"]);

const PRODUCT_INTENTS = Object.freeze({
  feat: "enhancement",
  feature: "enhancement",
  fix: "bug",
  bugfix: "bug",
  hotfix: "bug",
  docs: "documentation",
  doc: "documentation",
});

const SUPPORTING_INTENTS = Object.freeze({
  test: "chore",
  tests: "chore",
  ci: "chore",
  build: "chore",
  refactor: "chore",
  chore: "chore",
});

function conventionalType(subject) {
  const text = String(subject || "").trim();
  const colon = text.indexOf(":");
  if (colon <= 0) return null;
  if (text.slice(colon + 1).trim() === "") return null;
  let left = text.slice(0, colon).trim();
  if (left.endsWith("!")) left = left.slice(0, -1).trim();
  const scope = left.indexOf("(");
  if (scope >= 0) {
    if (!left.endsWith(")")) return null;
    left = left.slice(0, scope).trim();
  }
  if (!/^[A-Za-z]+$/.test(left)) return null;
  return left.toLowerCase();
}

function intentOf(subject) {
  const type = conventionalType(subject);
  if (!type) return null;
  if (Object.prototype.hasOwnProperty.call(PRODUCT_INTENTS, type)) {
    return { kind: "product", label: PRODUCT_INTENTS[type] };
  }
  if (Object.prototype.hasOwnProperty.call(SUPPORTING_INTENTS, type)) {
    return { kind: "supporting", label: "chore" };
  }
  return null;
}

function classifyTitle(title) {
  const intent = intentOf(title);
  return intent ? intent.label : null;
}

function classifyCommitMessages(messages) {
  if (!Array.isArray(messages)) return null;
  const product = new Set();
  let supporting = false;
  for (const message of messages) {
    const subject = String(message || "").split("\n", 1)[0];
    const intent = intentOf(subject);
    if (!intent) continue;
    if (intent.kind === "product") product.add(intent.label);
    else supporting = true;
  }
  if (product.size === 1) return [...product][0];
  if (product.size > 1) return null;
  if (supporting) return "chore";
  return null;
}

function humanLockedTypeLabels(events) {
  if (!Array.isArray(events)) return false;
  for (const event of events) {
    if (event?.event !== "labeled" && event?.event !== "unlabeled") continue;
    const name = event.label?.name;
    if (!name || !MANAGED_TYPE_LABELS.has(name)) continue;
    const actor = event.actor?.login;
    if (actor && actor !== ACTIONS_BOT) return true;
  }
  return false;
}

function planManagedTypeLabels(input) {
  const currentLabels = Array.isArray(input?.currentLabels) ? input.currentLabels : [];
  const events = Array.isArray(input?.events) ? input.events : [];

  if (humanLockedTypeLabels(events)) {
    return { apply: false, reason: "human-override" };
  }

  const type = classifyTitle(input?.title) ?? classifyCommitMessages(input?.commitMessages);
  if (!type) {
    return { apply: false, reason: "unclassified" };
  }

  const present = new Set(currentLabels);
  const remove = [...MANAGED_TYPE_LABELS].filter((label) => present.has(label) && label !== type);
  const add = present.has(type) ? null : type;
  return { apply: true, type, add, remove };
}

module.exports = {
  MANAGED_TYPE_LABELS,
  PRODUCT_INTENTS,
  SUPPORTING_INTENTS,
  classifyTitle,
  classifyCommitMessages,
  humanLockedTypeLabels,
  planManagedTypeLabels,
};
