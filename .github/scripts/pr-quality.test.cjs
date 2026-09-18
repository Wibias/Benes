"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const {
  gatherQualityProblems,
  inspectReadiness,
  canonicalReadinessBlock,
  hasRepoPush,
  CLAIM_BOX,
  restoreBlankReadiness,
  clearClaimTicks,
} = require("./pr-quality.cjs");

const SUMMARY = [
  "## Summary",
  "Keep combo failover on the first visible stream event and hop HTTP 410 like other gone statuses.",
  "",
  "## Verification",
  "- go test ./internal/combo",
  "- Confirm the walker stays on the committed member after output",
].join("\n");

function problems(extra) {
  return gatherQualityProblems({
    baseRef: "dev",
    allowedBases: ["dev"],
    body: SUMMARY,
    behindMain: 1,
    behindBase: 0,
    authorPermission: "read",
    ...extra,
  }).map((item) => item.code);
}

describe("branch gate", () => {
  it("fails an ordinary pull request targeting main", () => {
    assert.equal(
      problems({ baseRef: "main", headRef: "feat/x", sameRepository: true, authorPermission: "write" })
        .includes("wrong_base"),
      true,
    );
  });

  it("allows same-repository maintainer promotion from dev to preview", () => {
    assert.equal(
      problems({
        baseRef: "preview",
        headRef: "dev",
        sameRepository: true,
        authorPermission: "write",
      }).includes("wrong_base"),
      false,
    );
  });

  it("allows same-repository maintainer promotion from dev to main", () => {
    assert.equal(
      problems({
        baseRef: "main",
        headRef: "dev",
        sameRepository: true,
        authorPermission: "admin",
      }).includes("wrong_base"),
      false,
    );
  });

  it("rejects promotion-shaped pulls from forks or non-maintainers", () => {
    assert.equal(
      problems({
        baseRef: "preview",
        headRef: "dev",
        sameRepository: false,
        authorPermission: "write",
      }).includes("wrong_base"),
      true,
    );
    assert.equal(
      problems({
        baseRef: "preview",
        headRef: "dev",
        sameRepository: true,
        authorPermission: "read",
      }).includes("wrong_base"),
      true,
    );
  });

  it("passes the branch portion for dev", () => {
    assert.equal(problems({}).includes("wrong_base"), false);
  });

  it("skips wrong_base for a stacked parent head", () => {
    assert.equal(
      problems({ baseRef: "feat/parent", stackedOnParent: true }).includes("wrong_base"),
      false,
    );
  });
});

describe("description gate", () => {
  it("fails an untouched template", () => {
    const body = fs.readFileSync(
      path.join(__dirname, "..", "PULL_REQUEST_TEMPLATE.md"),
      "utf8",
    );
    const result = gatherQualityProblems({
      baseRef: "dev",
      allowedBases: ["dev"],
      body,
      behindMain: 1,
      behindBase: 0,
      authorPermission: "read",
    });
    assert.equal(result.some((item) => item.code === "bad_description"), true);
  });

  it("accepts a meaningful Summary and Verification", () => {
    assert.equal(problems({}).includes("bad_description"), false);
  });
});

describe("readiness boxes", () => {
  it("treats four ticked boxes as complete and a missing block as absent", () => {
    const block = canonicalReadinessBlock().replaceAll("- [ ] ", "- [x] ");
    assert.equal(inspectReadiness(block).complete, true);
    assert.equal(inspectReadiness("## Summary\nplain").present, false);
  });

  it("unticks only the claimed latest-dev box", () => {
    const body = canonicalReadinessBlock().replaceAll("- [ ] ", "- [x] ");
    const updated = clearClaimTicks(body, [CLAIM_BOX.latest_dev]);
    assert.match(updated, /\[ \] I pushed my PR to the latest dev commit\./);
    assert.match(updated, /\[x\] All CI tests are green on my local testing\./);
  });

  it("resets a completed checklist after a new head", () => {
    const completed = canonicalReadinessBlock().replaceAll("- [ ] ", "- [x] ");
    assert.equal(inspectReadiness(restoreBlankReadiness(completed)).checked, 0);
  });
});

describe("GUI screenshot", () => {
  it("requires a screenshot for a visual gui path", () => {
    assert.equal(
      problems({ changedFilePaths: ["gui/src/pages/Providers.tsx"] }).includes(
        "missing_ui_screenshot",
      ),
      true,
    );
  });

  it("accepts an embedded image and a maintainer waiver comment", () => {
    assert.equal(
      problems({
        body: `${SUMMARY}\n\n![after](https://example.com/after.png)`,
        changedFilePaths: ["gui/src/pages/Providers.tsx"],
      }).includes("missing_ui_screenshot"),
      false,
    );
    assert.equal(
      problems({
        changedFilePaths: ["gui/src/pages/Providers.tsx"],
        guiOverrideComments: [{ author_association: "OWNER", body: "no gui changes here" }],
      }).includes("missing_ui_screenshot"),
      false,
    );
  });
});

describe("maintainer authority", () => {
  it("treats write access as maintainer push authority", () => {
    assert.equal(hasRepoPush("write"), true);
    assert.equal(hasRepoPush("read"), false);
  });
});
