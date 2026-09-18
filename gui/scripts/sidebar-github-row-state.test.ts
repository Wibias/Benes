/**
 * The sidebar GitHub/update row's interaction and projection.
 *
 * The row component is markup plus wiring; everything with a decision in it lives in
 * `sidebar-github-row-state.ts`, so it is asserted here directly.
 */
import assert from "node:assert/strict";
import test from "node:test";

import type { TFn } from "../src/i18n/shared.ts";
import {
  IDLE_STAR_INTERACTION,
  githubRowView,
  starInteraction,
  starOrbLabel,
  updateOrbLabel,
  type GithubRowInput,
} from "../src/components/sidebar-github-row-state.ts";

function translate(key: string, vars?: Record<string, string | number>): string {
  return vars ? `${key}(${Object.values(vars).join(",")})` : key;
}
const t = translate as unknown as TFn;

const FALLBACK = "https://github.com/Wibias/Benes";

function input(overrides: Partial<GithubRowInput> = {}): GithubRowInput {
  return {
    starStatus: null,
    updateBadge: null,
    interaction: IDLE_STAR_INTERACTION,
    fallbackRepositoryUrl: FALLBACK,
    ...overrides,
  };
}

test("the idle interaction is not busy and holds no override", () => {
  assert.deepEqual(IDLE_STAR_INTERACTION, { busy: false, override: null });
});

test("starting a request only raises the pending flag", () => {
  const busy = starInteraction(IDLE_STAR_INTERACTION, { kind: "request-started" });
  assert.deepEqual(busy, { busy: true, override: null });
});

test("a response event carries data and never clears the pending flag", () => {
  const busy = starInteraction(IDLE_STAR_INTERACTION, { kind: "request-started" });
  const decided = starInteraction(busy, {
    kind: "response-decided",
    override: { state: "starred", basedOn: null },
  });
  assert.deepEqual(decided, { busy: true, override: { state: "starred", basedOn: null } });
});

test("a response with nothing to record leaves the interaction as it was", () => {
  const busy = starInteraction(IDLE_STAR_INTERACTION, { kind: "request-started" });
  assert.deepEqual(starInteraction(busy, { kind: "response-decided", override: null }), busy);
});

test("only settling clears the pending flag, and it keeps the override", () => {
  const override = { state: "starred" as const, basedOn: "not-starred" as const };
  const state = starInteraction(
    starInteraction(IDLE_STAR_INTERACTION, { kind: "request-started" }),
    { kind: "response-decided", override },
  );
  assert.deepEqual(starInteraction(state, { kind: "request-settled" }), { busy: false, override });
});

test("a late settle for an old request cannot resurrect a cleared override", () => {
  const override = { state: "starred" as const, basedOn: null };
  const settled = starInteraction({ busy: true, override }, { kind: "request-settled" });
  assert.deepEqual(settled, { busy: false, override });
  assert.deepEqual(starInteraction(settled, { kind: "request-settled" }), settled);
});

test("with no reading at all the row shows the repository fallback and no update", () => {
  const view = githubRowView(input());
  assert.equal(view.repositoryUrl, FALLBACK);
  assert.equal(view.polledStarState, null);
  assert.equal(view.starState, "not-starred");
  assert.equal(view.starred, false);
  assert.equal(view.starDisabled, false);
  assert.equal(view.updateAvailable, false);
  assert.equal(view.latestVersion, null);
});

test("the endpoint's own repository url wins over the fallback", () => {
  const view = githubRowView(input({ starStatus: { state: "starred", url: "https://example.test/repo" } }));
  assert.equal(view.repositoryUrl, "https://example.test/repo");
  assert.equal(view.starred, true);
});

test("an override based on the polled reading replaces it", () => {
  const view = githubRowView(input({
    starStatus: { state: "not-starred" },
    interaction: { busy: false, override: { state: "starred", basedOn: "not-starred" } },
  }));
  assert.equal(view.polledStarState, "not-starred");
  assert.equal(view.starState, "starred");
  assert.equal(view.starred, true);
  assert.equal(view.starDisabled, true);
});

test("an override based on a stale reading is ignored", () => {
  const view = githubRowView(input({
    starStatus: { state: "unauthenticated" },
    interaction: { busy: false, override: { state: "starred", basedOn: "not-starred" } },
  }));
  assert.equal(view.starState, "unauthenticated");
  assert.equal(view.starred, false);
});

test("a mid-request row disables the star orb without claiming the star", () => {
  const view = githubRowView(input({
    starStatus: { state: "not-starred" },
    interaction: { busy: true, override: null },
  }));
  assert.equal(view.starDisabled, true);
  assert.equal(view.starred, false);
});

test("the update reading drives both the emphasis and the version", () => {
  const available = githubRowView(input({ updateBadge: { updateAvailable: true, latestVersion: "2.0.0" } }));
  assert.equal(available.updateAvailable, true);
  assert.equal(available.latestVersion, "2.0.0");
  const quiet = githubRowView(input({ updateBadge: { updateAvailable: false, latestVersion: "2.0.0" } }));
  assert.equal(quiet.updateAvailable, false);
  assert.equal(quiet.latestVersion, "2.0.0");
});

test("the star orb names each state it can be in", () => {
  const starred = githubRowView(input({ starStatus: { state: "starred" } }));
  assert.equal(starOrbLabel(starred, t), "sidebar.starred");
  const unauthenticated = githubRowView(input({ starStatus: { state: "unauthenticated" } }));
  assert.equal(starOrbLabel(unauthenticated, t), "sidebar.starUnauthenticated");
  const plain = githubRowView(input({ starStatus: { state: "not-starred" } }));
  assert.equal(starOrbLabel(plain, t), "sidebar.star");
});

test("the update orb names the version only when there is one", () => {
  const available = githubRowView(input({ updateBadge: { updateAvailable: true, latestVersion: "2.0.0" } }));
  assert.equal(updateOrbLabel(available, t), "sidebar.updateAvailable(2.0.0)");
  const unknownVersion = githubRowView(input({ updateBadge: { updateAvailable: true } }));
  assert.equal(updateOrbLabel(unknownVersion, t), "sidebar.checkUpdate");
  const quiet = githubRowView(input({ updateBadge: { updateAvailable: false } }));
  assert.equal(updateOrbLabel(quiet, t), "sidebar.checkUpdate");
});
