import assert from "node:assert/strict";
import test from "node:test";

import { compareStructuralBaseline, summarizeStructuralDiagnostics } from "./check-structural-baseline.ts";

function diagnostic(path, code, message) {
  return {
    code,
    filename: path,
    message,
    severity: "warning",
    labels: [{ span: { line: 10, column: 1, offset: 0, length: 1 } }],
  };
}

function approved(path, code, message, count = 1) {
  return { path, code, message, count };
}

test("non-structural diagnostics are ignored", () => {
  const actual = summarizeStructuralDiagnostics([
    { code: "eslint(eqeqeq)", filename: "src/a.ts", message: "other" },
  ]);
  assert.deepEqual(actual, []);
});

test("unchanged approved legacy complexity and depth pass", () => {
  const complexity = "function `legacy` has a complexity of 20. Maximum allowed is 15.";
  const depth = "Blocks are nested too deeply (5). Maximum allowed is 4.";
  const actual = summarizeStructuralDiagnostics([
    diagnostic("src/a.ts", "eslint(complexity)", complexity),
    diagnostic("src/b.ts", "eslint(max-depth)", depth),
  ]);
  assert.deepEqual(compareStructuralBaseline(actual, [
    approved("src/a.ts", "eslint(complexity)", complexity),
    approved("src/b.ts", "eslint(max-depth)", depth),
  ]), []);
});

test("a new structural violation fails", () => {
  const message = "function `newWork` has a complexity of 16. Maximum allowed is 15.";
  const actual = summarizeStructuralDiagnostics([
    diagnostic("src/new.ts", "eslint(complexity)", message),
  ]);
  const errors = compareStructuralBaseline(actual, []);
  assert.equal(errors.length, 1);
  assert.match(errors[0], /unapproved structural violation/i);
});

test("increasing existing complexity fails and leaves the old baseline stale", () => {
  const oldMessage = "function `legacy` has a complexity of 20. Maximum allowed is 15.";
  const newMessage = "function `legacy` has a complexity of 21. Maximum allowed is 15.";
  const actual = summarizeStructuralDiagnostics([
    diagnostic("src/a.ts", "eslint(complexity)", newMessage),
  ]);
  const errors = compareStructuralBaseline(actual, [
    approved("src/a.ts", "eslint(complexity)", oldMessage),
  ]);
  assert.equal(errors.length, 2);
  assert.ok(errors.some(error => /unapproved structural violation/i.test(error)));
  assert.ok(errors.some(error => /stale structural baseline/i.test(error)));
});

test("deeper nesting fails and leaves the old depth baseline stale", () => {
  const oldMessage = "Blocks are nested too deeply (5). Maximum allowed is 4.";
  const newMessage = "Blocks are nested too deeply (6). Maximum allowed is 4.";
  const actual = summarizeStructuralDiagnostics([
    diagnostic("src/a.ts", "eslint(max-depth)", newMessage),
  ]);
  const errors = compareStructuralBaseline(actual, [
    approved("src/a.ts", "eslint(max-depth)", oldMessage),
  ]);
  assert.equal(errors.length, 2);
  assert.ok(errors.some(error => /unapproved structural violation/i.test(error)));
  assert.ok(errors.some(error => /stale structural baseline/i.test(error)));
});

test("reducing structural debt forces the baseline to shrink", () => {
  const message = "function `legacy` has a complexity of 20. Maximum allowed is 15.";
  const errors = compareStructuralBaseline([], [
    approved("src/a.ts", "eslint(complexity)", message),
  ]);
  assert.equal(errors.length, 1);
  assert.match(errors[0], /stale structural baseline/i);
});

test("moving a violation without changing its diagnostic passes", () => {
  const message = "Blocks are nested too deeply (5). Maximum allowed is 4.";
  const moved = diagnostic("src/a.ts", "eslint(max-depth)", message);
  moved.labels[0].span.line = 400;
  const actual = summarizeStructuralDiagnostics([moved]);
  assert.deepEqual(compareStructuralBaseline(actual, [
    approved("src/a.ts", "eslint(max-depth)", message),
  ]), []);
});

test("duplicating an identical anonymous structural violation fails multiplicity", () => {
  const message = "async function has a complexity of 20. Maximum allowed is 15.";
  const actual = summarizeStructuralDiagnostics([
    diagnostic("src/a.ts", "eslint(complexity)", message),
    diagnostic("src/a.ts", "eslint(complexity)", message),
  ]);
  const errors = compareStructuralBaseline(actual, [
    approved("src/a.ts", "eslint(complexity)", message),
  ]);
  assert.equal(errors.length, 1);
  assert.match(errors[0], /count 2.*approved 1/i);
});
