# Routing profile icon catalog design

Date: 2026-09-06

## Purpose

Grow the Routing Profiles rail icon picker from a fixed 12-glyph set into a **curated Material Design Icons (MDI) catalog (~120–150 icons)** with **search**, loaded **lazily** when the picker opens. Persist the chosen kebab-case icon id on the profile (existing `icon` field). No new npm dependency.

## Decisions

| Decision | Choice |
| --- | --- |
| Catalog size | Curated ~120–150 Pictogrammers MDI paths (not full ~7k set) |
| Packaging | Hand-vendored path map in GUI source (approach A) |
| Load timing | Dynamic `import()` when the picker opens; idle rail stays small |
| Search | Case-insensitive substring match on icon id |
| Dependency | None (`@mdi/js` out of scope) |
| License | Apache 2.0 notice retained with the path module |

## Non-goals

- Full MDI catalog
- Adding `@mdi/js` or other icon packages
- Custom SVG upload
- Category tabs / tags beyond search
- Changing the Go API shape for `icon` (remains optional string)

## Current state

- `gui/src/pages/routing-profile-icon-data.ts` — 12 hardcoded ids + paths; `normalizeRoutingProfileIconId` clamps unknowns to `file-document`
- `gui/src/pages/routing-profile-icons.tsx` — renders a path for a normalized id
- `gui/src/pages/routing-profiles-sections.tsx` — `RoutingProfileIconPicker` grid, no search, sync catalog
- Profile DTO / PUT already accept `icon` as a string

## Catalog module

- New lazy module, e.g. `gui/src/pages/routing-profile-icon-catalog.ts`, exporting:
  - `ROUTING_PROFILE_ICON_CATALOG: Readonly<Record<string, string>>` (id → SVG path `d`)
  - `ROUTING_PROFILE_ICON_CATALOG_IDS: readonly string[]` (stable alphabetical order for the grid)
- Keep a **small always-loaded** surface in `routing-profile-icon-data.ts`:
  - `ROUTING_PROFILE_DEFAULT_ICON`
  - default path (and optionally the original 12 for closed-rail paint before catalog loads)
  - `normalizeRoutingProfileIconId` / path lookup that:
    1. uses the tiny sync map when present
    2. after catalog load, resolves any curated id
    3. unknown → default
- Theme the curated set around routing / ops / agents / status / docs (expand the existing 12; no brand/logo marks).

## Lazy load behaviour

1. Closed rail button: render current icon from sync map if known; if the stored id is only in the lazy catalog, either wait until catalog has been loaded once in-session and cached, or show default until first open then re-render. Preferred: **module-level cache** after first successful `import()` so subsequent closed-rail rows use full paths without re-fetch.
2. Open picker → start `import()` of the catalog (once); show a compact loading affordance if still pending; on resolve, populate grid.
3. Failed import → muted error line (i18n); keep previous selection; allow close.

## Picker UI

- Same rail popover anchor as today (`routing-rail-icon-picker`).
- Top: search `<input>` (placeholder i18n), autofocus when opened.
- Below: scrollable icon grid; `role="listbox"` / `option`; selected marked `is-active`.
- Filter: `id.toLowerCase().includes(query.trim().toLowerCase())`.
- Empty filter result: muted “No icons match” (i18n in all locales).
- Click option → `onChange(id)`, close popover, clear search for next open.
- Outside pointerdown and Escape close the popover.
- Stop propagation on picker interactions so rail row selection does not fire.

## Persistence

- Draft / PUT continue to send kebab id strings (e.g. `rocket-launch`).
- Server stores opaque string; no server-side allowlist required for this slice.
- Display: unknown id → `file-document` glyph (current behaviour).

## i18n

Add keys (all locale modules + `en-base` / `en.ts` as required by GUI AGENTS):

- Search placeholder (e.g. `routing.form.iconSearch`)
- Empty results (`routing.form.iconSearchEmpty`)
- Load failure (`routing.form.iconCatalogFailed`) if shown

## Verification

- Focused GUI check: open picker once → catalog loads; search narrows grid; select persists on draft/rail; reopen uses cached catalog without blank flash.
- `npm run lint:i18n` after copy keys.
- Existing routing profile tests stay green; add a small unit test for id filter / normalize if cheap next to current routing GUI tests.

## Out of scope follow-ups

- Virtualized grid if catalog grows past ~200
- Full MDI or CDN-hosted packs
- Icon picker on overview-only (non-edit) rows
