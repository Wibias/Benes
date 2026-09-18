import assert from "node:assert/strict";
import test from "node:test";

import {
  baseUrlForChoice,
  matchChoiceId,
  resolvedBaseUrlForChoice,
  type BaseUrlChoice,
} from "../src/base-url-choice.ts";

const choices: BaseUrlChoice[] = [
  { id: "openai", label: "OpenAI", baseUrl: "https://api.openai.com/v1" },
  { id: "custom", label: "Custom" },
];

test("trailing slashes do not change matching", () => {
  assert.equal(matchChoiceId(choices, "https://api.openai.com/v1/"), "openai");
  assert.equal(matchChoiceId(choices, "  https://api.openai.com/v1  "), "openai");
});

test("a known preset writes its configured URL", () => {
  assert.equal(baseUrlForChoice(choices, "openai", "https://example.test"), "https://api.openai.com/v1");
});

test("switching from a known preset URL to custom clears the field", () => {
  assert.equal(baseUrlForChoice(choices, "custom", "https://api.openai.com/v1/"), "");
});

test("genuine custom text survives switching to custom", () => {
  assert.equal(baseUrlForChoice(choices, "custom", "https://proxy.internal/v1"), "https://proxy.internal/v1");
});

test("unknown choice ids keep the previous text", () => {
  assert.equal(baseUrlForChoice(choices, "missing", "https://keep.me"), "https://keep.me");
  assert.equal(baseUrlForChoice(undefined, "custom", "https://keep.me"), "https://keep.me");
});

test("matchChoiceId prefers custom when present, otherwise the first choice", () => {
  assert.equal(matchChoiceId(choices, "https://other.example/v1"), "custom");
  assert.equal(matchChoiceId([{ id: "openai", label: "OpenAI", baseUrl: "https://api.openai.com/v1" }], "https://x"), "openai");
  assert.equal(matchChoiceId(undefined, "https://x"), "custom");
  assert.equal(matchChoiceId([], "https://x"), "custom");
});

test("duplicate normalised preset URLs keep the first matching choice", () => {
  const duplicates: BaseUrlChoice[] = [
    { id: "first", label: "First", baseUrl: "https://same.example/v1/" },
    { id: "second", label: "Second", baseUrl: "https://same.example/v1" },
    { id: "custom", label: "Custom" },
  ];
  assert.equal(matchChoiceId(duplicates, "https://same.example/v1"), "first");
  assert.equal(matchChoiceId(duplicates, "https://same.example/v1/"), "first");
  assert.equal(baseUrlForChoice(duplicates, "custom", "https://same.example/v1/"), "");
});

test("resolved URLs trim presets and custom text", () => {
  assert.equal(resolvedBaseUrlForChoice(choices, "openai", " ignored "), "https://api.openai.com/v1");
  assert.equal(resolvedBaseUrlForChoice(choices, "custom", "  https://proxy.internal/v1  "), "https://proxy.internal/v1");
  assert.equal(resolvedBaseUrlForChoice(undefined, "custom", "  https://x  "), "https://x");
});
