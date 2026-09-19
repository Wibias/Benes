# Antigravity request-local failover budget research

Date: 2026-09-19

## Result

**APPLIES**

The Antigravity pre-output failover counter is stored on the long-lived provider pool, but the limit is intended to bound one request.

## Current authority

`internal/providers/antigravity/accounts.go` stores:

- `Pool.attempts int`;
- `AllowPreStreamFailover()`, which increments that shared field;
- `ResetAttempts()`, which can clear it.

Repository search finds no caller of `ResetAttempts()`.

The same pool instance is retained by the Antigravity client. Calls to `AllowPreStreamFailover()` therefore accumulate across independent requests.

The counter is also shared by different recovery paths. Account failover and endpoint-peer failover both consume the same process-lifetime allowance.

## Existing request-local bounds

The request path already has request-local loop guards:

- `accountHops < maxPreStreamFailover` bounds account changes for one request;
- `peerFailed` prevents repeated peer-endpoint failover;
- downstream output commit and physical ownership rules independently prevent unsafe replay.

A second long-lived mutable counter is not required to stop a single-request carousel.

## Failure mode

After three successful pre-output failover decisions on one client, `AllowPreStreamFailover()` returns false for every later request until the client is rebuilt or someone calls the currently-unused reset method.

This can make a healthy alternate account or endpoint unreachable only because earlier unrelated requests consumed the shared allowance.

## Required implementation invariant

A separate implementation PR should:

1. Make the failover budget request-local.
2. Keep `maxPreStreamFailover` as the per-request account-hop bound.
3. Keep peer failover bounded to one peer transition.
4. Keep hard pins, continuation ownership, output-commit rules, cancellation, and paid-image rules unchanged.
5. Prove that four independent requests can each use their own bounded account failover.
6. Avoid adding a request-start reset with shared mutable state, because concurrent requests could reset each other's budget.

## Non-goals

- No increase to the per-request account-hop limit.
- No change to cooldown duration.
- No new retry class.
- No change to provider selection ordering.
