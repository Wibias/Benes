---
title: For agents
description: Headless install and the GitHub-star consent boundary.
---

If you are an agent installing or operating Benes on a user’s machine, this page is the consent contract and the command sequence.

## Sequence

```bash
npm install -g @wibias/benes
benes start          # or: benes service
benes init           # writes ~/.benes/config.json; may inject Codex
benes ready --wait
```

`init` never starts the proxy. Live mutations (`benes provider add`, `benes combo set`, …) require a reachable listener.

## Do not star the repo

`benes start` / `benes service install` may print a one-line deferral about starring `Wibias/Benes`.

- Do not answer that prompt.
- Do not call `POST /api/github/star`. Without a dashboard session it returns `403 consent_required` (`internal/server/github_star_api.go`). Treat that as final.
- Do not run `gh repo star` on the user’s behalf.

Relay the question once, as a required Yes/No, in the same turn as the start that printed the deferral. Silence is not Yes. An explicit No is permanent.

## Detection

Agent-driven runs suppress the interactive star prompt so the human still sees it on their next interactive start.
