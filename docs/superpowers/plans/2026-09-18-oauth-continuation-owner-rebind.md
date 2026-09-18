# OAuth continuation owner rebind audit

## Goal

Audit every credential-pool failover path that can reuse provider continuation state and prove that continuation ownership follows the credential that produced it.

This is a Benes-wide authority check, not a provider-specific patch.

## Current Benes evidence

Benes already models provider state and account ownership in several places:

- ChatGPT thread affinity and continuation binding;
- Kiro continuation owners;
- OAuth/provider credential pools;
- pre-output 401/429 recovery;
- combo failover and provider-local retry.

The risk is not that failover exists. The risk is that a continuation token, conversation id, reasoning receipt, or provider session key can survive a credential change without an explicit ownership decision.

## Required invariant

Provider-generated continuation state is reusable only inside the authority scope that created it.

A credential hop must do exactly one of the following:

1. prove the state is portable;
2. rebuild from portable conversation history;
3. rebind only after new state is issued by the new credential;
4. fail closed.

It must never blindly replay credential-owned state under a new account.

## Research matrix

Build a table from production code, not documentation, for every provider that has both pooled credentials and continuation-like state.

For each carrier record:

- carrier name;
- where it is parsed;
- where it is stored;
- owner identity;
- whether it crosses retries;
- whether it crosses account hops;
- invalidation point;
- regression test.

Start with ChatGPT, Kiro, Google account-backed paths, Cursor, and any generic OAuth pool exposed through `internal/providerregistry`.

## Test strategy

Add focused tests at the lowest shared boundary that can observe both credential selection and continuation reuse.

The RED condition is:

- request 1 produces provider-owned state under credential A;
- recovery selects credential B;
- outbound request to B still contains A-owned state.

Negative controls:

- same credential retry preserves state;
- portable text/tool history survives a hop;
- a fresh B-issued continuation can be stored after the hop;
- refusal does not destroy A's durable state;
- combo target changes do not accidentally share continuation ownership across providers.

## Preferred design if gaps exist

Use one typed owner value for provider state:

`provider + destination + auth class + credential identity + provider-specific scope`.

State stores should compare owners structurally. Avoid ad hoc booleans such as `changedAccount` at each call site.

A successful credential hop should produce a new dispatch authority before any provider-owned continuation is considered. The continuation layer then decides whether to reuse, rebuild, or refuse.

## Observability

Diagnostics may record a bounded reason such as `continuation_owner_changed`, but must not record account ids, tokens, conversation ids, or raw continuation values.

## Non-goals

- Forcing all providers onto one continuation format.
- Removing provider-native sessions.
- Retrying after output is committed.
- Treating a combo hop as equivalent to an account hop.

## Acceptance gate

No runtime change lands from this research alone.

Implementation is justified only by a focused RED case on current `dev`, and each fix must preserve same-owner continuation behavior while proving that cross-owner state cannot leak through another retry path.
