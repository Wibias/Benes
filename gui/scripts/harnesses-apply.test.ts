import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import { harnessHeaderAction } from "../src/pages/harnesses/presentation.ts";

test("Apply is hidden unless the harness is installed, unapplied, and not in conflict", () => {
  assert.deepEqual(
    harnessHeaderAction({ installed: false, applied: false, issue: "none" }),
    { kind: "none" },
  );
  assert.deepEqual(
    harnessHeaderAction({ installed: true, applied: false, issue: "conflict" }),
    { kind: "none" },
  );
  assert.deepEqual(
    harnessHeaderAction({ installed: true, applied: false, issue: "none" }),
    { kind: "apply", enabled: true },
  );
  assert.deepEqual(
    harnessHeaderAction({ installed: true, applied: true, issue: "none" }),
    { kind: "disable", enabled: true },
  );
  assert.deepEqual(
    harnessHeaderAction({ installed: true, applied: true, issue: "conflict" }),
    { kind: "disable", enabled: false },
  );
});

test("Connect lives on the selected overview row, not a second picker modal", () => {
  const page = readFileSync(
    path.join(path.dirname(fileURLToPath(import.meta.url)), "../src/pages/harnesses/Harnesses.tsx"),
    "utf8",
  );
  assert.equal(page.includes("ConnectDialog"), false);
  assert.equal(page.includes("connectOpen"), false);
  assert.equal(page.includes("connectCandidates"), false);
  assert.equal(page.includes("harnesses.connectTitle"), false);
});
