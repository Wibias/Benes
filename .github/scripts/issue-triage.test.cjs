"use strict";

const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const {
  decideDuplicateClose,
  decideRelatedReports,
  readNominationNumbers,
  evidenceFromIssue,
} = require("./issue-triage.cjs");

const COMBO_GONE =
  "POST /v1/responses returned HTTP 410 after the first combo target and before any output.";
const COMBO_WORDING =
  "Codex saw HTTP 410 from POST /v1/responses when the first combo member was gone.";
const CATALOG_GONE =
  "GET /v1/models returned HTTP 410 from a stale catalog cache entry.";
const KIRO_RESET =
  "The Kiro adapter closed the stream with ECONNRESET while rotating a 429 toward quota headroom.";
const DASHBOARD_RESET =
  "The dashboard quota poll failed with ECONNRESET against 127.0.0.1:23100.";
const ANTHROPIC_SYSTEM =
  "POST /v1/messages returned HTTP 400 because the system field was dropped.";
const ANTHROPIC_TOOLS =
  "POST /v1/messages returned HTTP 400 because tools were stripped from the payload.";

function closeOf(current, candidate, nominated = [String(candidate.number)]) {
  return decideDuplicateClose({
    currentIssue: current,
    candidateIssues: [candidate],
    nominatedIds: nominated,
  });
}

describe("automatic close needs identical locator-plus-outcome records", () => {
  it("closes when both issues carry the same specific failure record and the model nominated the candidate", () => {
    const match = closeOf(
      { number: 40, title: "combo hop", body: COMBO_GONE },
      { number: 12, title: "earlier combo hop", body: COMBO_GONE },
    );
    assert.equal(match.number, "12");
    assert.equal(match.signature, COMBO_GONE);
  });

  it("does not close on the same generic HTTP status in unrelated failures", () => {
    assert.equal(
      closeOf(
        { number: 40, title: "combo 410", body: COMBO_GONE },
        { number: 12, title: "quota 410", body: CATALOG_GONE },
      ),
      null,
    );
  });

  it("does not close on the same errno in unrelated contexts", () => {
    assert.equal(
      closeOf(
        { number: 40, title: "kiro reset", body: KIRO_RESET },
        { number: 12, title: "dashboard poll", body: DASHBOARD_RESET },
      ),
      null,
    );
  });

  it("does not close on the same provider with different behaviour", () => {
    assert.equal(
      closeOf(
        { number: 40, title: "anthropic system field", body: ANTHROPIC_SYSTEM },
        { number: 12, title: "anthropic streaming", body: ANTHROPIC_TOOLS },
      ),
      null,
    );
  });

  it("does not close when the model nominates a candidate that lacks the record", () => {
    assert.equal(
      closeOf(
        { number: 40, title: "combo hop", body: COMBO_GONE },
        { number: 12, title: "unrelated", body: "Failover still feels wrong." },
        ["12"],
      ),
      null,
    );
  });

  it("ignores a matching record hidden in an HTML comment", () => {
    assert.equal(
      closeOf(
        { number: 40, title: "combo hop", body: COMBO_GONE },
        { number: 12, title: "hidden", body: `visible noise\n<!-- ${COMBO_GONE} -->` },
      ),
      null,
    );
  });
});

describe("related suggestions use a lower independently verified bar", () => {
  it("suggests related when both records share a locator and outcome without being the same line", () => {
    const rows = decideRelatedReports({
      currentIssue: { number: 40, title: "combo hop wording", body: COMBO_WORDING },
      candidateIssues: [{ number: 12, title: "earlier", body: COMBO_GONE }],
      nominatedRelated: [{ number: "12", caption: "ignore this model sentence" }],
    });
    assert.equal(rows.length, 1);
    assert.equal(rows[0].number, "12");
    assert.match(rows[0].reason, /POST \/V1\/RESPONSES/i);
    assert.match(rows[0].reason, /HTTP 410/);
    assert.doesNotMatch(rows[0].reason, /ignore this model sentence/);
  });

  it("does not treat model prose as evidence", () => {
    const rows = decideRelatedReports({
      currentIssue: { number: 40, title: "bind", body: "The tray failed to start." },
      candidateIssues: [{ number: 12, title: "other", body: "Docs typo on combos.md." }],
      nominatedRelated: [
        {
          number: "12",
          caption: "Both return HTTP 410 from POST /v1/responses during combo failover.",
        },
      ],
    });
    assert.deepEqual(rows, []);
  });
});

describe("nomination ingest is fail-closed", () => {
  const ctx = { currentNumber: "40", knownNumbers: ["12", "13"] };

  it("drops unknown and self issue numbers", () => {
    const admitted = readNominationNumbers(
      JSON.stringify({ duplicates: ["12", "40", "99", "12"], related: [] }),
      ctx,
    );
    assert.deepEqual(admitted.duplicates, ["12"]);
  });

  it("rejects malformed model output", () => {
    assert.equal(readNominationNumbers("not json", ctx), null);
    assert.equal(readNominationNumbers("", ctx), null);
  });
});

describe("weak single symptoms are not close records", () => {
  it("does not treat a lone HTTP 500 or ECONNRESET line as close evidence", () => {
    assert.equal(evidenceFromIssue({ title: "fail", body: "returned HTTP 500" }).closeRecords.size, 0);
    assert.equal(evidenceFromIssue({ title: "fail", body: "stream died with ECONNRESET" }).closeRecords.size, 0);
  });
});
