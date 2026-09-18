"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { missingMaintainerSponsorship } = require("./pr-sponsored-surface.cjs");
const {
  TEST_EXCEPTION_LABEL,
  SUPPRESSION_LABEL,
  GENERATED_LABEL,
  DEPENDENCY_LABEL,
  HYGIENE_BLOCKED_LABEL,
  SPONSOR_LABEL,
} = require("./pr-quality-frozen.cjs");

const BEHAVIOR_ROOTS = ["internal/", "cmd/", "gui/src/"];
const GENERATED_ROOTS = ["gui/dist/", "docs/dist/", "coverage/", "node_modules/", "dist/"];
const TEST_PATH = /(?:^|\/)(?:__tests__\/.+|[^/]+\.(?:test|spec)\.[^.]+|[^/]+_test\.go)$/;
const SUPPRESSION_MARKERS = [
  "oxlint-disable",
  "eslint-disable",
  "@ts-ignore",
  "@ts-nocheck",
  "prettier-ignore",
];
const FOCUSED_TEST = /\b(?:describe|it|test)\.(?:only|skip)\s*\(/;
const EMPTY_CATCH = /catch\s*(?:\([^)]*\))?\s*\{\s*\}/;

const HYGIENE_HINTS = {
  missing_regression_test:
    "A behavior path changed without coverage in its own test domain. Go packages need a surviving `_test.go` in that package (`cmd/benes` is one domain). GUI `gui/src` changes need a surviving GUI test that imports that source, or a conservative same-stem policy test. Add coverage or obtain `test-exception-approved`.",
  generated_output:
    "Generated output was committed. Remove it or obtain `generated-change-approved`.",
  orphan_lockfile:
    "`package-lock.json` changed without `package.json`. Revert the churn or obtain `dependency-change-approved`.",
  new_suppression:
    "A new Oxlint, ESLint, TypeScript, or formatter suppression was added. Fix the cause or obtain `suppression-approved`.",
  focused_or_skipped_test:
    "A focused or skipped test was added. Restore the full suite or obtain `test-exception-approved`.",
  empty_catch:
    "An empty catch block was added. Handle, report, or rethrow the error.",
  unsponsored_surface:
    "This changes an authentication, workflow, release-automation, or dependency surface. Ask a maintainer to apply `maintainer-sponsored` after security review.",
};

const WAKE_LABELS = [
  HYGIENE_BLOCKED_LABEL,
  SPONSOR_LABEL,
  TEST_EXCEPTION_LABEL,
  SUPPRESSION_LABEL,
  GENERATED_LABEL,
  DEPENDENCY_LABEL,
];

const HASH_COMMENT_EXT = new Set([
  ".py",
  ".sh",
  ".bash",
  ".zsh",
  ".yml",
  ".yaml",
  ".toml",
  ".rb",
  ".ps1",
  ".psm1",
  ".r",
  ".pl",
  ".mak",
  ".mk",
  ".gitignore",
  ".npmignore",
  ".dockerignore",
  ".editorconfig",
  ".env",
]);

function classifyDiffLine(line) {
  if (line.startsWith("@@")) return { kind: "hunk" };
  if (line.startsWith("+") && !line.startsWith("+++")) {
    return { kind: "add", text: line.slice(1) };
  }
  if (line.startsWith("-") && !line.startsWith("---")) {
    return { kind: "del", text: line.slice(1) };
  }
  if (line.startsWith(" ")) return { kind: "ctx", text: line.slice(1) };
  return { kind: "meta" };
}

function fileUsesHashComments(filePath) {
  const name = String(filePath || "")
    .replace(/\\/g, "/")
    .split("/")
    .pop() || "";
  if (/^(makefile|dockerfile|jenkinsfile)(\.|$)/i.test(name)) return true;
  const dot = name.lastIndexOf(".");
  if (dot <= 0) return false;
  return HASH_COMMENT_EXT.has(name.slice(dot).toLowerCase());
}

function classifyCommentLine(raw, inBlock, allowHash) {
  const text = String(raw ?? "").trim();
  if (inBlock) {
    const close = text.indexOf("*/");
    if (close < 0) return { comment: true, inBlock: true };
    const after = text.slice(close + 2).trim();
    if (after === "") return { comment: true, inBlock: false };
    return classifyCommentLine(after, false, allowHash);
  }
  if (text === "") return { comment: true, inBlock: false };
  if (text.startsWith("#!") || text.startsWith("//")) {
    return { comment: true, inBlock: false };
  }
  if (allowHash && text.startsWith("#")) return { comment: true, inBlock: false };
  if (text.startsWith("/*")) {
    const close = text.indexOf("*/", 2);
    if (close < 0) return { comment: true, inBlock: true };
    const after = text.slice(close + 2).trim();
    if (after === "") return { comment: true, inBlock: false };
    return classifyCommentLine(after, false, allowHash);
  }
  return { comment: false, inBlock: false };
}

function isCommentOnlyPatch(patch, filePath) {
  if (typeof patch !== "string" || patch === "") return false;
  const allowHash = fileUsesHashComments(filePath);
  let inBlock = false;
  let sawChange = false;
  for (const line of patch.split("\n")) {
    const item = classifyDiffLine(line);
    if (item.kind === "hunk") {
      // @@ windows are independent. Omitted source may open or close a block comment.
      if (inBlock) return false;
      inBlock = false;
      continue;
    }
    if (item.kind === "meta") continue;
    const classified = classifyCommentLine(item.text, inBlock, allowHash);
    inBlock = classified.inBlock;
    if (item.kind !== "add" && item.kind !== "del") continue;
    sawChange = true;
    if (!classified.comment) return false;
  }
  return sawChange;
}

function inspectPatch(patch) {
  const added = [];
  const deleted = [];
  const hunks = [];
  if (typeof patch !== "string") return { added, deleted, hunks };
  let hunk = null;
  for (const line of patch.split("\n")) {
    const item = classifyDiffLine(line);
    if (item.kind === "hunk") {
      hunk = { surviving: [] };
      hunks.push(hunk);
      continue;
    }
    if (hunk === null) {
      hunk = { surviving: [] };
      hunks.push(hunk);
    }
    if (item.kind === "add") {
      added.push(item.text);
      hunk.surviving.push(item.text);
    } else if (item.kind === "del") {
      deleted.push(item.text);
    } else if (item.kind === "ctx") {
      hunk.surviving.push(item.text);
    }
  }
  return { added, deleted, hunks };
}

function buildChangeSet(files = []) {
  const entries = [];
  const paths = new Set();
  for (const file of files) {
    const pathName = file.filename;
    const previous = file.previous_filename || null;
    paths.add(pathName);
    if (previous) paths.add(previous);
    const patch = inspectPatch(file.patch);
    entries.push({
      path: pathName,
      previous,
      removed: file.status === "removed",
      added: patch.added,
      deleted: patch.deleted,
      hunks: patch.hunks,
      commentOnly: isCommentOnlyPatch(file.patch, pathName),
    });
  }
  return { entries, paths: [...paths] };
}

function inRoot(filePath, roots) {
  return roots.some((root) => filePath.startsWith(root));
}

function isTestPath(filePath) {
  return TEST_PATH.test(filePath);
}

function posixPath(filePath) {
  return String(filePath || "").replace(/\\/g, "/");
}

function goCoverageDomain(filePath) {
  const pathName = posixPath(filePath);
  if (!pathName.endsWith(".go")) return null;
  if (pathName.startsWith("cmd/benes/")) return "go:cmd/benes";
  if (pathName.startsWith("internal/") || pathName.startsWith("cmd/")) {
    const dir = pathName.slice(0, pathName.lastIndexOf("/"));
    return `go:${dir}`;
  }
  return null;
}

function guiFileStem(filePath) {
  const base = posixPath(filePath).split("/").pop() || "";
  return base
    .replace(/\.(?:test|spec)\.[^.]+$/i, "")
    .replace(/_test\.go$/i, "")
    .replace(/\.[^.]+$/, "");
}

function normalizeGuiStem(value) {
  return String(value || "")
    .replace(/([a-z0-9])([A-Z])/g, "$1-$2")
    .replace(/[_\s]+/g, "-")
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-|-$/g, "");
}

const GENERIC_GUI_TOKENS = new Set([
  "provider",
  "providers",
  "page",
  "pages",
  "model",
  "models",
  "test",
  "tests",
  "src",
  "lib",
  "component",
  "components",
  "script",
  "scripts",
  "form",
  "modal",
  "util",
  "utils",
  "hook",
  "hooks",
]);

function guiSourcePath(filePath) {
  const pathName = posixPath(filePath);
  if (!pathName.startsWith("gui/src/") || isTestPath(pathName)) return null;
  return pathName;
}

function defaultReadRepoFile(filePath) {
  return fs.readFileSync(path.join(process.cwd(), posixPath(filePath)), "utf8");
}

function resolveGuiImport(fromFile, spec) {
  const specifier = String(spec || "");
  if (!specifier.startsWith(".")) return [];
  const fromDir = posixPath(fromFile).split("/").slice(0, -1).join("/");
  const joined = path.posix.normalize(`${fromDir}/${specifier}`);
  const candidates = [];
  if (/\.[a-z][a-z0-9]+$/i.test(joined)) {
    candidates.push(joined);
  } else {
    for (const ext of [".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"]) {
      candidates.push(`${joined}${ext}`);
      candidates.push(`${joined}/index${ext}`);
    }
  }
  return candidates.filter((item) => item.startsWith("gui/src/") && !isTestPath(item));
}

function guiImportsInText(fromFile, content) {
  const covered = new Set();
  const pattern = /(?:from\s+|import\s*\(\s*|require\s*\(\s*)["']([^"']+)["']/g;
  let match = pattern.exec(content);
  while (match) {
    for (const resolved of resolveGuiImport(fromFile, match[1])) covered.add(resolved);
    match = pattern.exec(content);
  }
  return covered;
}

function guiTestSourceText(filePath, entry, readFile) {
  try {
    return readFile(filePath);
  } catch {
    // Head blob unavailable: keep only diff text already in the change set.
  }
  const chunks = [];
  if (entry?.added?.length) chunks.push(entry.added.join("\n"));
  if (entry?.hunks?.length) {
    chunks.push(entry.hunks.flatMap((hunk) => hunk.surviving || []).join("\n"));
  }
  return chunks.join("\n");
}

function guiNamingCovers(sourcePath, testPath) {
  const sourceStem = normalizeGuiStem(guiFileStem(sourcePath));
  const testStem = normalizeGuiStem(guiFileStem(testPath));
  if (!sourceStem || !testStem) return false;
  if (sourceStem === testStem) return true;
  if (!testStem.startsWith(`${sourceStem}-`) && !sourceStem.startsWith(`${testStem}-`)) return false;
  const shared = sourceStem.length <= testStem.length ? sourceStem : testStem;
  const tokens = shared.split("-").filter(Boolean);
  if (tokens.length === 0) return false;
  if (tokens.length === 1 && GENERIC_GUI_TOKENS.has(tokens[0])) {
    // A source named Providers.tsx may still be covered by providers-*.test.*
    return testStem.startsWith(`${sourceStem}-`);
  }
  return true;
}

function guiColocatedTest(sourcePath, testPath) {
  const source = posixPath(sourcePath);
  const test = posixPath(testPath);
  if (!test.startsWith("gui/src/") || !isTestPath(test)) return false;
  const sourceDir = source.split("/").slice(0, -1).join("/");
  const testDir = test.split("/").slice(0, -1).join("/");
  return sourceDir === testDir && normalizeGuiStem(guiFileStem(source)) === normalizeGuiStem(guiFileStem(test));
}

function collectGuiCoverage(changeSet, readFile) {
  const imported = new Set();
  const tests = [];
  for (const entry of changeSet.entries) {
    if (entry.removed) continue;
    const pathName = posixPath(entry.path);
    if (!isTestPath(pathName)) continue;
    if (!pathName.startsWith("gui/src/") && !pathName.startsWith("gui/scripts/")) continue;
    tests.push(pathName);
    const text = guiTestSourceText(pathName, entry, readFile);
    for (const importedPath of guiImportsInText(pathName, text)) imported.add(importedPath);
  }
  return { imported, tests };
}

function guiSourceIsCovered(sourcePath, coverage) {
  const pathName = posixPath(sourcePath);
  if (coverage.imported.has(pathName)) return true;
  return coverage.tests.some(
    (testPath) => guiColocatedTest(pathName, testPath) || guiNamingCovers(pathName, testPath),
  );
}

function behaviorDomains(filePath) {
  const go = goCoverageDomain(filePath);
  if (go) return isTestPath(filePath) ? [] : [go];
  const gui = guiSourcePath(filePath);
  return gui ? [`gui:${gui}`] : [];
}

function coverageDomains(filePath) {
  const pathName = posixPath(filePath);
  if (!isTestPath(pathName)) return [];
  const go = goCoverageDomain(pathName);
  return go ? [go] : [];
}

function survivingEntry(changeSet, filePath) {
  return changeSet.entries.find((item) => item.path === filePath || item.previous === filePath);
}

function pathNeedsBehaviorCoverage(changeSet, filePath) {
  if (!inRoot(posixPath(filePath), BEHAVIOR_ROOTS)) return false;
  if (behaviorDomains(filePath).length === 0) return false;
  const entry = survivingEntry(changeSet, filePath);
  return !entry || !entry.commentOnly;
}

function emptyCatchPresent(entry) {
  const windows = entry.deleted.length > 0
    ? entry.hunks.map((hunk) => hunk.surviving)
    : [entry.added];
  return windows.some((lines) => EMPTY_CATCH.test(lines.join("\n")));
}

function ruleMissingTest(changeSet, labels, readFile = defaultReadRepoFile) {
  if (labels.has(TEST_EXCEPTION_LABEL)) return null;
  const covered = new Set();
  for (const filePath of changeSet.paths) {
    const removed = changeSet.entries.some((entry) => entry.path === filePath && entry.removed);
    if (removed || !isTestPath(filePath)) continue;
    for (const domain of coverageDomains(filePath)) covered.add(domain);
  }
  const guiCoverage = collectGuiCoverage(changeSet, readFile);
  const uncovered = [];
  for (const filePath of changeSet.paths) {
    if (!pathNeedsBehaviorCoverage(changeSet, filePath)) continue;
    if (guiSourcePath(filePath)) {
      if (guiSourceIsCovered(filePath, guiCoverage)) continue;
      uncovered.push(filePath);
      continue;
    }
    const domains = behaviorDomains(filePath);
    if (domains.some((domain) => covered.has(domain))) continue;
    uncovered.push(filePath);
  }
  if (uncovered.length) return { code: "missing_regression_test", paths: uncovered };
  return null;
}

function ruleGenerated(changeSet, labels) {
  if (labels.has(GENERATED_LABEL)) return null;
  const generated = changeSet.paths.filter(
    (filePath) =>
      inRoot(filePath, GENERATED_ROOTS) &&
      !changeSet.entries.some((entry) => entry.path === filePath && entry.removed),
  );
  if (generated.length) return { code: "generated_output", paths: generated };
  return null;
}

function ruleOrphanLockfile(changeSet, labels) {
  if (labels.has(DEPENDENCY_LABEL)) return null;
  const lock = changeSet.paths.includes("package-lock.json");
  const lockRemoved = changeSet.entries.some((entry) => entry.path === "package-lock.json" && entry.removed);
  const pkg = changeSet.paths.includes("package.json");
  if (lock && !lockRemoved && !pkg) return { code: "orphan_lockfile" };
  return null;
}

function ruleSuppressions(changeSet, labels) {
  const paths = changeSet.entries
    .filter((entry) => entry.added.some((line) => SUPPRESSION_MARKERS.some((marker) => line.includes(marker))))
    .map((entry) => entry.path);
  if (paths.length && !labels.has(SUPPRESSION_LABEL)) {
    return { code: "new_suppression", paths };
  }
  return null;
}

function ruleFocusedTests(changeSet, labels) {
  const paths = changeSet.entries
    .filter((entry) => entry.added.some((line) => FOCUSED_TEST.test(line)))
    .map((entry) => entry.path);
  if (paths.length && !labels.has(TEST_EXCEPTION_LABEL)) {
    return { code: "focused_or_skipped_test", paths };
  }
  return null;
}

function ruleEmptyCatch(changeSet) {
  const paths = changeSet.entries.filter(emptyCatchPresent).map((entry) => entry.path);
  if (paths.length) return { code: "empty_catch", paths };
  return null;
}

function evaluatePatchHygiene({ files = [], labels = [], readFile = defaultReadRepoFile } = {}) {
  const changeSet = buildChangeSet(files);
  const labelSet = new Set(labels);
  return [
    (changeSetArg, labelsArg) => ruleMissingTest(changeSetArg, labelsArg, readFile),
    ruleGenerated,
    ruleOrphanLockfile,
    ruleSuppressions,
    ruleFocusedTests,
    ruleEmptyCatch,
  ]
    .map((rule) => rule(changeSet, labelSet))
    .filter(Boolean);
}

function evaluateHygiene({ files = [], labels = [], authorHasPushPermission = false, readFile = defaultReadRepoFile } = {}) {
  const sides = [
    ...new Set(
      (files || []).flatMap((file) => [
        file.filename,
        ...(file.previous_filename ? [file.previous_filename] : []),
      ]),
    ),
  ];
  return [
    ...evaluatePatchHygiene({ files, labels, readFile }),
    ...missingMaintainerSponsorship({
      authorHasPushPermission,
      changedFiles: sides,
      labels,
    }),
  ];
}

module.exports = {
  evaluatePatchHygiene,
  evaluateHygiene,
  hygieneFailures: evaluateHygiene,
  isCommentOnlyPatch,
  buildChangeSet,
  goCoverageDomain,
  guiNamingCovers,
  guiImportsInText,
  HYGIENE_HINTS,
  WAKE_LABELS,
};
