/**
 * Generate `gui/react-doctor.oxlintrc.json` from oxlint-plugin-react-doctor@0.9.12.
 *
 * Plugin rule keys are unprefixed (`js-tosorted-immutable`); oxlint config uses
 * `react-doctor/<id>`. Re-run after a plugin bump:
 * `node --experimental-strip-types ./scripts/sync-react-doctor-oxlint-rules.ts`
 *
 * Off reasons live in `gui/.react-doctor-ignores.md`.
 */
import { writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import {
  REACT_DOCTOR_RULES,
  REACT_DOCTOR_SCAN_RULES,
} from "oxlint-plugin-react-doctor/core";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
export const guiRoot = path.resolve(scriptDir, "..");
export const generatedConfigPath = path.join(guiRoot, "react-doctor.oxlintrc.json");

/** Frameworks this Vite dashboard does not use. */
export const SKIP_FRAMEWORKS = Object.freeze([
  "react-native",
  "nextjs",
  "preact",
  "tanstack-query",
  "tanstack-start",
]);

/**
 * Rules previously off in `gui/doctor.config.json`. Keep them off until the
 * matching dashboard debt in `.react-doctor-ignores.md` is gone.
 */
export const EXISTING_OFF_RULES = Object.freeze([
  "no-fetch-in-effect",
  "no-giant-component",
  "prefer-useReducer",
  "no-pass-live-state-to-parent",
  "no-pass-data-to-parent",
  "no-prop-callback-in-effect",
  "no-derived-state",
  "no-derived-state-effect",
  "only-export-components",
  "prefer-html-dialog",
  "js-hoist-intl",
  "no-loading-flag-reset-outside-finally",
  "rerender-state-only-in-handlers",
  "prefer-use-effect-event",
  "js-combine-iterations",
]);

/**
 * Rules that currently fire on full-tree `dev`. Do not enable them in this
 * conversion — that would mean fixing ~1700 JSX/a11y findings in the same PR.
 */
export const CURRENTLY_FIRING_OFF_RULES = Object.freeze([
  "jsx-no-new-function-as-prop",
  "jsx-no-new-object-as-prop",
  "react-compiler-no-manual-memoization",
  "jsx-max-depth",
  "jsx-no-new-array-as-prop",
  "control-has-associated-label",
  "jsx-no-jsx-as-prop",
  "no-locale-format-in-render",
  "no-unguarded-browser-global-in-render-or-hook-init",
  "rerender-memo-with-default-value",
  "no-adjust-state-on-prop-change",
  "no-array-index-as-key",
  "prefer-tag-over-role",
  "no-fetch-response-used-without-status-check",
  "js-set-map-lookups",
  "no-chain-state-updates",
  "async-await-in-loop",
  "prefer-module-scope-pure-function",
  "no-set-state-after-await-in-effect",
  "prefer-use-sync-external-store",
  "no-unstable-nested-components",
  "js-flatmap-filter",
]);

const SKIP_FRAMEWORK_SET = new Set(SKIP_FRAMEWORKS);
const OFF_RULE_SET = new Set([...EXISTING_OFF_RULES, ...CURRENTLY_FIRING_OFF_RULES]);

function scanRuleIds() {
  return new Set(REACT_DOCTOR_SCAN_RULES.map((entry) => entry.id));
}

function ruleMeta(entry) {
  return entry.rule ?? {};
}

export function shouldSkipReactDoctorRule(entry, scanIds = scanRuleIds()) {
  const meta = ruleMeta(entry);
  if (meta.isScanRule) return true;
  if (scanIds.has(entry.id)) return true;
  if (SKIP_FRAMEWORK_SET.has(meta.framework)) return true;
  if (meta.defaultEnabled === false) return true;
  if (meta.lifecycle === "retired") return true;
  return false;
}

export function oxlintRuleKey(entry) {
  return `react-doctor/${entry.id}`;
}

export function buildReactDoctorOxlintRules() {
  const scanIds = scanRuleIds();
  const rules = {};
  for (const entry of REACT_DOCTOR_RULES) {
    if (shouldSkipReactDoctorRule(entry, scanIds)) continue;
    rules[oxlintRuleKey(entry)] = OFF_RULE_SET.has(entry.id) ? "off" : "error";
  }
  return Object.fromEntries(Object.entries(rules).sort(([a], [b]) => a.localeCompare(b)));
}

export function buildReactDoctorOxlintConfig() {
  return {
    $schema: "./node_modules/oxlint/configuration_schema.json",
    rules: buildReactDoctorOxlintRules(),
  };
}

export function serializedReactDoctorOxlintConfig() {
  return `${JSON.stringify(buildReactDoctorOxlintConfig(), null, 2)}\n`;
}

export function writeReactDoctorOxlintConfig() {
  writeFileSync(generatedConfigPath, serializedReactDoctorOxlintConfig());
}

const invoked = process.argv[1] ? path.resolve(process.argv[1]) : "";
if (invoked === fileURLToPath(import.meta.url)) {
  writeReactDoctorOxlintConfig();
}
