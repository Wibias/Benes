"use strict";

const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const { normalizePackEntry } = require("./npm-pack-json.cjs");

describe("npm pack JSON normalization", () => {
  const entry = {
    name: "@wibias/benes",
    filename: "wibias-benes-0.1.0-preview.0.tgz",
    files: [{ path: "gui/dist/index.html" }],
  };

  it("accepts npm <=11 array output", () => {
    assert.deepEqual(normalizePackEntry([entry], "@wibias/benes"), entry);
  });

  it("accepts npm 12 package-keyed object output", () => {
    assert.deepEqual(normalizePackEntry({ "@wibias/benes": entry }, "@wibias/benes"), entry);
  });

  it("rejects ambiguous or malformed output", () => {
    assert.equal(normalizePackEntry({}, "@wibias/benes"), null);
    assert.equal(normalizePackEntry({ other: entry, another: entry }, "@wibias/benes"), null);
    assert.equal(normalizePackEntry(null, "@wibias/benes"), null);
  });
});
