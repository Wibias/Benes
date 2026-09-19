# Antigravity account-scoped auth failure research

Date: 2026-09-19

## Result

**APPLIES**

Benes already has a real multi-account OAuth authority for Google Antigravity. The current pre-output recovery policy is not consistent across account-scoped failures.

## Current behavior

The active Antigravity request path has these behaviors:

- HTTP 429 marks the selected account and may retry the same request on another eligible account before any downstream output is committed.
- HTTP 403 is classified and marks the selected account, but the current request then returns an error instead of attempting another eligible account.
- HTTP 401 refreshes the exact same physical account once. If the refreshed credential also returns 401, the request terminates without marking that account unusable or attempting another eligible account.
- A refresh failure also terminates without trying another eligible account.

The account pool itself already knows how to exclude cooled or unusable accounts for later selection. This means some failures can steer a later request away from the bad account while still failing the request that discovered the problem.

## Evidence

Current production authority:

- `internal/providers/antigravity/client.go`
  - 429 can enter bounded pre-stream account failover.
  - 403 is handled in the same classification block but is not allowed into the account-hop branch.
  - 401 performs one same-account refresh and then stops.
- `internal/providers/antigravity/accounts.go`
  - account cooldown is bounded.
  - selection skips accounts in cooldown.
- `internal/providers/antigravity/auth_recovery_test.go`
  - current tests explicitly require 401 recovery to stay on the same account.
  - current tests explicitly require refresh failure not to hop accounts.
- `internal/providers/antigravity/client_test.go`
  - current coverage proves 429 account rotation.
  - geoblocked 403 is correctly non-rotating.

## Why this matters

A generic account-scoped 403 can indicate a problem with one OAuth account rather than the provider as a whole. A second 401 after a bounded same-account refresh also proves that the refreshed physical account is still unusable for the current request.

When another eligible account exists and no output has been committed, terminating the request wastes an already-existing recovery authority.

## Required implementation invariant

A separate implementation PR should:

1. Keep the existing same-account refresh as the first 401 recovery step.
2. After that bounded refresh fails or the refreshed account is still unauthorized, mark only that physical account as auth-unusable and permit one bounded pre-output account failover.
3. Allow account-scoped 403 failures to fail over only when they are not provider-wide failures such as geoblock.
4. Keep geoblock and other provider-wide failure classes non-rotating.
5. Never rotate after downstream output has been committed.
6. Preserve physical continuation and credential affinity rules.
7. Bind cooldown or unusable evidence to the exact account that produced the failure.
8. Keep the total physical-send budget bounded.
9. Preserve cancellation and deadline errors as authoritative.
10. Add RED tests for 403 rotation, second-401 rotation after one refresh, refresh-failure rotation, and geoblock non-rotation.

## Non-goals

- No generic OAuth policy rewrite.
- No provider-wide retry loop.
- No arbitrary 403 retry.
- No increase to unbounded send attempts.
- No production change in this research branch.
