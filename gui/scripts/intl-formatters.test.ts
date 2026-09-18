import assert from "node:assert/strict";
import test from "node:test";

import {
  cachedNumberFormat,
  formatCreditDate,
  formatCreditDateTime,
  formatEstimatedUsdValue,
} from "../src/intl-formatters.ts";

test("invalid dates and costs become an em dash", () => {
  assert.equal(formatCreditDate("not-a-date"), "\u2014");
  assert.equal(formatCreditDateTime("nope"), "\u2014");
  assert.equal(formatEstimatedUsdValue(Number.NaN), "\u2014");
  assert.equal(formatEstimatedUsdValue(-1), "\u2014");
});

test("credit dates and USD estimates format through cached Intl", () => {
  const date = formatCreditDate("2026-01-15T00:00:00.000Z", "en-US");
  assert.match(date, /Jan/);
  assert.match(date, /15/);
  assert.match(date, /2026/);
  const dateTime = formatCreditDateTime("2026-01-15T13:04:00.000Z", "en-US");
  assert.match(dateTime, /Jan/);
  const usd = formatEstimatedUsdValue(1.23456, "en-US");
  assert.match(usd, /^~/);
  assert.match(usd, /1\.2346/);
  const first = cachedNumberFormat("en-US", { maximumFractionDigits: 1 });
  const second = cachedNumberFormat("en-US", { maximumFractionDigits: 1 });
  assert.equal(first, second);
});
