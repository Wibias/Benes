import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import {
  AUTO_COMPACT_DEFAULT,
  claudeSettingsPutBody,
  compactFieldValue,
  compactInputToOverride,
  decodeClaudeSettings,
} from "../src/pages/harnesses/claude-settings-state.ts";

const ROOT = path.join(path.dirname(fileURLToPath(import.meta.url)), "..");
const SETTINGS = path.join(ROOT, "src/pages/harnesses/claude-settings.tsx");

test("resetting an explicit compaction override persists inherited null", () => {
  const baseline = decodeClaudeSettings({ autoCompactWindow: 350_000 }).draft;
  const resetDraft = { ...baseline, autoCompactWindow: null };
  const compactText = compactFieldValue(resetDraft.autoCompactWindow);

  assert.equal(compactText, String(AUTO_COMPACT_DEFAULT));
  const compactParsed = compactInputToOverride(compactText, resetDraft.autoCompactWindow);
  assert.equal(compactParsed, null);

  const liveDraft = { ...resetDraft, autoCompactWindow: compactParsed };
  assert.equal(claudeSettingsPutBody(baseline, liveDraft).autoCompactWindow, null);

  const settings = readFileSync(SETTINGS, "utf8");
  assert.equal(settings.includes("compactInputToOverride(compactText, draft.autoCompactWindow)"), true);
  assert.equal(settings.includes("compactInputToOverride(compactText, baseline.autoCompactWindow)"), false);
});
