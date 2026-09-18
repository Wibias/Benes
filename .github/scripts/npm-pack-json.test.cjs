"use strict";

const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const { normalizePackEntry } = require("./npm-pack-json.cjs");

describe("npm pack JSON normalization", () => {
  const entry = {
    name: "benes",
    filename: "benes-2.18.0.tgz",
    files: [{ path: "gui/dist/index.html" }],
  };

  it("accepts npm <=11 array output", () => {
    assert.deepEqual(normalizePackEntry([entry], "benes"), entry);
  });

  it("accepts npm 12 package-keyed object output", () => {
    assert.deepEqual(normalizePackEntry({ benes: entry }, "benes"), entry);
  });

  it("rejects ambiguous or malformed output", () => {
    assert.equal(normalizePackEntry({}, "benes"), null);
    assert.equal(normalizePackEntry({ other: entry, another: entry }, "benes"), null);
    assert.equal(normalizePackEntry(null, "benes"), null);
  });
});
