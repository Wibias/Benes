"use strict";

const {
  GATE_HTML,
  HYGIENE_HTML,
  HYGIENE_OPEN,
  HYGIENE_CLOSE,
  CODEX_BOT,
  CODE_RABBIT_BOT,
  READINESS_BOXES,
  FRESHNESS_BEHIND_MAX,
} = require("./pr-quality-frozen.cjs");
const { encodeGateRecord } = require("./pr-quality-legacy-state.cjs");

const BOT_LABEL = {
  [CODEX_BOT]: "Codex",
  [CODE_RABBIT_BOT]: "CodeRabbit",
};

const HYGIENE_BLOCK_RE = new RegExp(
  `^[ \\t]*${HYGIENE_OPEN}[ \\t]*\\n([\\s\\S]*?)\\n[ \\t]*${HYGIENE_CLOSE}[ \\t]*$`,
  "m",
);

function longestTicks(text) {
  let longest = 0;
  let run = 0;
  for (const ch of text) {
    if (ch === "`") {
      run += 1;
      if (run > longest) longest = run;
    } else {
      run = 0;
    }
  }
  return longest;
}

function wrapInline(value) {
  const text = String(value);
  const delimiter = "`".repeat(longestTicks(text) + 1);
  return `${delimiter}${text}${delimiter}`;
}

function readHygienePayload(body) {
  if (typeof body !== "string") return null;
  const match = body.match(HYGIENE_BLOCK_RE);
  if (!match) return null;
  return match[1]
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line !== "" && line !== HYGIENE_HTML)
    .join("\n");
}

function writeHygienePayload(body, hygieneLines) {
  const base = typeof body === "string" ? body : "";
  const block = [HYGIENE_OPEN, HYGIENE_HTML, "", ...hygieneLines, "", HYGIENE_CLOSE].join("\n");
  if (HYGIENE_BLOCK_RE.test(base)) {
    return base.replace(HYGIENE_BLOCK_RE, block);
  }
  return `${base.replace(/\s+$/, "")}\n\n## Hygiene\n\n${block}\n`;
}

function composeGateBody(state, opts) {
  const {
    status,
    statusReason,
    actions = [],
    readiness,
    checklistRequired = true,
    notices = [],
    hygiene,
  } = opts;
  const complete = readiness?.present && readiness?.complete;
  const emoji = status === "READY" ? "✅" : "⏳";
  const lines = [
    GATE_HTML,
    encodeGateRecord(state),
    "",
    `## ${emoji} ${status}`,
    statusReason ? `- ${statusReason}` : "",
    "",
  ];
  if (actions.length > 0) {
    lines.push("## What to do", "", ...actions.map((line) => `- ${line}`), "");
  }
  if (checklistRequired && readiness?.present) {
    lines.push(
      "## Review readiness checklist",
      "",
      ...READINESS_BOXES.map((item, index) => {
        const ticked = readiness.items?.[index]?.checked;
        return `- ${ticked ? "✅" : "⬜"} ${item}`;
      }),
      "",
      complete
        ? "✅ **4/4** boxes ticked."
        : `**${readiness.checked}/${readiness.total}** boxes ticked.`,
      "",
    );
  }
  if (hygiene && hygiene.length > 0) {
    lines.push(
      "## Hygiene",
      "",
      HYGIENE_OPEN,
      HYGIENE_HTML,
      "",
      ...hygiene,
      "",
      HYGIENE_CLOSE,
      "",
    );
  }
  lines.push(...notices);
  return lines.filter((line) => line !== null && line !== undefined);
}

function summarizeFailures(failures, { pr }) {
  return failures
    .map((failure) => {
      if (failure.code === "wrong_base") return `wrong base (${pr.base.ref})`;
      if (failure.code === "wrong_ancestry") return "wrong ancestry";
      if (failure.code === "bad_description") return `bad description (${failure.reason})`;
      if (failure.code === "missing_ui_screenshot") return "missing UI screenshot";
      return failure.code;
    })
    .join("; ");
}

function renderRevalidationNotice({
  staleHead = false,
  completionHeadSha = null,
  liveHeadSha = "",
  eventAction = "",
  freshness = [],
  findings = { byBot: {} },
  threadsUnreadable = false,
} = {}) {
  const lines = [];
  const head = wrapInline(String(liveHeadSha).slice(0, 7));
  if (staleHead) {
    if (completionHeadSha) {
      lines.push(
        `Stored attestation ${wrapInline(String(completionHeadSha).slice(0, 7))} does not match live head ${head}.`,
      );
    } else if (eventAction === "synchronize") {
      lines.push(`Synchronize arrived with a completed checklist and no stored attestation. Live head is ${head}.`);
    } else {
      lines.push(`Checklist ticks predate live head ${head}.`);
    }
    lines.push("Checklist cleared for this head. Re-run local verification, then tick all four boxes again.");
  }
  if (freshness.includes("review_findings")) {
    if (threadsUnreadable) {
      lines.push(
        "The Codex/CodeRabbit findings claim could not be verified; the **Codex/CodeRabbit findings** box has been unticked. The PR stays a draft until review threads are readable again.",
      );
    } else {
      for (const [login, count] of Object.entries(findings.byBot || {})) {
        const label = BOT_LABEL[login] ?? login;
        lines.push(
          `${label} still has ${count} open finding${count === 1 ? "" : "s"}. The **Codex/CodeRabbit findings** box is now unticked.`,
        );
      }
      lines.push("Resolve those bot review threads, then tick the findings box again.");
    }
  }
  if (freshness.includes("latest_dev")) {
    lines.push(
      `This branch is more than ${FRESHNESS_BEHIND_MAX} commits behind ${wrapInline("dev")}. The **latest dev** box is now unticked.`,
    );
    lines.push("Rebase onto current dev, re-verify, then tick the boxes again.");
  }
  return lines;
}

function waiverNote({ waived, byLabel, label }) {
  if (!waived) return null;
  if (byLabel) return `Screenshot requirement waived via ${wrapInline(label)}.`;
  return "Screenshot requirement waived by a maintainer.";
}

function actionLines({
  failures,
  DEFAULT_INTEGRATION,
  HYGIENE_HINTS,
  checklistRequired,
  checklistComplete,
  readiness,
  revalidationNotice,
}) {
  const actions = [];
  if (failures.some((failure) => failure.code === "wrong_base")) {
    actions.push(
      `Point this PR at ${wrapInline(DEFAULT_INTEGRATION)}. That is the integration branch.`,
    );
  }
  if (failures.some((failure) => failure.code === "wrong_ancestry")) {
    actions.push(
      `Rebase onto current ${wrapInline(DEFAULT_INTEGRATION)} instead of opening from ${wrapInline("main")}.`,
    );
  }
  if (failures.some((failure) => failure.code === "bad_description")) {
    actions.push("Write a real **Summary** and **Verification** section.");
  }
  if (failures.some((failure) => failure.code === "missing_ui_screenshot")) {
    actions.push("Put a screenshot of the UI change in the description.");
  }
  for (const failure of failures) {
    const hint = HYGIENE_HINTS[failure.code];
    if (!hint) continue;
    const paths = failure.paths?.length
      ? ` Paths: ${failure.paths.map((item) => wrapInline(item)).join(", ")}.`
      : "";
    actions.push(`Fix **${failure.code}** — ${hint}${paths}`);
  }
  if (checklistRequired && !checklistComplete) {
    actions.push(
      `Mark all four readiness boxes when the work is done (now ${readiness.checked}/${readiness.total}).`,
    );
  }
  if (revalidationNotice.length > 0) actions.push(...revalidationNotice);
  return actions;
}

function failureReasonLine(failures, { pr, DEFAULT_INTEGRATION, HYGIENE_HINTS }) {
  return failures
    .map((failure) => {
      if (failure.code === "wrong_base") {
        return `base is ${pr.base.ref}; point it at ${wrapInline(DEFAULT_INTEGRATION)}.`;
      }
      if (failure.code === "wrong_ancestry") return "ancestry sits on released main; rebase onto current dev.";
      if (failure.code === "bad_description") return `description is incomplete (${failure.reason}).`;
      if (failure.code === "missing_ui_screenshot") return "screenshot required for this UI change.";
      if (HYGIENE_HINTS[failure.code]) return `hygiene: ${failure.code}.`;
      return failure.code;
    })
    .join(" ");
}

function qualityDraftNotices({
  failureNotices: notices,
  wantsDraftConversion,
  convertDraftErrored,
  checklistRequired,
  checklistComplete,
  authorLogin,
  DEFAULT_INTEGRATION,
}) {
  const tick =
    checklistRequired && !checklistComplete
      ? [
          `@${authorLogin} Tick the boxes after local CI is green, the branch is on current ${wrapInline(DEFAULT_INTEGRATION)}, and every correct Codex and CodeRabbit finding is resolved.`,
        ]
      : [];
  if (convertDraftErrored) {
    return [
      ...notices,
      "The gate could not convert this PR to draft (the token cannot change draft status). Convert it yourself. The `enforce-target` check stays red until the items above are fixed.",
    ];
  }
  if (wantsDraftConversion) {
    return [
      ...notices,
      "The gate is holding this PR in draft. After the items above are fixed, it can be marked ready again.",
      ...tick,
    ];
  }
  return [
    ...notices,
    "This PR was already a draft. It stays a draft after the items above are fixed.",
    ...tick,
  ];
}

function checklistDraftNotices({ revalidationNotice, screenshotWaiverNotice, conversionFailed }) {
  const notices = [
    ...revalidationNotice,
    ...(screenshotWaiverNotice ? [screenshotWaiverNotice] : []),
  ];
  if (conversionFailed) {
    notices.push(
      "Draft conversion failed. Put this PR in draft until every readiness box is ticked.",
    );
    return notices;
  }
  notices.push("This PR remains a draft until every readiness box is ticked.");
  return notices;
}

function readyNotices({
  screenshotWaiverNotice,
  readyConversionFailed,
  readyConverted,
  contributorReady,
  REVIEW_READY,
  notified,
  maintainers,
}) {
  return [
    screenshotWaiverNotice,
    readyConversionFailed
      ? "Ready-for-review conversion failed. Mark it ready yourself if it is still a draft."
      : readyConverted
        ? "Marked ready for review."
        : "Already ready for review.",
    contributorReady
      ? `The ${wrapInline(REVIEW_READY)} label marks this PR as ready; review automation runs independently.`
      : "",
    notified && maintainers.length > 0
      ? `Maintainers notified: ${maintainers.map((login) => `@${login}`).join(" ")}`
      : maintainers.length > 0
        ? `Maintainers: ${maintainers.map((login) => `@${login}`).join(" ")}`
        : "Maintainers will be notified.",
  ].filter(Boolean);
}

module.exports = {
  wrapInline,
  readHygienePayload,
  writeHygienePayload,
  composeGateBody,
  summarizeFailures,
  renderRevalidationNotice,
  waiverNote,
  actionLines,
  failureReasonLine,
  qualityDraftNotices,
  checklistDraftNotices,
  readyNotices,
};
