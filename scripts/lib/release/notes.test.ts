import assert from "node:assert/strict";
import { describe, test } from "node:test";
import {
  buildNotes,
  categoryFromTitle,
  mentionsPull,
  parseAssociatedPulls,
  parseGitLogRecords,
  sanitizeNoteText,
  selectNotesBaseline,
  type HistoryCommit,
} from "./notes.ts";

const SHA_A = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
const SHA_B = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";
const SHA_C = "cccccccccccccccccccccccccccccccccccccccc";

function commit(partial: Partial<HistoryCommit> & Pick<HistoryCommit, "sha" | "subject">): HistoryCommit {
  return { body: "", pulls: [], ...partial };
}

describe("selectNotesBaseline", () => {
  test("preview notes start at the previous reachable release on either channel", () => {
    assert.equal(
      selectNotesBaseline("4.2.0-preview.2", ["v4.2.0-preview.1", "v4.1.0"]),
      "v4.2.0-preview.1",
    );
  });

  test("stable notes start at the previous stable so preview work is reconstructed", () => {
    assert.equal(
      selectNotesBaseline("4.2.0", ["v4.2.0-preview.1", "v4.1.0", "v4.1.0-preview.4"]),
      "v4.1.0",
    );
  });

  test("a trailing same-core preview does not hide the prior stable for the next preview", () => {
    assert.equal(
      selectNotesBaseline("4.2.0-preview.1", ["v4.1.0", "v4.1.0-preview.9"]),
      "v4.1.0",
    );
  });
});

describe("buildNotes", () => {
  test("renders a PR-backed commit once, in Features, with coverage", () => {
    const built = buildNotes({
      version: "4.2.0",
      packageName: "benes",
      distTag: "latest",
      repository: "testhost/app",
      tags: ["v4.1.0"],
      generatedNotes: "",
      commits: [
        commit({
          sha: SHA_A,
          subject: "feat: add local routing board (#501)",
          pulls: [
            {
              number: 501,
              title: "feat: add local routing board",
              author: "river",
              labels: ["enhancement"],
              merged: true,
            },
          ],
        }),
      ],
    });
    assert.equal(built.errors.length, 0, built.errors.join("\n"));
    assert.match(built.markdown, /## Features/);
    assert.match(built.markdown, /Add local routing board \(#501\)/);
    assert.equal(mentionsPull(built.markdown, 501), true);
    assert.equal(built.changes.length, 1);
  });

  test("renders a direct commit when GitHub metadata is missing", () => {
    const built = buildNotes({
      version: "4.2.0",
      packageName: "benes",
      distTag: "latest",
      repository: "testhost/app",
      tags: ["v4.1.0"],
      commits: [commit({ sha: SHA_B, subject: "fix: honor empty quota remaining" })],
    });
    assert.equal(built.errors.length, 0, built.errors.join("\n"));
    assert.match(built.markdown, /## Fixes/);
    assert.match(built.markdown, /bbbbbbbb/);
    assert.match(built.markdown, /honor empty quota remaining/i);
  });

  test("skips an internal skip-changelog pull and fails closed if nothing visible remains", () => {
    const skipped = buildNotes({
      version: "4.2.0",
      packageName: "benes",
      distTag: "latest",
      repository: "testhost/app",
      tags: ["v4.1.0"],
      commits: [
        commit({
          sha: SHA_A,
          subject: "chore: rotate fixture seeds (#502)",
          pulls: [
            {
              number: 502,
              title: "chore: rotate fixture seeds",
              author: "maple",
              labels: ["skip-changelog"],
              merged: true,
            },
          ],
        }),
      ],
    });
    assert.equal(skipped.changes.length, 0);
    assert.equal(skipped.skipped.length, 1);
    assert.ok(skipped.errors.length > 0);

    const mixed = buildNotes({
      version: "4.2.0",
      packageName: "benes",
      distTag: "latest",
      repository: "testhost/app",
      tags: ["v4.1.0"],
      commits: [
        commit({
          sha: SHA_A,
          subject: "chore: rotate fixture seeds (#502)",
          pulls: [
            {
              number: 502,
              title: "chore: rotate fixture seeds",
              author: "maple",
              labels: ["skip-changelog"],
              merged: true,
            },
          ],
        }),
        commit({ sha: SHA_C, subject: "docs: describe failover walker" }),
      ],
    });
    assert.equal(mixed.errors.length, 0, mixed.errors.join("\n"));
    assert.match(mixed.markdown, /## Documentation/);
    assert.doesNotMatch(mixed.markdown, /#502/);
  });

  test("ignores version-bump metadata and does not duplicate overlapping PR references", () => {
    const built = buildNotes({
      version: "4.2.0",
      packageName: "benes",
      distTag: "latest",
      repository: "testhost/app",
      tags: ["v4.1.0"],
      generatedNotes: [
        "### Features",
        "* Add local routing board by @river in https://github.com/testhost/app/pull/501",
      ].join("\n"),
      commits: [
        commit({
          sha: SHA_A,
          subject: "feat: add local routing board (#501)",
          pulls: [
            {
              number: 501,
              title: "feat: add local routing board",
              author: "river",
              labels: ["enhancement"],
              merged: true,
            },
          ],
        }),
        commit({ sha: SHA_B, subject: "release: v4.2.0" }),
      ],
    });
    assert.equal(built.errors.length, 0, built.errors.join("\n"));
    assert.equal(built.markdown.match(/#501/g)?.length, 2); // body bullet + changelog catalog
    assert.doesNotMatch(built.markdown, /release: v4\.2\.0/);
  });

  test("orders categories deterministically and sanitizes mention injection", () => {
    const built = buildNotes({
      version: "4.2.0",
      packageName: "benes",
      distTag: "latest",
      repository: "testhost/app",
      tags: ["v4.1.0"],
      commits: [
        commit({ sha: SHA_C, subject: "chore: tidy scripts" }),
        commit({ sha: SHA_A, subject: "feat: ping @admin in copy" }),
        commit({ sha: SHA_B, subject: "fix: restore <script> guard" }),
      ],
    });
    const features = built.markdown.indexOf("## Features");
    const fixes = built.markdown.indexOf("## Fixes");
    const maintenance = built.markdown.indexOf("## Maintenance");
    assert.ok(features < fixes);
    assert.ok(fixes < maintenance);
    assert.match(built.markdown, /@\u200badmin/);
    assert.match(built.markdown, /\\<script\\>/);
  });

  test("does not treat nested category-style headings in supplied notes as Benes headings", () => {
    const built = buildNotes({
      version: "4.2.0",
      packageName: "benes",
      distTag: "latest",
      repository: "testhost/app",
      tags: ["v4.1.0"],
      generatedNotes: [
        "### New Features",
        "* Add local routing board by @river in https://github.com/testhost/app/pull/501",
      ].join("\n"),
      commits: [
        commit({
          sha: SHA_A,
          subject: "feat: add local routing board (#501)",
          pulls: [
            {
              number: 501,
              title: "feat: add local routing board",
              author: "river",
              labels: ["enhancement"],
              merged: true,
            },
          ],
        }),
      ],
    });
    assert.equal(built.errors.length, 0, built.errors.join("\n"));
    assert.match(built.markdown, /## Other/);
    assert.doesNotMatch(built.markdown, /## Features/);
  });

  test("malformed git log fails closed", () => {
    assert.throws(() => parseGitLogRecords("not-a-record-without-fields"));
  });

  test("categoryFromTitle maps conventional types", () => {
    assert.equal(categoryFromTitle("feat: x"), "Features");
    assert.equal(categoryFromTitle("fix(proxy): y"), "Fixes");
    assert.equal(categoryFromTitle("docs: z"), "Documentation");
    assert.equal(categoryFromTitle("chore: z"), "Maintenance");
    assert.equal(categoryFromTitle("unprefixed change"), "Other");
  });

  test("sanitizeNoteText flattens controls", () => {
    assert.equal(sanitizeNoteText("a\n\tb"), "a b");
  });

  test("mentionsPull does not match a longer number", () => {
    assert.equal(mentionsPull("see #12 and #123", 12), true);
    assert.equal(mentionsPull("see #123", 12), false);
  });
});

describe("notes determinism and fail-safe ingestion", () => {
  const fixture = () => ({
    version: "4.2.0",
    packageName: "benes",
    distTag: "latest" as const,
    repository: "testhost/app",
    tags: ["v4.1.0"],
    commits: [
      commit({ sha: SHA_C, subject: "chore: tidy scripts" }),
      commit({ sha: SHA_A, subject: "feat: add local routing board (#501)" }),
      commit({ sha: SHA_B, subject: "fix: restore <script> guard" }),
    ],
  });

  test("renders identical markdown bytes for identical input", () => {
    const first = buildNotes(fixture());
    const second = buildNotes(fixture());
    assert.equal(first.markdown, second.markdown);
    assert.equal(first.errors.join("\n"), second.errors.join("\n"));
  });

  test("keeps a generated pull out of the fallback association", () => {
    const built = buildNotes({
      version: "4.2.0",
      packageName: "benes",
      distTag: "latest",
      repository: "testhost/app",
      tags: ["v4.1.0"],
      generatedNotes: [
        "### Features",
        "* Add local routing board by @river in https://github.com/testhost/app/pull/501",
      ].join("\n"),
      commits: [
        commit({
          sha: SHA_A,
          subject: "feat: add local routing board (#501)",
          pulls: [
            {
              number: 501,
              title: "feat: add local routing board",
              author: "river",
              labels: ["enhancement"],
              merged: true,
            },
          ],
        }),
      ],
    });
    assert.equal(built.errors.length, 0, built.errors.join("\n"));
    assert.equal(built.changes.filter((change) => change.kind === "pr" && change.number === 501).length, 1);
  });

  test("carries a bounded short sha and commit url for a direct commit", () => {
    const built = buildNotes({
      version: "4.2.0",
      packageName: "benes",
      distTag: "latest",
      repository: "testhost/app",
      tags: ["v4.1.0"],
      commits: [commit({ sha: SHA_B, subject: "fix: honor empty quota remaining" })],
    });
    assert.match(built.markdown, new RegExp(`\\[bbbbbbbb\\]\\(https://github\\.com/testhost/app/commit/${SHA_B}\\)`));
  });

  test("only counts reachable tags when an ancestry predicate is supplied", () => {
    const tags = ["v4.1.0", "v4.1.1"];
    assert.equal(selectNotesBaseline("4.2.0", tags), "v4.1.1");
    assert.equal(selectNotesBaseline("4.2.0", tags, (tag) => tag !== "v4.1.1"), "v4.1.0");
    assert.equal(selectNotesBaseline("4.2.0", tags, () => false), null);
  });

  test("refuses a non-array associated-pull payload", () => {
    assert.throws(() => parseAssociatedPulls({ number: 1 }), /non-array JSON/);
  });

  test("skips unusable associated-pull rows and defaults the author", () => {
    const pulls = parseAssociatedPulls([
      null,
      "not-an-object",
      { number: "501", title: "wrong number type" },
      { number: 502 },
      { number: 503, title: "no user" },
      { number: 504, title: "merged", merged_at: "2026-01-01T00:00:00Z", user: { login: "river" }, labels: [{ name: "bug" }, {}] },
      { number: 505, title: "unmerged", merged_at: null, user: null },
    ]);
    assert.deepEqual(pulls, [
      { number: 503, title: "no user", author: "unknown", labels: [], merged: false },
      { number: 504, title: "merged", author: "river", labels: ["bug"], merged: true },
      { number: 505, title: "unmerged", author: "unknown", labels: [], merged: false },
    ]);
  });
});
