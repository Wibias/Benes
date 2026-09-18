## Summary

- What changed for a Benes user or maintainer (Go CLI, loopback listener on 127.0.0.1:23100, dashboard, or docs).
- If this touches combos, say the strategy is still failover-only unless the projector actually gained a new one.

## Verification

- Paste commands and their output. Default set: `go test ./...`, `go vet ./...`, `npm run privacy:scan`.
- GUI: `cd gui && npm run lint && npm run build` (and `npm run lint:i18n` after copy). Attach a screenshot unless a maintainer applied `gui-screenshot-waived`.
- Docs: `cd docs && npm run build`.
- Hosted Actions on this org may not start because of billing. Local logs are the evidence. “CI is green” with no log is not a plan.

## Checklist

- [ ] Targets `dev` (not `main`).
- [ ] Diff stays on the stated problem.
- [ ] User-facing behaviour has a matching `docs/` or README update.
- [ ] Auth, secrets, workflows, and `scripts/release.ts` had a second look.
- [ ] Did not rename `opencode.svg` or treat OpenCode as this project.
