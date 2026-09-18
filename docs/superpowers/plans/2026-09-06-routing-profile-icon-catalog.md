# Routing profile icon catalog Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expand the Routing Profiles rail icon picker to a curated ~120–150 MDI path catalog with search, lazy-loaded on first open.

**Architecture:** Keep a tiny sync map for the default glyph; dynamic-`import()` a vendored catalog module when the picker opens; cache the module for closed-rail renders. Filter the grid by kebab id substring. No new runtime dependency.

**Tech Stack:** React + Vite GUI (`gui/`), Pictogrammers MDI paths (Apache 2.0), existing i18n + `styles-routing.css`.

## Global Constraints

- Curated catalog only (~120–150), not full MDI
- No `@mdi/js` (or other icon) package left in `gui/package.json`
- Persist kebab-case `icon` string; unknown → `file-document`
- Every user-facing string in all locale modules; `npm run lint:i18n`
- Apache 2.0 notice retained with path data

---

## File map

| File | Role |
| --- | --- |
| `gui/src/pages/routing-profile-icon-data.ts` | Default id, tiny sync paths, normalize, path lookup + catalog cache helpers |
| `gui/src/pages/routing-profile-icon-catalog.ts` | Lazy chunk: full curated `Record<id, path>` + sorted ids |
| `gui/src/pages/routing-profile-icons.tsx` | SVG render via path lookup |
| `gui/src/pages/routing-profiles-sections.tsx` | Picker: search, lazy load, grid |
| `gui/src/styles-routing.css` | Search field + taller scrollable picker |
| `gui/src/i18n/en-base.mts` + locale `*.ts` | Search / empty / failed keys |
| `gui/scripts/models-routing-combos-policy.test.ts` (or sibling) | Filter + normalize unit checks |

---

### Task 1: Catalog data + path lookup API

**Files:**
- Create: `gui/src/pages/routing-profile-icon-catalog.ts`
- Modify: `gui/src/pages/routing-profile-icon-data.ts`
- Modify: `gui/src/pages/routing-profile-icons.tsx`
- Test: extend `gui/scripts/models-routing-combos-policy.test.ts` (or add `gui/scripts/routing-profile-icons.test.ts`)

**Interfaces:**
- Produces:
  - `export const ROUTING_PROFILE_DEFAULT_ICON = "file-document"`
  - `export function normalizeRoutingProfileIconId(value: string | null | undefined): string`
  - `export function routingProfileIconPath(id: string | null | undefined): string`
  - `export function ensureRoutingProfileIconCatalog(): Promise<{ ids: readonly string[]; paths: Readonly<Record<string, string>> }>`
  - Catalog module exports `ROUTING_PROFILE_ICON_CATALOG` and `ROUTING_PROFILE_ICON_CATALOG_IDS`

- [ ] **Step 1: Write failing tests for filter helper + normalize**

Add to a focused test file:

```ts
import assert from "node:assert/strict";
import test from "node:test";
import {
  filterRoutingProfileIconIds,
  normalizeRoutingProfileIconId,
  ROUTING_PROFILE_DEFAULT_ICON,
} from "../src/pages/routing-profile-icon-data.ts";

test("normalizeRoutingProfileIconId falls back for empty/unknown", () => {
  assert.equal(normalizeRoutingProfileIconId(""), ROUTING_PROFILE_DEFAULT_ICON);
  assert.equal(normalizeRoutingProfileIconId("not-an-icon"), ROUTING_PROFILE_DEFAULT_ICON);
  assert.equal(normalizeRoutingProfileIconId("rocket-launch"), "rocket-launch");
});

test("filterRoutingProfileIconIds matches substring case-insensitively", () => {
  const ids = ["rocket-launch", "file-document", "shield-check"];
  assert.deepEqual(filterRoutingProfileIconIds(ids, "ROCKET"), ["rocket-launch"]);
  assert.deepEqual(filterRoutingProfileIconIds(ids, "  "), ids);
});
```

- [ ] **Step 2: Run test — expect FAIL** (helpers missing / normalize rejects unknown curated ids)

Run: `cd gui && npx --yes tsx scripts/routing-profile-icons.test.ts`

- [ ] **Step 3: Implement sync data + lazy catalog**

1. Generate curated paths once via temporary `@mdi/js` (do not commit the dependency): unpack, map kebab ids → `mdiX` exports, write `routing-profile-icon-catalog.ts` with Apache notice + ~130 themed ids (ops/routing/agents/status/docs).
2. In `routing-profile-icon-data.ts`: keep default + original 12 in sync map; `normalize` accepts any non-empty string that is either in sync map **or** looks like a kebab id already stored (for display before catalog loads: prefer accepting known sync ids + after cache, catalog ids; for unknown never-seen ids still default). Spec: unknown → default. For stored curated ids not in sync map, keep the id string on the draft but paint default until catalog loads, then re-render — OR include all catalog ids in normalize after cache. Preferred: `normalize` returns trimmed id if non-empty and (in sync OR in cached catalog OR matches `/^[a-z0-9]+(-[a-z0-9]+)*$/` and will be validated on paint via path lookup falling back to default path). Simpler rule matching spec: **normalize returns trimmed id if present in sync or cached catalog, else default**; before catalog load, curated-only ids fall back to default glyph but draft keeps raw id in form state (picker `onChange` still stores catalog id). Rail display uses `routingProfileIconPath` which returns default path until catalog has that id.
3. `ensureRoutingProfileIconCatalog` dynamic-imports catalog, fills module cache, returns `{ ids, paths }`.
4. `filterRoutingProfileIconIds(ids, query)` as above.
5. `RoutingProfileIcon` uses `routingProfileIconPath(id)`.

- [ ] **Step 4: Run tests — PASS**

- [ ] **Step 5: Commit** (only if user asked)

---

### Task 2: Picker UI + CSS + i18n

**Files:**
- Modify: `gui/src/pages/routing-profiles-sections.tsx` (`RoutingProfileIconPicker`)
- Modify: `gui/src/styles-routing.css`
- Modify: `gui/src/i18n/en-base.mts` + `de.ts` `fr.ts` `ja.ts` `ko.ts` `ru.ts` `tr.ts` `zh.ts` `zh-TW.ts` (+ `en.ts` if keys are declared there)

**Interfaces:**
- Consumes: `ensureRoutingProfileIconCatalog`, `filterRoutingProfileIconIds`, `routingProfileIconPath` / `RoutingProfileIcon`

- [ ] **Step 1: Add i18n keys**

```
"routing.form.iconSearch": "Search icons…"
"routing.form.iconSearchEmpty": "No icons match"
"routing.form.iconCatalogFailed": "Could not load icons"
```

Same keys in every locale (English copy OK where translation pending is existing practice for some locales).

- [ ] **Step 2: Expand picker**

On open: `void ensureRoutingProfileIconCatalog().then(setCatalog).catch(setError)`; search input autofocus; filter ids; loading / error / empty states; Escape closes; keep stopPropagation.

- [ ] **Step 3: CSS**

Widen picker slightly; add `.routing-rail-icon-search` full-width 4px radius matching other search fields; grid max-height scroll.

- [ ] **Step 4: Verify**

`cd gui && npm run lint:i18n`  
`cd gui && npx --yes tsx scripts/routing-profile-icons.test.ts`  
Manual: open Create Profile → icon → search `shield` → select → rail updates.

---

## Spec coverage

- Curated lazy catalog — Task 1  
- Search — Task 2  
- i18n empty/fail — Task 2  
- No `@mdi/js` dep — Task 1 generation only  
- Unknown → default path — Task 1  
