# Dashboard rules (`gui/`)

Parent file: `/AGENTS.md`. This folder is the Vite/React control surface served as `gui/dist`. It is not the Go data plane.

## Scope

- Edit source under `gui/src`, `gui/public`, and the dashboard configs. Never hand-edit `gui/dist`.
- Follow the existing component, routing, styling, and `/api/*` shapes. A new abstraction needs a reason that the current pattern cannot cover.
- Dashboard state must stay consistent with the management API (`/api/*`, `/healthz`, `/benes-session`) and with the provider/account model the listener already exposes.
- `gui/public/provider-icons/opencode.svg` is the third-party OpenCode mark (brand `#211E1E`). Do not replace it with a Benes logo. Do not invent a second mark file for that client.

## Visible copy

Hardcoded English or German in `src/pages`, `src/components`, `src/App.tsx`, or `src/ui.tsx` is a defect, even if it “fixes” a bad translation.

Every user-facing string belongs in the English catalogue. Translated locales store only values that differ from English:

1. Add the key to `src/i18n/en.ts` (`TKey` lives there).
2. If the translated text differs from English, add an override in `src/i18n/locale-overrides.ts`. Missing overrides fall back to English at runtime. `npm run lint:i18n` checks liveness and extra override keys. A new language also needs a profile in `src/i18n/locale-registry.ts`.
3. Render with `useT()` / `t("key")`. For `{cmd}` chips use `<Trans k="key" cmd="..." />`.

Leave these untranslated (allowlist: `.eslint/i18n-allowlist.ts`):

- Vendor and product names (OpenAI, Anthropic, Codex, OpenCode, GitHub).
- Catalog model ids shown as data (`gpt-4o`, `deepseek-v4-flash-free`), not labels such as “Default model”.
- Machine text: CLI samples (`curl …`, `export VAR=…`, `benes claude`), anything inside `<pre>` / `<code>`, HTTP headers, env names, protocol dumps (`model=…`, `thinking`), units next to numbers (`ms`, `k`, `1M`, cache `c`/`w`), URLs, adapter ids (`oauth`, `passthrough`, npm channels).
- Code comments, including `#` in shell samples. Do not delete comments to silence the i18n linter.

If the linter flags technical text, extend the allowlist or wrap it in `<pre>`/`<code>`. Do not invent a translation key for a command line.

After copy or locale-key changes: `npm run lint:i18n`. That command also rejects unused canonical keys, extra locale overrides, analysis errors, and unbounded translation calls. Do not make `scripts/sync-locale-keys.ts` delete overrides or copy English into translated resources.

## Implementation

Keyboard use, names, focus, semantic controls, and readable validation errors stay. Do not add a dependency for something the stack already does, or that a few local lines would do. New packages need security review. User-visible dashboard behaviour updates `docs/`.

## Checks

During a scoped change, run the smallest test that covers the behaviour. If copy or keys changed, run `npm run lint:i18n`. If `benes dev` is serving **23200**, run `npm run check:vite` after source edits — `tsc` will not see an empty Vite transform. Run `npm run build` once before calling the GUI change done. Stop there unless a failure or a shared-dependency change forces a wider run.

Before a review-ready PR, or when the user asks for full GUI validation:

```
cd gui
npm run lint
npm run build
```

After copy or locale work, also:

```
cd gui
npm run lint:i18n
```

Do not rerun a passing check on unchanged code unless a later edit can invalidate what that check proved.
