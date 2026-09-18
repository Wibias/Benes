---
title: Diagnostics
description: Bounded in-memory request telemetry for the Diagnostics inspector.
---

Diagnostics request telemetry is **in-memory and bounded**. It is not durable history.

The ring keeps at most **2000** records in the current process. Restarting Benes drops every row. Evicted rows are gone. A durable Session may outlive its Diagnostics rows; that is expected. Diagnostics never reconstructs missing rows from Sessions.

This is metadata/telemetry, not traffic capture. Request bodies, response bodies, prompts, tool arguments, Authorization headers, API keys, OAuth tokens, cookies, and provider secrets are not stored.

## Relationship to other surfaces

| Surface | What it is |
| --- | --- |
| Sessions | Durable conversation overview (`$BENES_HOME/sessions.sqlite`) |
| `GET /api/logs` | Legacy compatibility projection of the same in-memory ring |
| `GET /api/diagnostics/requests` | Structured summary/detail contract for the Diagnostics inspector (`#logs`) |
| Debug streams | Provider debug / usage extraction / injection / Claude inbound — separate |

Normal request telemetry does not require Provider debug.

## Legacy `GET /api/logs`

Loopback only. Response remains an **array** of:

```json
[
  {
    "id": "client-supplied-or-generated",
    "timestamp": "2026-09-04T12:00:00.0000000Z",
    "method": "POST",
    "path": "/v1/responses",
    "status": 200,
    "durationMs": 42,
    "sessionId": "ses_..."
  }
]
```

Query: `limit` (default 200), `status`, `sessionId`. Malformed `sessionId` is **400** `invalid_session_id`. A valid id with no retained matches is **200** `[]`.

`id` is the legacy data-plane request id: inbound `X-Request-ID` when present, otherwise a generated hex value. It is **not** the Benes `req_*` request id. Inbound `X-Request-ID` values retained as this `id` or as structured `correlationId` are clipped to **256 bytes** at a UTF-8 rune boundary. Benes-owned `req_*` identifiers are not clipped.

## Structured summary

`GET /api/diagnostics/requests`

```json
{
  "requests": [
    {
      "requestId": "req_ab12...",
      "timestamp": "2026-09-04T12:00:00.0000000Z",
      "sessionId": "ses_...",
      "protocol": "responses",
      "method": "POST",
      "path": "/v1/responses",
      "resolvedModel": "gpt-5.6",
      "provider": "openai-apikey",
      "status": 200,
      "durationMs": 42,
      "totalTokens": 6,
      "usageStatus": "reported"
    }
  ],
  "nextCursor": "MQ...",
  "reset": false,
  "historyTruncated": false
}
```

`requestId` is Benes-owned (`req_*`). `correlationId` is not on the summary; it is inbound `X-Request-ID` on the detail record. Protocol is set only when the request path established it (`responses`, `chat_completions`, `anthropic_messages`). Token fields and `usageStatus` are omitted when usage was unreported.

Query filters (exact, composable): `sessionId`, `status`, `protocol`, `provider`, `model`, `routeKind` (`direct` | `combo` | `policy`), `requestId`, `correlationId`. `limit` defaults to 200 and caps at 2000.

Invalid `sessionId` / `protocol` / `routeKind` / `status` / `limit` / `cursor` return **400** with `invalid_session_id`, `invalid_protocol`, `invalid_route_kind`, `invalid_status`, `invalid_limit`, or `invalid_cursor`.

Filtered example: `/api/diagnostics/requests?sessionId=ses_...&status=200`

## Incremental polling

The cursor is opaque. It is a process-generation plus a monotonic sequence, not a timestamp.

Without `cursor`: return the most recent matching page and a high-water cursor for the whole ring.

With `cursor`: return matching rows added after that high-water mark. The cursor is the global ring mark, not “matching records only”, so a filtered client does not rescan the same irrelevant rows. If a burst exceeds `limit`, `nextCursor` stops at the last returned row so the client can continue.

Empty incremental poll:

```json
{ "requests": [], "nextCursor": "MQ...", "reset": false, "historyTruncated": false }
```

A cursor from a previous process generation returns the current snapshot with `"reset": true`. A cursor older than the ring floor returns the current snapshot with `"reset": true` and `"historyTruncated": true`.

## Structured detail

`GET /api/diagnostics/requests/:requestId`

If the request is no longer in the ring:

```json
{ "error": { "code": "request_not_retained", "message": "request is no longer retained" } }
```

Diagnostics does not query Sessions to fabricate a partial record.

Example (fields omitted when unknown):

```json
{
  "requestId": "req_ab12...",
  "correlationId": "client-id",
  "sessionId": "ses_...",
  "timestamp": "2026-09-04T12:00:00.0000000Z",
  "protocol": "responses",
  "method": "POST",
  "path": "/v1/responses",
  "status": 200,
  "durationMs": 42,
  "requestBytes": 128,
  "responseBytes": 2048,
  "requestedServiceTier": "flex",
  "configuredServiceTier": "priority",
  "responseServiceTier": "flex",
  "routing": {
    "kind": "combo",
    "requestedModel": "combo/fast",
    "requestedProvider": "combo",
    "resolvedModel": "gpt-5.4",
    "provider": "openai-apikey",
    "comboId": "fast",
    "committedMember": "openai-apikey/gpt-5.4"
  },
  "attempts": [
    { "ordinal": 1, "member": "google/gemini-flash", "status": 503, "decision": "hop" },
    { "ordinal": 2, "member": "openai-apikey/gpt-5.4", "status": 200, "decision": "committed" }
  ],
  "timing": {
    "totalMs": 42,
    "headersMs": 12,
    "firstByteMs": 18,
    "ttftMs": 20,
    "firstDownstreamMs": 21,
    "upstreamEndMs": 40,
    "downstreamEndMs": 42
  },
  "timeline": [
    { "stage": "pre_dispatch", "side": "local", "elapsedMs": 0, "ok": true }
  ],
  "usage": { "status": "reported", "inputTokens": 4, "outputTokens": 2, "totalTokens": 6 },
  "cost": {
    "kind": "exact",
    "currency": "USD",
    "total": 0.0001,
    "price": { "provider": "xai", "modelId": "grok-4.6", "source": "x.ai/docs/models#grok-4.6", "confidence": "exact" }
  }
}
```

Admission-denied and ungrouped requests appear in Diagnostics. `sessionId` is present only when durable session resolution actually succeeded. Sessions store failure does not suppress the telemetry row; it has no `sessionId` and may include `failure.cause: "session_persist_failed"`.

There is **no** `client` or `surface` field. Benes has no authoritative request-origin signal that is safe to publish. Untagged traffic is not Codex.

Service-tier evidence is captured during the request. `requestedServiceTier` is the tier the client asked for, `configuredServiceTier` is the tier the canonical request policy (`requestPolicy.serviceTier`) configured for the request, and `responseServiceTier` is the tier the provider reported back. Each field is omitted when that fact does not exist. They are separate facts: an explicit client tier does not overwrite the configured one, and neither is collapsed into the provider's answer. The API never infers a tier from a model id or from the serialized outbound body.

The configured tier is frozen when the request arrives. A settings change applies to requests admitted afterwards, not to one already in flight, and every attempt, failover member, and continuation of one request keeps the tier that request admitted.

Effort inspector fields are not captured and stay omitted.

Candidate sets and selection reasons are not recalculated at read time. If they were not captured during the request, they are absent.

## Timing

`timing.totalMs` is the whole HTTP request duration. Milestone fields are elapsed milliseconds from request start and are present only when that milestone was **successfully** observed (`ok: true`). A failed milestone-shaped event (for example headers wait with `ok: false`) may appear on the timeline and in `failure`, but it does not populate the corresponding timing field. A later successful observation of the same milestone does.

| Field | Milestone |
| --- | --- |
| `headersMs` | `headers` |
| `firstByteMs` | `first_byte` (first upstream byte) |
| `ttftMs` | `ttft` (first model text) |
| `firstDownstreamMs` | `first_downstream` |
| `upstreamEndMs` | `upstream_end` |
| `downstreamEndMs` | `downstream_end` |

Those three first-output clocks are not interchangeable. Missing milestones stay absent. Timeline events are capped at 16 and include only `stage`, `side`, `milestone`, `elapsedMs`, `attempt`, `ok`, and a sanitized `normalizedCause` (lowercase identifier). Arbitrary strings and JSON payloads are dropped.

## Failure

`failure` is the last unsuccessful timeline event: `side`, `stage`, `cause`. Causes are normalized identifiers captured during the request (`admission_denied`, `connection_reset`, `provider_open_failed`, `session_persist_failed`, `internal_panic`, …). Raw upstream error text and panic payloads are not exposed. `errorCode`, when present, is that same sanitized identifier. A data-plane panic before any response write is recorded as HTTP 500 with `internal_panic`. If a status was already committed on the wire, that status is kept and `internal_panic` is still attributed. For admitted/groupable requests, durable Sessions store the same final observed HTTP status.

## Usage and cost

`usage.status` is `reported`, `estimated`, or `unreported`. Estimated values stay labelled estimated. Unreported/unsupported is not turned into zero. Durable Sessions still store only exact usage.

`cost.kind` is `exact`, `estimated`, or `unavailable`. Exact cost is never claimed from estimated usage or non-exact pricing. Currency is published only when the price table resolved USD. Unpriced models are `unavailable` with `reason: "price_unmatched"`. Cost projection loads operator `modelCosts` overlays only when usage is present, from a request-scoped snapshot shared by Sessions and Diagnostics. A usage-bearing request reads `config.json` at most once; a later request observes a file published by another process (`benes config set`, dashboard, or an atomic replace). Requests without usage do not read the config file.

## Bounds

- 2000 request records
- 16 timeline events
- 16 attempts
- 256 bytes (UTF-8 rune-safe) for externally supplied identifiers (`correlationId`, legacy `/api/logs` `id` from `X-Request-ID`, routing/attempt strings)
- 64 characters for normalized causes
