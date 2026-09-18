/**
 * Provider status colour semantics, shared by the Overview and Management surfaces.
 *
 * One semantic state keeps one colour: green for ready/connected active state,
 * amber for attention/degraded/pending, red for a provider that cannot serve,
 * and the neutral muted token for inactive/disabled/unknown. Colour is the
 * behaviour on these surfaces, so the per-surface assertions read the
 * stylesheets; the severity mapping itself is asserted through the shared
 * mapper the Overview calls.
 */
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import { overviewIssueTone, overviewIssueToneClass } from "../src/provider-workspace/access-presentation.ts";
import { detailStatusPill, detailStatusPillKind } from "../src/provider-workspace/connection-test.ts";

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
const rail = source("src", "styles", "provider-workspace-rail.css");
const settings = source("src", "styles", "provider-workspace-settings.css");
const dashboard = source("src", "components", "provider-workspace", "ProviderOverviewDashboard.tsx");

test("fleet issue severity keeps the listener band and stays quiet when unknown", () => {
  // The listener sends `error` only for provider_health_failure and `warn` for the
  // recoverable codes, so the row tone follows that field rather than a code list.
  assert.equal(overviewIssueTone("error"), "error");
  assert.equal(overviewIssueTone("warn"), "warn");
  assert.equal(overviewIssueTone("info"), "off");
  assert.equal(overviewIssueTone(undefined), "off");
  assert.equal(overviewIssueToneClass("error"), "providers-overview-issue-status is-error");
  assert.equal(overviewIssueToneClass("warn"), "providers-overview-issue-status is-warn");
});

test("the Overview attention row paints the issue severity instead of one attention amber", () => {
  assert.match(dashboard, /overviewIssueToneClass\(issue\.severity\)/);
  assert.equal(dashboard.includes('className="providers-overview-issue-status"'), false);
  assert.match(rule(providers, ".providers-overview-issue-status.is-error"), /var\(--red\)/);
  assert.match(rule(providers, ".providers-overview-issue-status.is-warn"), /var\(--amber\)/);
  assert.match(rule(providers, ".providers-overview-issue-status.is-off"), /var\(--muted\)/);
});

test("ready, attention and inactive keep one colour per surface", () => {
  const labels = { connected: "connected", disabled: "disabled", attention: "attention" };
  // Management: the rail dot and the detail pill agree with each other.
  assert.equal(detailStatusPill(detailStatusPillKind("ready"), labels).cls, "providers-pill--on");
  assert.equal(detailStatusPill(detailStatusPillKind("needs-setup"), labels).cls, "providers-pill--amber");
  assert.equal(detailStatusPill(detailStatusPillKind("disabled"), labels).cls, "providers-pill--muted");
  assert.match(rule(rail, ".providers-workspace-rail-status--active"), /background: var\(--green\)/);
  assert.match(rule(rail, ".providers-workspace-rail-status--warning"), /background: var\(--amber\)/);
  assert.match(rule(rail, ".providers-workspace-rail-status--inactive"), /background: var\(--muted\)/);
  assert.match(rule(providers, ".providers-pill--on"), /var\(--green\)/);
  assert.match(rule(providers, ".providers-pill--amber"), /var\(--amber\)/);
  assert.match(rule(providers, ".providers-pill--muted"), /var\(--muted\)/);
  // The detail title row must not set its own colour: it out-specifies the pill
  // tone classes, so every pill would render neutral while the rail dot showed state.
  assert.equal(
    /\.providers-detail-title-row \.providers-status\s*\{[^}]*color:/.test(providers),
    false,
    "the detail title row must leave the status pill colour to the tone classes",
  );
  // Management: credential dots. An account that needs reauth is attention, not inactive.
  assert.match(rule(providers, ".providers-access .providers-pill-dot.is-ok"), /background: var\(--green\)/);
  assert.match(rule(providers, ".providers-access .providers-pill-dot.is-warn"), /background: var\(--amber\)/);
  assert.match(rule(providers, ".providers-access .providers-pill-dot.is-off"), /background: var\(--muted\)/);
  assert.match(rule(settings, ".pwi-auth-dot--ok"), /background: var\(--green\)/);
  assert.match(rule(settings, ".pwi-auth-dot--warn"), /background: var\(--amber\)/);
  assert.match(rule(settings, ".pwi-auth-dot--off"), /background: var\(--muted\)/);
});

test("status colours stay off tier badges and off undefined design tokens", () => {
  // Free/Local describe tier and location, not health: the rail row's status colour
  // is the rail dot. A badge rule here would reintroduce a second meaning for green.
  for (const badge of [".pwi-rail-badge--free", ".pwi-rail-badge--local"]) {
    assert.equal(rail.includes(badge), false, `${badge} must not carry a health colour`);
  }
  const sheets: [string, string][] = [
    ["styles-providers", providers],
    ["provider-workspace-rail", rail],
    ["provider-workspace-settings", settings],
  ];
  for (const [name, css] of sheets) {
    assert.equal(/var\(--(danger|warn)\b/.test(css), false, `${name} must use the canonical --red/--amber tokens`);
  }
});
