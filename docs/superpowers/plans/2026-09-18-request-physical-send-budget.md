# Request-scoped physical send budget

## Goal

Determine the maximum number of physical upstream sends a single admitted request can cause across provider retries, credential recovery, and combo failover.

If the current bound is not explicit, introduce one shared request-scoped budget rather than independent counters at each layer.

## Why this needs research first

Benes deliberately has several recovery mechanisms:

- provider-specific transient retry;
- API-key or OAuth credential failover;
- ChatGPT account recovery;
- Google retry;
- combo failover;
- provider-native recovery.

Each mechanism is reasonable in isolation. Multiplication between layers is the failure mode to test.

## Required invariant

For every admitted logical request:

- every physical inference send has one ordinal;
- each physical send is charged once;
- a reservation that never reaches the network is refundable;
- retry preparation happens only after admission;
- post-output failover stays forbidden;
- the configured maximum is independent of how many nested layers participate.

Catalog discovery, OAuth login, quota probes, and other non-inference traffic need separate accounting and must not silently consume inference capacity.

## Research sequence

1. Trace the real dispatch graph from listener admission to physical `http.Client.Do` calls.
2. Identify each loop or recursion that can trigger another inference send.
3. Build a deterministic test provider that returns a chosen sequence of 5xx, 429, connection failure, and success.
4. Combine it with:
   - two credential slots;
   - a two-member combo;
   - provider transient retry enabled.
5. Count actual physical sends, not high-level attempts.
6. Establish whether the current maximum can exceed the intended policy.

## Preferred design if a shared bound is needed

Add a request-owned `PhysicalSendBudget` carried in `providers.DispatchRequest` or an adjacent typed execution context.

Use a lease API:

- `Reserve(reason)`;
- `Commit()` immediately before the physical send;
- `Release()` only before commit;
- immutable ordinal and reason for diagnostics.

A child dispatch receives the same budget object. It does not copy the remaining integer.

Separate route-hop admission from physical-send admission. One route transition may result in zero or one physical send, and the accounting must represent that distinction.

## Failure contract

Budget exhaustion is a local policy refusal, not "provider unreachable".

Return a stable non-5xx error before another physical send. Preserve the last real upstream response when that is the established recovery contract.

## Tests

The final implementation must prove:

- one retry equals one additional charge;
- nested retry layers cannot double-charge one send;
- a pre-dispatch refusal consumes no send;
- cancellation before network I/O refunds a reservation;
- cancellation after send does not refund;
- combo + credential + transient retry stays inside one total ceiling;
- no retry occurs after output is committed;
- diagnostics expose ordinals/reasons without secrets.

Shared execution changes require `go test ./...` and `go vet ./...`.

## Non-goals

- Maximising retries.
- Hiding genuine upstream 429/5xx responses.
- Counting model discovery or quota refresh as inference.
- Adding provider-specific magic numbers in the shared layer.

## Acceptance gate

Do not add a global budget until a test demonstrates either an unbounded/multiplicative path or inconsistent accounting on current `dev`.

If current behavior is already globally bounded, keep the architecture and add only the regression that proves it.
