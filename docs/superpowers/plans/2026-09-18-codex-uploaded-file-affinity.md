# Codex uploaded-file account affinity

## Goal

Prove and then close the case where a Responses request carries an uploaded `file_id` while the ChatGPT account pool changes the serving account.

Do not modify routing until a focused regression proves the current behavior.

## Current Benes evidence

- `internal/responses/request` accepts `file_id` on image and file content.
- `internal/responses/history` preserves file references in structured history.
- The `openai` provider can run in account-pool mode with thread affinity.
- Pre-output auth, quota, cooldown, and model-compatibility recovery can move a request to another account.
- Current continuation protection is strongest around `previous_response_id`; no separate uploaded-file ownership receipt is obvious in the account-change path.

This creates an authority question that must be answered from Benes state rather than by deleting request content.

## Required invariant

A file reference must never be sent under a different account merely because request recovery selected a new credential.

The proxy must not silently remove user-attached files to make a retry portable.

## Research sequence

1. Add a focused Go regression that creates two managed ChatGPT accounts and a request containing only a `file_id` as its account-bound carrier.
2. Force a pre-output account transition through the real pool path.
3. Capture the selected credential and outbound body without logging the file id outside the test process.
4. Establish RED only if the same file reference reaches a credential other than the issuing account.
5. Repeat with:
   - `input_image.file_id`;
   - `input_file.file_id`;
   - a request that also carries `previous_response_id`;
   - same-account refresh;
   - a new request with no prior account owner.

## Preferred design if RED is proven

Introduce a typed account-bound carrier classification at the Responses boundary.

The classifier should distinguish:

- portable user text and inline bytes;
- continuation ids that Benes can safely rebuild;
- uploaded-file references that require the issuing account.

Bind uploaded-file authority to the existing thread/continuation owner when the issuing account is known. A recovery path may use another account only when every carrier is portable.

If a request cannot move, keep the original upstream failure visible where possible. Do not convert a real 429/401 into an unrelated provider error, and do not mutate the caller-owned body.

## Better-than-minimal boundary

The fix must be carrier-based, not a string search for `file_id`.

Use the parsed Responses structure so future file-bearing fields cannot bypass the rule through nesting or alternate content shapes.

Account ownership should use the same trusted account identity used by pool affinity, not caller-provided headers.

## Tests

Focused tests must cover:

- file-only request refuses cross-account replay;
- same-account refresh remains allowed;
- inline `file_data` remains portable;
- ordinary text remains portable;
- mixed `previous_response_id` + file reference cannot lose the file constraint;
- no request-body or account identifier enters logs;
- body remains byte-equivalent when a move is refused.

If the implementation changes shared routing, run `go test ./...` and `go vet ./...` before review-ready.

## Non-goals

- Copying files between upstream accounts.
- Automatically re-uploading user data.
- Removing attachments from history.
- Disabling account pools.
- Treating all Responses state as non-portable.

## Acceptance gate

Implementation begins only after a failing regression demonstrates the cross-account file case on current `dev`.

The final change is acceptable only when account ownership is explicit, same-account recovery still works, and the proxy never answers a different question by dropping an attachment.
