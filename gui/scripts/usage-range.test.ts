import assert from "node:assert/strict";
import test from "node:test";

import {
  customRangeError,
  exclusiveEndParam,
  usageSearchParams,
} from "../src/pages/usage-range.ts";

test("reversed custom range is invalid and is not remapped", () => {
  assert.equal(customRangeError("2026-08-22T12:00", "2026-08-20T12:00"), "reversed");
  const got = usageSearchParams("custom", "all", "2026-08-22T12:00", "2026-08-20T12:00", "UTC");
  assert.equal(got.ok, false);
  if (!got.ok) {
    assert.equal(got.error, "reversed");
  }
});

test("malformed timestamps fail closed", () => {
  assert.equal(customRangeError("nope", "2026-08-20"), "malformed");
  const got = usageSearchParams("custom", "all", "", "2026-08-20T12:00", "UTC");
  assert.equal(got.ok, false);
});

test("inclusive equal start and end is a one-minute window", () => {
  const got = usageSearchParams("custom", "all", "2026-08-20T14:30", "2026-08-20T14:30", "UTC");
  assert.equal(got.ok, true);
  if (!got.ok) {
    throw new Error("expected query");
  }
  const params = new URLSearchParams(got.query);
  assert.equal(params.get("start"), "2026-08-20T14:30");
  assert.equal(params.get("end"), "2026-08-20T14:31");
});

test("datetime-local inclusive end becomes exclusive next minute", () => {
  assert.equal(exclusiveEndParam("2026-08-20T14:59"), "2026-08-20T15:00");
  const got = usageSearchParams("custom", "all", "2026-08-20T14:30", "2026-08-20T14:59", "UTC");
  assert.equal(got.ok, true);
  if (!got.ok) {
    throw new Error("expected query");
  }
  const params = new URLSearchParams(got.query);
  assert.equal(params.get("start"), "2026-08-20T14:30");
  assert.equal(params.get("end"), "2026-08-20T15:00");
  assert.equal(params.get("tz"), "UTC");
});

test("date-only custom range keeps exclusive next-day end", () => {
  const got = usageSearchParams("custom", "all", "2026-08-20", "2026-08-21", "UTC");
  assert.equal(got.ok, true);
  if (!got.ok) {
    throw new Error("expected query");
  }
  const params = new URLSearchParams(got.query);
  assert.equal(params.get("start"), "2026-08-20");
  assert.equal(params.get("end"), "2026-08-22");
});
