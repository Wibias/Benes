# Public docs (`docs/`)

Parent file: `/AGENTS.md`. This tree is the English Starlight app. When GitHub Pages is enabled it is served at `https://wibias.github.io/Benes/`.

## What to write

Describe shipped Go behaviour, or behaviour that is intentionally pending. Open `cmd/benes` and `internal/` before you assert a command, port, env name, or HTTP path. Default bind is **127.0.0.1:23100**. Combos are **failover** only (`unsupported_strategy` for anything else). `benes lab rebuild` is opt-in and is not on the default start path.

English is the only locale. Do not add `fr/`, `ja/`, `ko/`, `zh-cn`, or any other translation tree.

Astro `site` is `https://wibias.github.io` and `base` is `/Benes/`. A markdown link that starts with `/use/` 404s under that prefix. Use `./rel` or `import.meta.env.BASE_URL`.

Leave `dist/` and `.astro/` alone. Those directories are generated.

## How to edit

Keep the current Starlight layout, sidebar, components, and CSS. When a user workflow changes, update every page that describes that workflow. Do not paste `MAINTAINERS.md`, `SECURITY.md`, or `CONTRIBUTING.md` into a docs page — link the canonical file.

Repository files get repository-relative links. Other docs pages get links that still resolve under `/Benes/`.

## Check

```bash
cd docs
npm ci
npm run build
```

A docs change is not valid until that build finishes successfully.
