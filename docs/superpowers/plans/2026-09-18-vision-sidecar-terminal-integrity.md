# Vision sidecar terminal integrity

## Goal

Prove that a vision description is complete before Benes treats it as a successful replacement for image input.

The sidecar must not turn a transport truncation or local output bound into an apparently complete caption.

## Current Benes evidence

`internal/sidecar/vision.Client.Describe` currently accumulates text until one of these conditions:

- provider emits `EventDone`;
- stream returns `io.EOF`;
- accumulated text exceeds `maxDescription`.

After all three conditions it bounds the accumulated string and returns it as a successful `Result` when non-empty.

That makes completion state implicit.

## Required invariant

A description is successful only when the provider supplies a clean terminal event for the same request.

Unexpected EOF, parser truncation, local byte/rune limits, cancellation, and upstream error must stay distinguishable from a complete result.

## Research sequence

1. Add a RED test where text deltas are followed by EOF without `EventDone`.
2. Add a RED test where text exceeds the configured description limit before `EventDone`.
3. Confirm the current code returns partial text as success in both cases.
4. Check each provider used as a vision sidecar to confirm that a successful stream has a canonical terminal event.
5. Only then change production behavior.

## Preferred design

Track terminal state explicitly inside `Describe`.

Suggested result states:

- complete;
- upstream error;
- incomplete transport;
- local description limit exceeded;
- caller cancellation/deadline.

Return a typed error for every non-complete state. Do not cache, inject, or forward partial descriptions.

The character limit should be enforced before the builder grows materially beyond the configured bound. Avoid repeatedly converting the full builder to a rune slice on every delta if a bounded counter can do the same work.

## Better-than-minimal behavior

Keep the output bound and completion proof independent:

- the bound protects memory and prompt size;
- the terminal event proves semantic completion.

A description exactly at the limit may still succeed if the terminal event follows. A description that exceeds the limit must fail closed even if later bytes contain a terminal event.

## Tests

Cover:

- text + `EventDone` succeeds;
- text + EOF without terminal event fails;
- empty + terminal event keeps the existing empty-result refusal;
- limit exceeded fails instead of returning a clipped caption;
- exact-limit text + terminal event succeeds;
- upstream 429 remains quota-classified;
- cancellation/deadline remains distinguishable;
- stream close still happens on every exit.

## Non-goals

- Raising the default caption size without evidence.
- Returning a truncated caption with a warning.
- Changing image-count or image-byte limits.
- Making the vision sidecar mandatory.

## Acceptance gate

Implementation begins only after focused tests reproduce incomplete-success behavior on current `dev`.

The final implementation must prove completion and bounds separately, with no path that converts partial output into a normal description.
