# OpenRouter reset-aware cooldown gate

## Result on current dev

The suspected reset-aware OpenRouter credential cooldown is not currently applicable to Benes' active OpenRouter request path.

Current architecture:

- the built-in `openrouter` provider uses the `openai-chat` protocol;
- `internal/providers/openaichat.Client` owns one resolved API key;
- the provider registry passes `spec.APIKey` to OpenAI Chat and does not construct a rotating key-pool authority for that protocol;
- OpenAI Chat has no per-credential cooldown/parking state;
- an upstream HTTP 429 is terminal on that client and its reset metadata is not used to select another credential.

Therefore there is no existing OpenRouter key-cooldown object to harden. Adding provider-specific reset parsing now would create unused policy machinery.

## Required invariant before cooldown support can ship

If Benes later introduces rotating OpenRouter credentials, rate-limit state must be attached to the exact credential that received the 429.

A dated cooldown may be accepted only from bounded, canonical OpenRouter quota evidence. Generic provider prose must not create long-lived credential parking.

The future flow must preserve these rules:

1. parse at most a small bounded error body;
2. honor cancellation/deadlines while reading it;
3. accept a dated reset only from the documented OpenRouter quota error shape or a trusted reset header;
4. otherwise use a bounded `Retry-After` when valid;
5. otherwise use the normal short rate-limit backoff;
6. never persist raw error bodies, API keys, account identifiers, or request payloads;
7. do not mark unrelated providers using an OpenAI-compatible adapter with OpenRouter-specific cooldown semantics.

## Future RED tests

Before runtime support is added, tests must prove:

- credential A receives canonical quota-reset evidence and becomes unavailable only until the bounded reset instant;
- credential B remains eligible;
- unrelated JSON/prose mentioning a future date does not create a dated cooldown;
- malformed/oversized bodies cannot extend cooldown;
- `Retry-After` remains a bounded fallback;
- request cancellation during bounded body parsing aborts without mutating credential health;
- provider-level 429 handling remains terminal when there is no alternate credential authority.

## Configuration note

`providerregistry.Spec` currently has a generic `APIKeyPool` field because other protocols use it, but the OpenAI Chat construction path does not consume it. This research does not redefine that field or silently enable pooling for OpenRouter.

If OpenAI Chat key pooling is introduced, it should be an explicit provider-registry contract with tests for ownership, failover, cooldown, and observability rather than accidental reuse of another protocol's pool.

## Non-goals

- parsing reset timestamps that no runtime component consumes;
- adding an OpenRouter-only global cooldown;
- turning every OpenAI-compatible 429 into OpenRouter semantics;
- adding automatic retry after a terminal 429 without a credential authority.

## Acceptance

For current Benes this finding closes as not applicable to the active request architecture.

The characterization should be revisited only when OpenAI Chat gains a real multi-credential authority or provider health layer capable of parking an individual OpenRouter credential.
