# Dashboard (`gui/`)

React + Vite. Packaged installs open it with `benes gui`. The Go listener serves the built files from `gui/dist`.

## Two processes

The listener and Vite run separately while you edit the UI.

```bash
# repo root — data plane
go run ./cmd/benes start
```

```bash
# another terminal
cd gui
npm ci
npm run dev
```

`npm run dev:gui` from the repo root is the same Vite command. Point Vite at a live listener with `BENES_PROXY_TARGET=http://127.0.0.1:23100`. That proxies `/api`, `/healthz`, `/readyz`, `/v1`, and `/benes-session` so the browser does not hit CORS.

`benes dev` is the same loop with the flag wiring (default GUI port **23200**): `--proxy off` (default) talks to the running `benes start` listener on **23100**; `--proxy on` runs checkout proxy code against `~/.benes` providers without Codex inject or overwriting the main pid file.

`GET /` on the Go port only serves the dashboard when `gui/dist` exists. A fresh clone should use Vite until you build.

## Production assets

From the repo root:

```bash
npm run build:gui
```

That installs and builds this app, then copies the output into the package layout `benes gui` uses.

## Gates

Inside `gui/`:

```bash
npm run lint         # oxlint — merge gate (`GUI lint` in CI), includes oxlint-plugin-react-doctor
npm run lint:i18n    # after visible copy or locale-key changes
```

From the repo root: `npm run lint:gui`. `npm run setup:hooks` runs oxlint on pre-push only when `gui/` changed.

| Tool | When |
| --- | --- |
| oxlint | Required before merge. Includes `oxlint-plugin-react-doctor` (enabled rules are `"error"`). Fix these first. |
| i18n lint | Required after copy or locale-key changes. |

Rules that stay off, and why, are listed in `.react-doctor-ignores.md`. Re-generate the oxlint fragment with `npm run sync:react-doctor-oxlint` after a plugin bump.

Visible strings go through `src/i18n/*.ts`. Rules: `AGENTS.md` in this folder.
