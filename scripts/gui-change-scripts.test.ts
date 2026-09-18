import assert from "node:assert/strict";
import { describe, test } from "node:test";
import { decide, isGuiPath } from "./gui-if-changed.ts";

describe("isGuiPath", () => {
  test("matches the gui directory and anything under it", () => {
    assert.equal(isGuiPath("gui"), true);
    assert.equal(isGuiPath("gui/src/App.tsx"), true);
    assert.equal(isGuiPath("gui/index.html"), true);
  });

  test("does not match names that only share a prefix", () => {
    assert.equal(isGuiPath("guidance/notes.md"), false);
    assert.equal(isGuiPath("guide.ts"), false);
    assert.equal(isGuiPath("cmd/benes/main.go"), false);
  });
});

describe("lint decision", () => {
  test("runs when the comparison base is missing", () => {
    assert.equal(decide("lint", null), "run");
  });

  test("skips a push that never touched gui/", () => {
    assert.equal(decide("lint", ["internal/server/foo.go", "README.md"]), "skip");
  });

  test("runs when a gui source file is in the range", () => {
    assert.equal(decide("lint", ["gui/src/App.tsx"]), "run");
  });
});

describe("build decision", () => {
  test("skips when ORIG_HEAD is unavailable", () => {
    assert.equal(decide("build", null), "skip");
  });

  test("skips a merge that never touched gui/", () => {
    assert.equal(decide("build", ["cmd/benes/main.go"]), "skip");
  });

  test("runs when gui/ changed in the merge range", () => {
    assert.equal(decide("build", ["gui/index.html", "internal/server/x.go"]), "run");
  });
});
