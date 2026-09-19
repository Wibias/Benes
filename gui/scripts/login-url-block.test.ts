import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { describe, it } from "node:test";

const source = readFileSync(new URL("../src/components/login-url-block.tsx", import.meta.url), "utf8");

describe("OAuth manual-open link boundary", () => {
  it("does not bind the raw authorization value directly to href", () => {
    assert.doesNotMatch(source, /<ManualOpenLink\s+href=\{url\}/);
    assert.match(source, /safeOAuthManualHref/);
    assert.match(source, /manualHref\s*&&/);
  });
});
