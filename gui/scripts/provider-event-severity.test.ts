/**
 * Provider recent-event severity contract.
 *
 * The listener owns event severity: `internal/provideractivity` records `info`, `warn`
 * or `error`, and `internal/server/providers_workspace.go` forwards that field on
 * `/api/providers/workspace`. The dashboard only decides how the field is presented.
 * One mapping answers that for every recent-event list, so the provider Overview, the
 * Management credential activity block and the fleet Overview cannot disagree.
 *
 * Informational events stay neutral. The listener never marks an activity event
 * healthy, so a row may not be painted green merely because it exists.
 */
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import {
  providerEventSeverity,
  providerEventSeverityWordKey,
} from "../src/provider-workspace/access-presentation.ts";

const guiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

function source(...segments: string[]): string {
  return readFileSync(path.join(guiRoot, ...segments), "utf8");
}

/** The declaration body of one selector, so a rule is read as written. */
function rule(css: string, selector: string): string {
  const at = css.indexOf(selector);
  assert.ok(at >= 0, `missing CSS rule: ${selector}`);
  const open = css.indexOf("{", at);
  assert.ok(open > at, `unterminated CSS rule: ${selector}`);
  const end = css.indexOf("}", open);
  assert.ok(end > open, `unterminated CSS rule: ${selector}`);
  return css.slice(at, end);
}

const providers = source("src", "styles-providers.css");
const eventList = source("src", "components", "provider-workspace", "ProviderEvents.tsx");
const overview = source("src", "components", "provider-workspace", "ProviderOverview.tsx");
const access = source("src", "components", "provider-workspace", "ProviderAccess.tsx");
const fleet = source("src", "components", "provider-workspace", "ProviderOverviewDashboard.tsx");

test("an error event is red and a warning event is amber", () => {
  assert.deepEqual(providerEventSeverity("error"), { tone: "error", className: "is-error" });
  assert.deepEqual(providerEventSeverity("warn"), { tone: "warn", className: "is-warn" });
});

test("an informational event stays neutral instead of borrowing a health colour", () => {
  // `info` is the listener's third and ordinary severity, and no event severity means healthy.
  for (const severity of ["info", "", "ok", "healthy", "success", "INFO", "WARN", "trace"]) {
    assert.deepEqual(providerEventSeverity(severity), { tone: "off", className: null }, severity);
  }
  assert.deepEqual(providerEventSeverity(undefined), { tone: "off", className: null });
});

test("only the tones that carry meaning get an accessible word", () => {
  assert.equal(providerEventSeverityWordKey("warn"), "prov.event.warn");
  assert.equal(providerEventSeverityWordKey("error"), "prov.event.error");
});

test("one mixed list keeps one tone per row", () => {
  assert.deepEqual(
    ["info", "error", "warn", "info"].map(providerEventSeverity),
    [
      { tone: "off", className: null },
      { tone: "error", className: "is-error" },
      { tone: "warn", className: "is-warn" },
      { tone: "off", className: null },
    ],
  );
});

test("the event list paints amber and red, and keeps no informational green of its own", () => {
  assert.match(rule(providers, ".providers-events .is-warn"), /var\(--amber\)/);
  assert.match(rule(providers, ".providers-events .is-error"), /var\(--red\)/);
  assert.equal(
    /\.providers-events[^{]*\.is-ok/.test(providers),
    false,
    "an informational row must not own a green treatment",
  );
});

test("the shared list renders its cue from the listener severity field", () => {
  assert.match(eventList, /providerEventSeverity\(event\.severity\)/);
  assert.match(eventList, /className=\{severity\.className\}/);
  assert.match(eventList, /className="sr-only"/);
  assert.match(eventList, /providerEventSeverityWordKey\(severity\.tone\)/);
  // Severity is read from the field, never compared against a type id or a rendered label.
  assert.equal(
    /event\.(type|label)\s*(===|!==|==|!=)/.test(eventList),
    false,
    "the event list must not derive severity from a type id or a label",
  );
});

test("Overview and Management render one list, and the fleet board reads the same mapping", () => {
  assert.match(overview, /<ProviderEvents\b/);
  assert.match(access, /<ProviderEvents\b/);
  assert.match(fleet, /providerEventSeverity\(event\.severity\)/);
  assert.equal(
    /event\.severity === "error" \? "error"/.test(fleet),
    false,
    "the fleet board must not keep a second inline severity switch",
  );
});

test("the Overview health row no longer depends on an unrelated optional value", () => {
  assert.equal(overview.includes("event.value"), false);
  assert.equal(overview.includes("is-ok"), false);
});
