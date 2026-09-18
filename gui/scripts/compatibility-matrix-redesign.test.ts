import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import {
  EVIDENCE_LAYERS,
  PRODUCT_EVIDENCE_LAYERS,
  buildMatrixRows,
  preferredMatrixVerdict,
  suiteIdsFromVerdicts,
  verdictQueryFromFilters,
  type VerdictDto,
} from "../src/lab/evidence-matrix.ts";
import { catalogValue } from "../src/i18n/catalogs.ts";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const guiRoot = path.resolve(scriptDir, "..");
const sections = fs.readFileSync(path.join(guiRoot, "src", "pages", "compatibility-matrix-sections.tsx"), "utf8");
const matrixPage = fs.readFileSync(path.join(guiRoot, "src", "pages", "CompatibilityMatrix.tsx"), "utf8");
const css = fs.readFileSync(path.join(guiRoot, "src", "styles-compatibility-matrix.css"), "utf8");
const client = fs.readFileSync(path.join(guiRoot, "src", "lab", "lab-client.ts"), "utf8");
const matrix = fs.readFileSync(path.join(guiRoot, "src", "lab", "evidence-matrix.ts"), "utf8");

function verdict(partial: Partial<VerdictDto> & Pick<VerdictDto, "projectionKey" | "subjectId" | "evidenceLayer" | "suiteId" | "verdict">): VerdictDto {
  return {
    suiteVersion: "1",
    suiteManifestDigest: "d",
    projectionSpecVersion: "1",
    asOf: 1,
    scenarioManifestDigests: [],
    claimSourceDigest: null,
    contributingEventIds: [],
    contradictingEventIds: [],
    notes: [],
    ...partial,
  };
}

test("schema keeps task_effectiveness; product layers exclude it", () => {
  assert.ok(EVIDENCE_LAYERS.includes("task_effectiveness"));
  assert.deepEqual([...PRODUCT_EVIDENCE_LAYERS], ["protocol_conformance", "live_route_compatibility"]);
  assert.equal(PRODUCT_EVIDENCE_LAYERS.includes("task_effectiveness" as never), false);
});

test("matrix UI iterates PRODUCT_EVIDENCE_LAYERS only (no empty task column)", () => {
  assert.match(sections, /PRODUCT_EVIDENCE_LAYERS\.map/);
  assert.doesNotMatch(sections, /(?<!PRODUCT_)EVIDENCE_LAYERS\.map/);
  assert.doesNotMatch(sections, /lab\.col\.task/);
  assert.doesNotMatch(sections, /lab\.subjectKind/);
  assert.doesNotMatch(matrixPage, /(?<!PRODUCT_)EVIDENCE_LAYERS\.map/);
  assert.match(matrixPage, /PRODUCT_EVIDENCE_LAYERS\.map/);
});

test("buildMatrixRows emits one Subject+Suite row; subject shown only on first suite", () => {
  const rows = buildMatrixRows([
    verdict({
      projectionKey: "a",
      subjectId: "s1",
      evidenceLayer: "task_effectiveness",
      suiteId: "suite-a",
      verdict: "CLAIMED",
    }),
    verdict({
      projectionKey: "b",
      subjectId: "s1",
      evidenceLayer: "protocol_conformance",
      suiteId: "suite-a",
      verdict: "VERIFIED",
    }),
    verdict({
      projectionKey: "c",
      subjectId: "s1",
      evidenceLayer: "live_route_compatibility",
      suiteId: "suite-b",
      verdict: "DEGRADED",
      asOf: 2,
    }),
    verdict({
      projectionKey: "d",
      subjectId: "s1",
      evidenceLayer: "protocol_conformance",
      suiteId: "suite-b",
      verdict: "CLAIMED",
    }),
  ], [{ subjectId: "s1", subjectKind: "model" }]);
  assert.equal(rows.length, 2);
  assert.equal(rows[0]!.suiteId, "suite-a");
  assert.equal(rows[0]!.showSubject, true);
  assert.equal(rows[0]!.byLayer.protocol_conformance?.verdict, "VERIFIED");
  assert.equal(rows[0]!.byLayer.task_effectiveness?.verdict, "CLAIMED");
  assert.equal(rows[1]!.suiteId, "suite-b");
  assert.equal(rows[1]!.showSubject, false);
  assert.equal(rows[1]!.byLayer.live_route_compatibility?.verdict, "DEGRADED");
  assert.equal(preferredMatrixVerdict(rows[1]!)?.projectionKey, "c");
});

test("matrix rendering uses Subject|Suite|layer columns without stacking suites in a cell", () => {
  assert.match(sections, /lab\.col\.suite/);
  assert.doesNotMatch(sections, /lab-verdict-stack/);
  assert.doesNotMatch(sections, /lab-verdict-suite/);
  assert.match(sections, /row\.showSubject/);
  assert.match(sections, /preferredMatrixVerdict/);
  assert.match(css, /box-shadow:\s*inset 2px 0 0 var\(--accent\)/);
});

test("suite ids come from loaded verdict data", () => {
  assert.deepEqual(
    suiteIdsFromVerdicts([
      verdict({ projectionKey: "1", subjectId: "s", evidenceLayer: "protocol_conformance", suiteId: "beta", verdict: "PROBED" }),
      verdict({ projectionKey: "2", subjectId: "s", evidenceLayer: "live_route_compatibility", suiteId: "alpha", verdict: "VERIFIED" }),
      verdict({ projectionKey: "3", subjectId: "s", evidenceLayer: "protocol_conformance", suiteId: "alpha", verdict: "VERIFIED" }),
    ]),
    ["alpha", "beta"],
  );
  assert.match(matrixPage, /suiteIdsFromVerdicts/);
  assert.match(sections, /lab-filter-suite/);
});

test("KPI cards and local Refresh toolbar are removed", () => {
  assert.doesNotMatch(sections, /lab-status-grid|lab-status-card|StatusCards|CompatibilityMatrixToolbar|lab\.refresh/);
  assert.doesNotMatch(sections, /IconRefresh/);
  assert.match(sections, /lab-projection-summary/);
  assert.match(sections, /lab\.summary\.counts/);
  assert.match(matrixPage, /refreshNonce/);
});

test("subviews are restrained text tabs; switching clears detail", () => {
  assert.match(sections, /lab-subview-tabs/);
  assert.match(sections, /role="tablist"/);
  assert.match(sections, /lab\.tab\.matrix/);
  assert.match(sections, /lab\.tab\.records/);
  assert.doesNotMatch(sections, /segmented|pill-tab/);
  assert.match(matrixPage, /changeSubview/);
  assert.match(matrixPage, /clearDetail\(\);\s*setSubview\(next\)/s);
});

test("verdict pills replaced with flat lines; detail is flat side panel without artifacts/community", () => {
  assert.match(sections, /lab-verdict-line/);
  assert.doesNotMatch(sections, /lab-verdict-badge/);
  assert.doesNotMatch(sections, /lab\.detailArtifacts|CommunityEvidencePanel|lab-community-evidence/);
  assert.match(sections, /lab-layout--with-detail/);
  assert.match(css, /minmax\(360px, 420px\)/);
  assert.match(sections, /Escape/);
  assert.match(client, /artifacts: \[\]/);
  assert.doesNotMatch(client, /fetchArtifactByDigest|artifactDigestsForVerdict/);
  assert.match(sections, /lab-records-footer/);
  assert.match(sections, /lab\.showing\.counts/);
  // Title is the canonical Subject identity; compact meta must not repeat Subject.
  assert.match(sections, /<h3 className="mono" title=\{verdict\.subjectId\}>\{verdict\.subjectId\}<\/h3>/);
  const compactMeta = sections.match(/lab-detail-meta--compact[\s\S]*?<\/dl>/)?.[0] ?? "";
  assert.match(compactMeta, /lab\.col\.layer/);
  assert.match(compactMeta, /lab\.col\.suite/);
  assert.match(compactMeta, /lab\.col\.suiteVersion/);
  assert.match(compactMeta, /lab\.col\.asOf/);
  assert.doesNotMatch(compactMeta, /lab\.col\.subject/);
});

test("subject filter is free-text search on subjectId only", () => {
  assert.match(sections, /type="search"/);
  assert.match(sections, /lab\.filter\.subjectPlaceholder/);
  assert.match(sections, /filters\.subjectQuery/);
  assert.doesNotMatch(sections, /subjectOptions/);
  assert.doesNotMatch(matrixPage, /subjectOptions/);
  assert.match(matrixPage, /filterVerdicts\(/);
  assert.deepEqual(
    verdictQueryFromFilters({ layer: "", verdict: "", subjectQuery: "gpt-4", suiteId: "" }),
    {},
  );
});

test("filter toolbar and Built metadata polish", () => {
  assert.match(css, /lab-filter-field--layer/);
  assert.match(css, /lab-filter-field--verdict/);
  assert.match(css, /lab-filter-field--suite/);
  assert.match(css, /\.lab-filter-field \.select-trigger/);
  assert.match(css, /\.lab-matrix td\.subject[^{]*\{[^}]*font-weight:\s*var\(--weight-semibold\)/s);
  assert.match(css, /\.lab-detail-table \.lab-verdict-status[^{]*\{[^}]*font-weight:\s*var\(--weight-medium\)/s);
  assert.match(sections, /formatBuiltAt/);
  assert.match(matrix, /export function formatBuiltAt/);
  assert.match(matrix, /minute:\s*"2-digit"/);
  assert.equal(catalogValue("en", "lab.filter.subjectPlaceholder"), "Search subjects...");
});

test("zero filter matches use distinct empty copy", () => {
  assert.match(sections, /lab\.emptyFiltered/);
  assert.match(sections, /filtersActive \? t\("lab\.emptyFiltered"\) : t\("lab\.empty"\)/);
});

test("catalog carries redesign keys", () => {
  assert.equal(catalogValue("en", "lab.summary.counts"), "{subjects} subjects · {verdicts} verdicts");
  assert.equal(catalogValue("en", "lab.tab.matrix"), "Matrix");
  assert.equal(catalogValue("en", "lab.filter.suite"), "Suite");
  assert.equal(catalogValue("en", "lab.col.live"), "Derived compatibility");
  assert.equal(catalogValue("en", "lab.showing.counts"), "Showing {shown} of {total} verdicts");
  assert.match(catalogValue("de", "lab.emptyFiltered"), /Filtern|Urteile/i);
});

test("P0 stale-refresh notices remain wired", () => {
  assert.match(matrixPage, /compatibilityPageNotices\(/);
  assert.match(matrixPage, /lab\.refreshFailedStale/);
  assert.match(sections, /staleRefreshWarning && !loadError/);
});
