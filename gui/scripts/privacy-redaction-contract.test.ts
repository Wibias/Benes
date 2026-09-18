import assert from "node:assert/strict";
import test from "node:test";

import {
  ACCOUNT_PLACEHOLDER,
  displayAccountId,
  maskAccountId,
  maskEmailAddress,
} from "../src/lib/privacy.ts";

/**
 * Every fixture below is synthetic. The contract under test is that no helper can hand a
 * caller back the value it was given, so the assertions check both the exact output and
 * the absence of each input's identifying characters.
 */

const SYNTHETIC_ACCOUNT_ID = "acct-9f31c7d2-4b8e-4a10-90cc-7d21ab34cd56";

test("an absent account id has no masked form", () => {
  for (const absent of [null, undefined, "", "   ", "\t\n "]) {
    assert.equal(maskAccountId(absent), null, JSON.stringify(absent));
  }
});

test("a short account id is withheld whole", () => {
  for (const short of ["a", "ab", "abc", "abcd"]) {
    assert.equal(maskAccountId(short), ACCOUNT_PLACEHOLDER, short);
    assert.equal(maskAccountId(` ${short} `), ACCOUNT_PLACEHOLDER, short);
  }
});

test("a longer account id keeps its tail and nothing before it", () => {
  assert.equal(maskAccountId("abcde"), `${ACCOUNT_PLACEHOLDER}bcde`);
  assert.equal(maskAccountId("  abcde  "), `${ACCOUNT_PLACEHOLDER}bcde`);
  assert.equal(maskAccountId(SYNTHETIC_ACCOUNT_ID), `${ACCOUNT_PLACEHOLDER}cd56`);
});

test("a masked account id never reproduces a prefix of the input", () => {
  const masked = maskAccountId(SYNTHETIC_ACCOUNT_ID);
  assert.ok(masked);
  assert.equal(masked.includes("acct"), false);
  assert.equal(masked.includes("9f31"), false);
  assert.equal(masked.includes(SYNTHETIC_ACCOUNT_ID), false);
});

test("the display label falls back to the placeholder, never the raw id", () => {
  assert.equal(displayAccountId(SYNTHETIC_ACCOUNT_ID), `${ACCOUNT_PLACEHOLDER}cd56`);
  for (const unusable of [null, undefined, "", "   ", "xy"]) {
    assert.equal(displayAccountId(unusable), ACCOUNT_PLACEHOLDER, JSON.stringify(unusable));
  }
  assert.equal(displayAccountId(null).includes("acct"), false);
});

test("a well-formed login email keeps only its domain and one local character", () => {
  assert.equal(maskEmailAddress("ada@example.com"), "a***@example.com");
  assert.equal(maskEmailAddress("JK@Example.COM"), "J***@Example.COM");
  assert.equal(maskEmailAddress("  jorja@sub.example.test  "), "j***@sub.example.test");
});

test("a masked email never reproduces the local part", () => {
  const masked = maskEmailAddress("jordan.reed@corp.example.test");
  assert.equal(masked, "j***@corp.example.test");
  assert.equal(masked?.includes("ordan"), false);
  assert.equal(masked?.includes("reed"), false);
});

test("the identity-free login placeholder has no masked form", () => {
  assert.equal(maskEmailAddress("Codex App login"), null);
  assert.equal(maskEmailAddress("Codex App login "), null);
});

test("an unusable email candidate has no masked form", () => {
  const unusable = [
    null,
    undefined,
    "",
    "   ",
    "@example.com",
    "ada@",
    "ada@localhost",
    "example.com",
    "ada@example@",
  ];
  for (const candidate of unusable) {
    assert.equal(maskEmailAddress(candidate), null, JSON.stringify(candidate));
  }
});

test("a multi-at address masks against its final separator", () => {
  assert.equal(maskEmailAddress("ada@mail.example.test@relay.test"), "a***@relay.test");
});
