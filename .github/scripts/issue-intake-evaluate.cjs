"use strict";

const catalog = require("./issue-intake-catalog.cjs");
const parse = require("./issue-intake-parse.cjs");

function slot(body, headings) {
  const text = parse.resolveSection(body, headings);
  if (text === null) return { present: false, text: "", empty: true };
  return { present: true, text, empty: parse.isEmpty(text) };
}

function detectFromContent(issue) {
  const title = issue?.title || "";
  const text = issue?.body || "";
  const labels = Array.isArray(issue?.labels) ? issue.labels : [];

  if (parse.countPresentHeadings(text, [...catalog.PROVIDER.current, ...catalog.PROVIDER.aliases]) >= 3) {
    return "provider-compatibility";
  }
  if (parse.countPresentHeadings(text, [...catalog.DOCS.current, ...catalog.DOCS.aliases]) >= 2) {
    return "documentation";
  }
  if (
    parse.countPresentHeadings(text, catalog.FEATURE.current) >= 2 ||
    (parse.countPresentHeadings(text, catalog.FEATURE.detectAliases) >= 2 &&
      parse.countPresentHeadings(text, catalog.FEATURE.goal) >= 1)
  ) {
    return "feature";
  }
  if (
    parse.countPresentHeadings(text, catalog.BUG.current) >= 3 ||
    parse.countPresentHeadings(text, catalog.BUG.aliases) >= 3
  ) {
    return "bug";
  }
  if (String(title).toLowerCase().startsWith(catalog.TITLE_FEATURE_PREFIX)) return "feature";
  if (parse.countPresentHeadings(text, catalog.FEATURE.legacyPair) >= 2) return "feature";
  const bugPrefix = String(title).toLowerCase().startsWith(catalog.TITLE_BUG_PREFIX);
  if (bugPrefix || parse.countPresentHeadings(text, catalog.BUG.legacyPair) >= 2) {
    if (bugPrefix || labels.includes("bug")) return "bug";
  }
  return null;
}

function kindFromLabels(labels) {
  const names = Array.isArray(labels) ? labels : [];
  for (const [label, kind] of catalog.LABEL_TO_KIND) {
    if (names.includes(label)) return kind;
  }
  return null;
}

function labelForKind(kind) {
  return kind ? catalog.KIND_TO_LABEL[kind] || null : null;
}

function recognizeKind(issue) {
  const stored = issue?.storedKind;
  const detected = detectFromContent(issue);
  if (!stored) return detected;
  if (detected && detected !== stored) return detected;
  return stored;
}

function verdict(kind, reasons, guidance) {
  return {
    kind,
    valid: reasons.length === 0,
    softPass: false,
    reasons,
    guidance,
  };
}

function filledTexts(slots) {
  return slots.filter((item) => !item.empty).map((item) => item.text);
}

function genericDefects(title, filled) {
  const reasons = [];
  if (filled.length >= 2 && parse.sameCanonical(filled)) {
    reasons.push("Every filled field copies the same sentence.");
  }
  if (filled.length >= 2 && filled.every((text) => parse.echoesTitle(text, title))) {
    reasons.push("Every filled field restates the title instead of adding evidence.");
  }
  return reasons;
}

function insufficientDetail(text, minimumWords) {
  if (parse.isEmpty(text)) return false;
  if (parse.hasConcreteDetail(text)) return false;
  return parse.countWords(text) < minimumWords;
}

function liftReproduction(source, reproduction) {
  if (!parse.isEmpty(reproduction)) return reproduction;
  const lifted = parse.resolveSection(source, catalog.BUG.evidenceAliases);
  if (parse.isEmpty(lifted)) return reproduction;
  if (/\b(curl|go run|benes |HTTP\s+[1-5]\d\d|\/v1\/)/i.test(lifted) || /```/.test(lifted)) {
    return lifted;
  }
  return reproduction;
}

function evaluateFeature(title, source) {
  const reasons = [];
  const guidance = [];
  const job = slot(source, catalog.FEATURE.goal);
  const limitation = slot(source, catalog.FEATURE.limitation);
  const desired = slot(source, catalog.FEATURE.behaviour);
  const interaction = slot(source, catalog.FEATURE.example);
  const missing = [];
  if (job.empty) missing.push("the user job");
  if (limitation.present && limitation.empty) missing.push("the current limitation");
  if (desired.empty) missing.push("the desired behavior");
  const live = parse.countPresentHeadings(source, catalog.FEATURE.current) >= 2;
  if (interaction.present && parse.isPlaceholder(interaction.text)) {
    reasons.push("The concrete interaction is a placeholder, not an example command or request.");
  } else if ((live || interaction.present) && interaction.empty) {
    missing.push("a concrete interaction");
  }
  if (missing.length) {
    reasons.push(`The feature request is missing ${missing.join(", ")}.`);
    guidance.push("Describe the job, what Benes cannot do today, the desired result, and one concrete interaction.");
  }
  if (!job.empty && parse.isMediaOnly(job.text)) {
    reasons.push("The user job is only an image or link, with no written request.");
  }
  if (insufficientDetail(job.text, 8)) {
    reasons.push("The user job does not describe a concrete Benes workflow.");
  }
  reasons.push(...genericDefects(title, filledTexts([job, limitation, desired, interaction])));
  return verdict("feature", reasons, guidance);
}

function evaluateBug(title, source) {
  const reasons = [];
  const guidance = [];
  const client = slot(source, catalog.BUG.client);
  const observation = slot(source, catalog.BUG.failure);
  const reproduction = {
    present: slot(source, catalog.BUG.reproduction).present,
    text: liftReproduction(source, slot(source, catalog.BUG.reproduction).text),
  };
  reproduction.empty = parse.isEmpty(reproduction.text);
  const version = slot(source, catalog.BUG.version);
  const environment = slot(source, catalog.BUG.os);
  const liveForm =
    parse.countPresentHeadings(source, catalog.BUG.current) >= 2 ||
    parse.countPresentHeadings(source, catalog.BUG.aliases) >= 2 ||
    client.present;

  if (observation.empty && reproduction.empty) {
    reasons.push("The report has neither an observation nor a reproduction.");
  } else if (observation.empty) {
    reasons.push("The report does not say what the client or listener actually did.");
  } else if (reproduction.empty) {
    reasons.push("The report does not include a command or sequence that produces the failure.");
  }
  if (!reproduction.empty && insufficientDetail(reproduction.text, 10)) {
    reasons.push("The reproduction has no command, path, or other independent step.");
  }
  if (
    !observation.empty &&
    !reproduction.empty &&
    parse.fold(observation.text) === parse.fold(reproduction.text) &&
    !/\bbenes\b/i.test(reproduction.text)
  ) {
    reasons.push("The reproduction restates the observation instead of giving independent steps.");
  }
  if (liveForm) {
    if (version.empty || parse.isUnknownEnv(version.text)) {
      reasons.push("The Benes version or commit is missing or unusable.");
    }
    if (environment.empty || parse.isUnknownEnv(environment.text)) {
      reasons.push("The host operating system is missing or unusable.");
    }
  }
  if (
    !observation.empty &&
    !reproduction.empty &&
    parse.sameCanonical([observation.text, reproduction.text]) &&
    parse.echoesTitle(observation.text, title)
  ) {
    reasons.push("Observation and reproduction only repeat the title.");
  }
  return verdict("bug", reasons, guidance);
}

function evaluateProvider(title, source) {
  const reasons = [];
  const guidance = [];
  const provider = slot(source, catalog.PROVIDER.providerName);
  const capability = slot(source, catalog.PROVIDER.endpoint);
  const observed = slot(source, catalog.PROVIDER.currentBehaviour);
  const required = slot(source, catalog.PROVIDER.expectedBehaviour);
  const request = slot(source, catalog.PROVIDER.request);
  const response = slot(source, catalog.PROVIDER.response);
  const upstream = slot(source, catalog.PROVIDER.upstreamDocs);
  const version = slot(source, catalog.PROVIDER.benesVersion);
  const missing = [];
  if (provider.empty) missing.push("provider or capability owner");
  if (capability.empty) missing.push("listener path or capability");
  if (observed.empty) missing.push("observed behavior");
  if (required.empty) missing.push("required behavior");
  if (version.empty) missing.push("Benes version");
  if (missing.length) {
    reasons.push(`The compatibility report is missing ${missing.join(", ")}.`);
  }
  if (!observed.empty && !required.empty && parse.fold(observed.text) === parse.fold(required.text)) {
    reasons.push("Observed and required behavior describe the same outcome.");
  }
  if (request.empty && response.empty) {
    reasons.push("Neither a redacted request nor a response or error is present.");
  }
  if (upstream.empty && !/\bno public spec/i.test(upstream.text || "")) {
    reasons.push("No upstream reference is given, and the report does not declare that no public spec exists.");
  }
  reasons.push(...genericDefects(title, filledTexts([provider, observed, required])));
  return verdict("provider-compatibility", reasons, guidance);
}

function evaluateDocs(title, source) {
  const reasons = [];
  const guidance = [];
  const location = slot(source, catalog.DOCS.location);
  const problem = slot(source, catalog.DOCS.problem);
  const corrected = slot(source, catalog.DOCS.expected);
  if (parse.isPlaceholder(problem.text)) {
    reasons.push("The incorrect or missing statement is placeholder text.");
  } else if (location.empty && problem.empty) {
    reasons.push("The docs report names neither a page nor the incorrect statement.");
  }
  if (!problem.empty && parse.echoesTitle(problem.text, title) && (location.empty || parse.echoesTitle(location.text, title))) {
    reasons.push("The body only restates the title.");
  }
  return verdict("documentation", reasons, guidance);
}

function looksLikeUntemplatedBugReport(issue) {
  const source = issue?.body || "";
  if (!source.trim()) return false;
  const near = parse.countPresentHeadings(source, catalog.NEAR_MISS_BUG_HEADINGS);
  const hasRepro = parse.countPresentHeadings(source, ["Reproduction", "Steps to reproduce", "How to reproduce"]) > 0;
  const hasDescription = parse.hasHeading(source, "Description");
  const hasLog = parse.countPresentHeadings(source, ["Log", "Logs", "Log entry", "Error", "Error output", "Stack trace"]) > 0;
  return (hasDescription && (hasRepro || hasLog)) || (hasRepro && hasLog) || near >= 2;
}

function evaluateUntemplated(issue) {
  if (looksLikeUntemplatedBugReport(issue)) {
    return verdict(
      null,
      [
        "This reads as a failure report but it does not use Bug report headings (for example Description/Log entry instead of What happened).",
      ],
      [
        "Use the Bug report template, or edit this issue to include: Client that hit the failure, What happened, Commands that reproduce it, benes version or commit, and Host OS.",
      ],
    );
  }
  return verdict(null, ["This issue is not a recognized Bug, Feature, Documentation, or Provider compatibility form."], [
    "Open a new issue with the Bug report, Feature request, Documentation, or Provider compatibility template.",
  ]);
}

function evaluateIssue(issue) {
  const title = issue?.title || "";
  const source = issue?.body || "";
  const kind = recognizeKind(issue) || kindFromLabels(issue?.labels);
  if (kind === "feature") return evaluateFeature(title, source);
  if (kind === "bug") return evaluateBug(title, source);
  if (kind === "provider-compatibility") return evaluateProvider(title, source);
  if (kind === "documentation") return evaluateDocs(title, source);
  return evaluateUntemplated(issue);
}

module.exports = {
  recognizeKind,
  detectFromContent,
  kindFromLabels,
  labelForKind,
  evaluateIssue,
  looksLikeUntemplatedBugReport,
};
