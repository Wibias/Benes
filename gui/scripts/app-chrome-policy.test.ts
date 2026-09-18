/**
 * Chrome policy contracts.
 *
 * The hook itself is not driven here — this repository has no arbitrary-hook renderer — so
 * the point of extracting `app-chrome-policy.ts` is that every decision the hook makes is
 * answered by one of these functions and can be asserted without a DOM.
 */
import assert from "node:assert/strict";
import test from "node:test";

import {
  DESKTOP_LAYOUT_QUERY,
  DRAWER_DISMISSAL_HISTORY_EVENT,
  DRAWER_DISMISSAL_ROUTE_EVENT,
  DRAWER_FOCUS_DELAY_MS,
  THEME_ATTRIBUTE,
  THEME_STORAGE_KEY,
  decodeStoredTheme,
  drawerFocusTarget,
  stopCommandPlan,
  themeApplication,
  type AppTheme,
} from "../src/app-chrome-policy.ts";

test("the chrome reads and writes the keys the stylesheet and storage already use", () => {
  assert.equal(THEME_STORAGE_KEY, "benes-theme");
  assert.equal(THEME_ATTRIBUTE, "data-theme");
  assert.equal(DESKTOP_LAYOUT_QUERY, "(min-width: 761px)");
  assert.equal(DRAWER_FOCUS_DELAY_MS, 200);
});

test("only the two explicit theme choices decode to themselves", () => {
  assert.equal(decodeStoredTheme("light"), "light");
  assert.equal(decodeStoredTheme("dark"), "dark");
});

test("an absent or unreadable stored theme falls back to the system default", () => {
  const fallbacks: ReadonlyArray<string | null | undefined> = [
    null,
    undefined,
    "",
    " ",
    "system",
    "Light",
    "DARK",
    "auto",
    "0",
  ];
  for (const stored of fallbacks) {
    assert.equal(decodeStoredTheme(stored), "system", JSON.stringify(stored));
  }
});

test("an explicit theme is written to the document and to storage", () => {
  for (const theme of ["light", "dark"] as const) {
    assert.deepEqual(themeApplication(theme), { attribute: theme, persist: true }, theme);
  }
});

test("the system theme removes the attribute and the stored override", () => {
  assert.deepEqual(themeApplication("system"), { attribute: null, persist: false });
});

test("every theme choice has an application plan", () => {
  const themes: readonly AppTheme[] = ["light", "dark", "system"];
  for (const theme of themes) {
    const plan = themeApplication(theme);
    if (theme === "system") {
      assert.equal(plan.attribute, null);
      continue;
    }
    assert.equal(plan.attribute, theme);
  }
});

test("a route change dismisses an open drawer", () => {
  assert.equal(DRAWER_DISMISSAL_ROUTE_EVENT, "hashchange");
  assert.equal(DRAWER_DISMISSAL_HISTORY_EVENT, "popstate");
  // Escape is a key, not a route event, so it must not appear among the dismissal reasons.
  assert.notEqual(DRAWER_DISMISSAL_ROUTE_EVENT, "keydown");
  assert.notEqual(DRAWER_DISMISSAL_HISTORY_EVENT, "keydown");
});

test("focus moves into the drawer on open and back to the menu button on close", () => {
  assert.equal(drawerFocusTarget(false, true), "sidebar");
  assert.equal(drawerFocusTarget(true, false), "menu");
});

test("a render that never opened the drawer leaves focus alone", () => {
  assert.equal(drawerFocusTarget(false, false), "none");
});

test("an accepted stop keeps the button in its stopping state", () => {
  assert.deepEqual(stopCommandPlan({ accepted: true }), { releaseButton: false, alert: null });
});

test("a rejected stop releases the button and carries the server's reason", () => {
  assert.deepEqual(
    stopCommandPlan({ accepted: false, message: "restore me" }),
    { releaseButton: true, alert: "restore me" },
  );
  assert.deepEqual(
    stopCommandPlan({ accepted: false, message: "Failed to stop proxy (HTTP 500)." }),
    { releaseButton: true, alert: "Failed to stop proxy (HTTP 500)." },
  );
});

test("a rejection without a reason still releases the button", () => {
  const plan = stopCommandPlan({ accepted: false, message: "" });
  assert.equal(plan.releaseButton, true);
  assert.notEqual(plan.alert, null);
});
