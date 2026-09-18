import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { REACT_DOCTOR_SCAN_RULES } from "oxlint-plugin-react-doctor/core";

import {
  generatedConfigPath,
  serializedReactDoctorOxlintConfig,
  buildReactDoctorOxlintRules,
} from "./sync-react-doctor-oxlint-rules.ts";

test("generated react-doctor oxlint fragment is current", () => {
  const onDisk = readFileSync(generatedConfigPath, "utf8");
  assert.equal(onDisk, serializedReactDoctorOxlintConfig());
});

test("selected JS rules stay enabled as errors", () => {
  const rules = buildReactDoctorOxlintRules();
  assert.equal(rules["react-doctor/js-cache-property-access"], "error");
  assert.equal(rules["react-doctor/js-tosorted-immutable"], "error");
  assert.equal(rules["react-doctor/prefer-module-scope-static-value"], "error");
});

test("security-scan rules are not enabled under oxlint", () => {
  const rules = buildReactDoctorOxlintRules();
  assert.ok(REACT_DOCTOR_SCAN_RULES.length >= 42);
  for (const entry of REACT_DOCTOR_SCAN_RULES) {
    const key = `react-doctor/${entry.id}`;
    assert.notEqual(rules[key], "error", `${key} must not be enabled`);
    assert.equal(rules[key], undefined);
  }
});
