"use strict";

/**
 * Trusted-base check for the release workflow's first job.
 * The publish job must not start unless this returns ok.
 *
 * Allowed triggers: workflow_dispatch on main or preview, with an audited
 * 40-character SHA that still matches GITHUB_SHA.
 */

const PUBLISHABLE_REFS = Object.freeze([
  "refs/heads/main",
  "refs/heads/preview",
]);

const FULL_SHA = /^[0-9a-f]{40}$/;

function inspectReleaseTrigger(input) {
  const event = input && input.event;
  const ref = input && input.ref;
  const audited = input && input.auditedSha;
  const observed = input && input.observedSha;

  if (event !== "workflow_dispatch") {
    return {
      ok: false,
      reason: `Only workflow_dispatch may start a release (received ${event || "nothing"}).`,
    };
  }

  if (!PUBLISHABLE_REFS.includes(ref)) {
    return {
      ok: false,
      reason: `Release is limited to main and preview (received ${ref || "nothing"}).`,
    };
  }

  if (!audited) {
    return {
      ok: false,
      reason: "auditedSha is required so the publish job cannot float onto a later commit.",
    };
  }

  if (!FULL_SHA.test(audited)) {
    return {
      ok: false,
      reason: `auditedSha must be a 40-character lowercase hex SHA (received ${audited}).`,
    };
  }

  if (observed !== audited) {
    return {
      ok: false,
      reason:
        `The branch moved after the audit (audited ${audited}, now ${observed || "nothing"}). ` +
        "Refusing to publish.",
    };
  }

  return { ok: true, reason: null };
}

module.exports = {
  PUBLISHABLE_REFS,
  inspectReleaseTrigger,
};
