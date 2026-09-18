# Cursor local-effect replay gate

## Result on current dev

The suspected replay-after-local-effect class is not currently reproducible in Benes.

The Cursor provider under `internal/providers/cursor` implements remote provider transport, continuation/checkpoint handling, and provider blob exchange. It does not currently contain a native file, shell, delete, write, or fetch execution path.

The provider preset still contains configuration language for a possible native-local-execution experiment. That configuration language is not evidence that the provider currently performs local effects.

No production change is justified from this finding today.

## Future invariant

If native local effects are introduced later, retry authority must become effect-aware.

Before the first irreversible or externally visible local effect, the request must cross a typed effect-commit boundary. After that point:

- a different credential must not replay the request;
- a different provider/target must not replay the request;
- transport recovery must not resend the effect-bearing turn unless the effect is proven idempotent by a stronger receipt;
- continuation state must remain bound to the credential and provider that observed the effect.

Request text, sandbox markers, model claims, and tool names are not authority to declare an effect safe to replay.

## Required tests before native execution can ship

Any future native execution path must add tests that prove:

- no local effect occurs without explicit operator enablement;
- a pre-effect network failure may retry under the existing policy;
- a post-effect 401, 429, 5xx, connection reset, or cancellation cannot trigger cross-credential replay;
- combo failover cannot duplicate an effect;
- an idempotency receipt, if introduced, is provider-verified and scoped to the exact operation;
- diagnostics expose only a bounded effect-state reason and never command contents, file contents, credentials, or provider continuation values.

## Better design

Do not add a Cursor-specific `doNotRetry` boolean.

If Benes gains local-effect execution, add a shared request execution state with explicit transitions such as:

- pre-dispatch;
- upstream-sent;
- local-effect-committed;
- output-committed.

Recovery policy can then use the same state across providers, combos, and credentials.

## Non-goals

- Adding native local execution.
- Re-enabling legacy experimental behavior.
- Treating ordinary remote tool calls as local effects.
- Adding retry machinery for a path that does not exist.

## Acceptance gate

This research PR is complete as a non-applicability finding for current `dev`.

No runtime code should be added unless a future change introduces an executable native-local-effect path.
