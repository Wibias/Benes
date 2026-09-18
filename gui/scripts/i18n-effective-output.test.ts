import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import test from "node:test";

import { catalogValue } from "../src/i18n/catalogs.ts";
import { en, type TKey } from "../src/i18n/en.ts";
import { labCatalogKeys, labCatalogText, labSupplement } from "../src/i18n/lab.ts";
import {
  logGuardLabel,
  logGuardOperationLabel,
  logGuardProtectionModeLabel,
  logGuardProtectionStateLabel,
  logGuardSchemaStateLabel,
} from "../src/i18n/log-guard.ts";
import { routingCompatibilityFieldLabel } from "../src/i18n/routing-compatibility.ts";
import { LOCALE_PROFILES, type Locale } from "../src/i18n/locale-registry.ts";
import { EFFECTIVE_OUTPUT_FIXTURE } from "./i18n-effective-output.fixture.ts";

function digest(text: string): string {
  return createHash("sha256").update(text, "utf8").digest("hex");
}

const SIDECAR_KEY_PREFIX = "harnesses.sidecar.";

const SUPPLEMENT_KEYS = [
  "subjectKindUnknown",
  "artifact.present",
  "artifact.corrupt",
  "artifact.purged_unavailable",
  "selectVerdict",
  "community.title",
  "community.notLocalVerdict",
  "community.bundles",
  "community.activeRecords",
  "community.revokedRecords",
] as const;

const LABEL_KEYS = [
  "inspectionOnly",
  "externalSqliteHome",
  "inspectionUnavailable",
  "protection",
  "compat",
  "quiet",
  "disable",
  "repair",
  "compact",
  "pagesUnit",
  "compactComplete",
  "compactPartial",
  "confirmCompact",
  "cancel",
  "applying",
  "error.generic",
  "error.codex_running",
  "error.process_enumeration_failed",
  "error.busy",
  "error.unsupported_schema",
  "error.trigger_collision",
  "error.unsafe_path",
  "error.database_error",
  "error.config_write_failed",
] as const;

const OPERATION_KEYS = [
  "applying",
  "error.generic",
  "error.codex_running",
  "error.process_enumeration_failed",
  "error.busy",
  "error.unsupported_schema",
  "error.unsafe_path",
  "error.database_error",
  "error.auto_vacuum_not_incremental",
  "error.integrity_check_failed",
] as const;

test("effective canonical translations match the Phase 2 freeze", async () => {
  const fixture = EFFECTIVE_OUTPUT_FIXTURE;
  const keys = Object.keys(en) as TKey[];
  const canonicalSorted = [...keys].sort();
  const sidecarSorted = canonicalSorted.filter((key) => key.startsWith(SIDECAR_KEY_PREFIX));
  const baselineSorted = canonicalSorted.filter((key) => !key.startsWith(SIDECAR_KEY_PREFIX));

  // Keep the post-#276 baseline freeze intact while freezing #257's independent
  // Harness sidecar copy separately. This prevents either branch from silently
  // rewriting the other's established effective-output evidence during merge.
  assert.equal(canonicalSorted.length, fixture.canonicalKeyCount + fixture.sidecarKeyCount);
  assert.equal(baselineSorted.length, fixture.canonicalKeyCount);
  assert.equal(digest(baselineSorted.join("\n")), fixture.canonicalDigest);
  assert.equal(sidecarSorted.length, fixture.sidecarKeyCount);
  assert.equal(digest(sidecarSorted.join("\n")), fixture.sidecarDigest);
  assert.deepEqual(LOCALE_PROFILES.map((profile) => profile.code), fixture.locales);

  for (const locale of fixture.locales) {
    const baselineLines = baselineSorted.map((key) => `${key}\0${catalogValue(locale, key)}`);
    assert.equal(digest(baselineLines.join("\n")), fixture.localeDigests[locale], `${locale} baseline`);

    const sidecarLines = sidecarSorted.map((key) => `${key}\0${catalogValue(locale, key)}`);
    assert.equal(digest(sidecarLines.join("\n")), fixture.sidecarLocaleDigests[locale], `${locale} sidecar`);
  }
});

test("lab overlay and supplements match the Phase 2 freeze", async () => {
  const fixture = EFFECTIVE_OUTPUT_FIXTURE;
  const labKeys = labCatalogKeys();
  assert.equal(labKeys.length, fixture.labCatalogKeyCount);
  const locales = fixture.locales;
  const labLines: string[] = [];
  for (const locale of locales) {
    for (const key of labKeys) labLines.push(`${locale}\0${key}\0${labCatalogText(locale, key)}`);
  }
  assert.equal(digest(labLines.join("\n")), fixture.labOverlayDigest);
  assert.equal(catalogValue("en", "lab.detailEvents"), "Evidence events");

  const supplementLines: string[] = [];
  for (const locale of locales) {
    for (const key of SUPPLEMENT_KEYS) supplementLines.push(`${locale}\0${key}\0${labSupplement(locale, key)}`);
  }
  assert.equal(digest(supplementLines.join("\n")), fixture.labSupplementDigest);
});

test("log-guard and routing closed copy is preserved", async () => {
  const fixture = EFFECTIVE_OUTPUT_FIXTURE;
  const locales = fixture.locales;
  const schemaStates = ["compatible", "missing", "unreadable", "unsupported"] as const;
  const protectionStates = ["off", "active", "drifted", "unsupported", "unknown"] as const;
  const protectionModes = ["off", "compat", "quiet", "collision"] as const;
  const routingFields = ["maxEvidenceAgeMs", "unknownEvidence", "degradedEvidence"] as const;
  const logGuardLines: string[] = [];
  const routingLines: string[] = [];
  for (const locale of locales) {
    for (const key of LABEL_KEYS) logGuardLines.push(`lg.label.${locale}.${key}\0${logGuardLabel(locale, key)}`);
    for (const key of OPERATION_KEYS) logGuardLines.push(`lg.op.${locale}.${key}\0${logGuardOperationLabel(locale, key)}`);
    for (const key of schemaStates) logGuardLines.push(`lg.schema.${locale}.${key}\0${logGuardSchemaStateLabel(locale, key)}`);
    for (const key of protectionStates) logGuardLines.push(`lg.prot.${locale}.${key}\0${logGuardProtectionStateLabel(locale, key)}`);
    for (const key of protectionModes) logGuardLines.push(`lg.mode.${locale}.${key}\0${logGuardProtectionModeLabel(locale, key)}`);
    for (const field of routingFields) {
      routingLines.push(`rc.${locale}.${field}\0${routingCompatibilityFieldLabel(locale, field)}`);
    }
  }
  assert.equal(digest(logGuardLines.join("\n")), fixture.logGuardDigest);
  assert.equal(digest(routingLines.join("\n")), fixture.routingCompatibilityDigest);
  assert.equal(logGuardLabel("en", "compact"), "Compact");
  assert.equal(routingCompatibilityFieldLabel("en", "maxEvidenceAgeMs"), "Maximum evidence age (ms)");
});
