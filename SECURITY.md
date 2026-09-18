# Security

benes is a local HTTP proxy. A report that only works after the reporter already has a shell as the user is still in scope if it escapes the intended bind, leaks credentials off the box, or skips the remote-bind token.

## Supported lines

| Line | Fixes accepted |
| --- | --- |
| `main` | yes |
| Latest published npm package `benes` | yes |
| Older tags | no |

If the report is against an old tag, maintainers may ask you to reproduce on `main` or the latest npm build before triage continues.

## Private reports only

Do not file an undisclosed vulnerability as a public GitHub issue.

Use GitHub’s private advisory form on this repository:

**https://github.com/Wibias/Benes/security/advisories/new**

Security tab → Report a vulnerability. There is no security email. The form is the only channel for undisclosed issues.

Include version or commit, how to reproduce, impact, and the bind address plus auth setup you used. Strip tokens, cookies, `auth.json`, and account ids from logs and screenshots.

If the form is unreachable, open a public issue that only asks for a private channel. Put no exploit details, secrets, or live targets in that issue.

## Attack surface that actually exists

Default bind is `127.0.0.1:23100`. Anything that can reach that port can send completions as the configured providers.

- Loopback does not require `BENES_API_AUTH_TOKEN`.
- `"hostname": "0.0.0.0"` refuses to start without that token. Every request must then carry `x-benes-api-key`.
- Durable secrets live in `~/.benes/auth.json` (mode 0600) and, for ChatGPT, `~/.benes/codex-accounts.json`. Treat `BENES_HOME` / `~/.benes` as credential storage.
- `benes init` and `benes sync` write Codex-native files under `CODEX_HOME` / `~/.codex`. `benes restore` undoes that. Deleting `~/.benes` alone does not.

## After a report

Maintainers review on a best-effort basis. There is no SLA. Typical order:

1. Confirm the version or commit.
2. Reproduce locally.
3. Judge impact and a safe fix.
4. Time disclosure if a patch is needed.

Non-sensitive hardening ideas can be public issues or PRs once disclosure is done.

Who handles advisories: [`MAINTAINERS.md`](./MAINTAINERS.md).
