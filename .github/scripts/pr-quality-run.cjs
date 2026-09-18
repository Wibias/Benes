"use strict";

const path = require("node:path");
const fs = require("node:fs");
const {
  gatherQualityProblems,
  hasRepoPush,
  maintainerNegatedGui,
  incompleteFileList,
  inspectReadiness,
  ensureReadiness,
  dropReadiness,
  clearClaimTicks,
  restoreBlankReadiness,
  INTEGRATION_BASES,
  DEFAULT_INTEGRATION,
} = require("./pr-quality.cjs");
const { hygieneFailures, HYGIENE_HINTS } = require("./pr-hygiene.cjs");
const { loadGuiTestContentsAtHead, readFileFromContents } = require("./pr-hygiene-head-files.cjs");
const {
  decodeGateRecord,
  decodeEnforcerRecord,
  decodeReadinessRecord,
  blankGateRecord,
  blendLegacyRecords,
  openReviewFindings,
  GATE_HTML,
  LEGACY_READINESS_HTML,
  HYGIENE_HTML,
  HYGIENE_OPEN,
  HYGIENE_CLOSE,
  wrapInline,
  composeGateBody,
  readHygienePayload,
  summarizeFailures,
  renderRevalidationNotice,
  waiverNote,
  actionLines,
  failureReasonLine,
  qualityDraftNotices,
  checklistDraftNotices,
  readyNotices,
  unreadFindings,
  threadAuthors,
} = require("./pr-quality-reconcile.cjs");
const { planQualityGate, botDraftOwnershipAfterExecute } = require("./pr-quality-plan.cjs");
const { readMaintainerGitHubLogins } = require("./pr-maintainers.cjs");
const githubApi = require("./pr-quality-github.cjs");
const facts = require("./pr-quality-facts.cjs");
const {
  WRONG_BASE_PREFIX,
  REVIEW_READY,
  GUI_WAIVER_LABEL,
  LEGACY_WRONG_BRANCH_HTML,
} = require("./pr-quality-frozen.cjs");

const MAINTAINERS_FILE = "MAINTAINERS.md";

function labelFingerprint(pr) {
  const names = (pr?.labels ?? [])
    .map((label) => (typeof label === "string" ? label : label?.name || ""))
    .filter(Boolean);
  return [...new Set(names)].sort().join("\n");
}

function prFingerprint(pr) {
  return {
    headSha: pr?.head?.sha || "",
    body: pr?.body ?? "",
    title: pr?.title || "",
    draft: Boolean(pr?.draft),
    baseRef: pr?.base?.ref || "",
    labels: labelFingerprint(pr),
  };
}

function fingerprintsDiffer(observed, live) {
  return (
    observed.headSha !== live.headSha ||
    observed.body !== live.body ||
    observed.title !== live.title ||
    observed.draft !== live.draft ||
    observed.baseRef !== live.baseRef ||
    observed.labels !== live.labels
  );
}

async function readLivePrFingerprint(github, { owner, repo, pull_number }) {
  const { data } = await github.rest.pulls.get({ owner, repo, pull_number });
  return { pr: data, fingerprint: prFingerprint(data) };
}

module.exports = {
  prFingerprint,
  fingerprintsDiffer,
  readLivePrFingerprint,
  botDraftOwnershipAfterExecute,
  async runPrQualityGate({ github, context, core }) {
    const { owner, repo } = context.repo;
    const pull_number = facts.parsePullNumber(process.env.RESOLVED_PULL_NUMBER);
    if (pull_number == null) {
      core.info("No pull request could be resolved for this gate event; skipping.");
      return;
    }
    if (!facts.acceptedWriteEvent(context.eventName)) {
      core.info(`Unsupported gate event ${context.eventName}; skipping.`);
      return;
    }

    const { pr, comments } = await facts.loadPrBundle({ github, owner, repo, pull_number });
    let gateComment = comments.find(
      (comment) => comment.user?.login === "github-actions[bot]" && comment.body?.includes(GATE_HTML),
    );
    let gateCommentId = gateComment?.id ?? null;
    const legacyEnforcerComment = facts.legacyEnforcerNote(comments, LEGACY_WRONG_BRANCH_HTML);
    const legacyReadinessComment = facts.botNote(comments, LEGACY_READINESS_HTML);
    const stored = facts.pickGateRecord({
      gateComment,
      legacyEnforcerComment,
      legacyReadinessComment,
      decodeGateRecord,
      decodeEnforcerRecord,
      decodeReadinessRecord,
      blendLegacyRecords,
      warn: (message) => core.warning(message),
    });

    const maintainerLogins = facts.readTrustedMaintainers({
      fs,
      path,
      cwd: process.cwd(),
      fileName: MAINTAINERS_FILE,
      readMaintainerLogins: readMaintainerGitHubLogins,
      core,
    });
    const { authorPermission, permissionLookupFailed } = await facts.lookupPushLevel({
      github,
      owner,
      repo,
      username: pr.user.login,
      core,
    });

    let stackedOnParent = false;
    let behindMain = 0;
    let behindBase = 0;
    let aheadMain = 0;
    let ancestryLookupFailed = false;
    if (!INTEGRATION_BASES.includes(pr.base.ref)) {
      stackedOnParent = await facts.stackedOnParent({
        github,
        owner,
        repo,
        pull_number,
        pr,
        core,
        listOpenPrs: (args) => github.paginate(github.rest.pulls.list, args),
      });
    } else {
      const ancestry = await facts.compareAncestry({ github, owner, repo, pr, core });
      behindMain = ancestry.behindMain;
      behindBase = ancestry.behindBase;
      aheadMain = ancestry.aheadMain;
      ancestryLookupFailed = ancestry.ancestryLookupFailed;
    }

    const { changedFiles, changedFilePaths, filesTruncated } = await facts.snapshotChangedFiles({
      github,
      owner,
      repo,
      pull_number,
      core,
      getPull: (...args) => github.rest.pulls.get(...args),
      listFiles: github.rest.pulls.listFiles,
      incompleteFileList,
    });

    let failures = gatherQualityProblems({
      baseRef: pr.base.ref,
      headRef: pr.head.ref,
      sameRepository:
        Boolean(pr.head.repo?.full_name) &&
        pr.head.repo.full_name === pr.base.repo?.full_name,
      allowedBases: INTEGRATION_BASES,
      body: pr.body,
      behindMain,
      behindBase,
      aheadMain,
      authorPermission,
      permissionLookupFailed,
      ancestryLookupFailed,
      stackedOnParent,
      guiOverrideComments: comments,
      changedFilePaths,
      filesTruncated,
    });
    const labelNames = (pr.labels ?? []).map((label) => label.name);
    const guiTestContents = await loadGuiTestContentsAtHead({
      github,
      owner,
      repo,
      files: changedFiles,
      warn: (message) => core.warning(message),
    });
    failures = [
      ...failures,
      ...hygieneFailures({
        files: changedFiles,
        labels: labelNames,
        authorHasPushPermission: !permissionLookupFailed && hasRepoPush(authorPermission),
        readFile: readFileFromContents(guiTestContents),
      }),
    ];

    const screenshotWaiverLabelPresent = (pr.labels ?? []).some((label) => label.name === GUI_WAIVER_LABEL);
    let screenshotWaiverLabelActorLogin = null;
    if (screenshotWaiverLabelPresent) {
      screenshotWaiverLabelActorLogin = await facts.latestWaiverActor({
        github,
        owner,
        repo,
        pull_number,
        label: GUI_WAIVER_LABEL,
        core,
      });
    }
    const screenshotWaivedByLabel = facts.labelWaiverHolds({
      present: screenshotWaiverLabelPresent,
      actorLogin: screenshotWaiverLabelActorLogin,
      maintainerLogins: new Set(maintainerLogins.map((login) => login.toLowerCase())),
    });
    if (screenshotWaivedByLabel) {
      failures = failures.filter((failure) => failure.code !== "missing_ui_screenshot");
    }
    const screenshotWaived = screenshotWaivedByLabel || maintainerNegatedGui({ comments });

    const authorHasWrite = !permissionLookupFailed && hasRepoPush(authorPermission);
    let readiness = inspectReadiness(pr.body);
    let findings = { code: null, unresolved: 0, byBot: {} };
    let threadsUnreadable = false;
    if (authorHasWrite === false && readiness.complete && failures.length === 0) {
      try {
        const threads = await githubApi.pageReviewThreads(github, { owner, repo, number: pull_number });
        const reviews = await githubApi.pageReviews(github, { owner, repo, pull_number });
        findings = openReviewFindings({
          threads: threadAuthors(threads),
          reviews,
          liveHeadSha: pr.head.sha,
        });
      } catch (error) {
        core.warning(`Could not list review threads for the readiness claim check: ${error.message}`);
        threadsUnreadable = true;
        findings = unreadFindings();
      }
    }

    const observed = prFingerprint(pr);
    const plan = planQualityGate({
      title: pr.title,
      draft: pr.draft,
      headSha: pr.head.sha,
      eventHeadSha: facts.eventHead({
        payload: context.payload,
        eventName: context.eventName,
        liveHeadSha: pr.head.sha,
      }),
      eventAction: context.payload.action,
      authorHasWrite,
      failures,
      readiness,
      stored,
      findings,
      threadsUnreadable,
      behindBase,
      behindUnknown: ancestryLookupFailed,
    });

    const live = await readLivePrFingerprint(github, { owner, repo, pull_number });
    if (fingerprintsDiffer(observed, live.fingerprint)) {
      core.info("Pull request changed after observation; skipping mutations so the next event can retry.");
      return { skipped: true, reason: "stale_pr" };
    }

    async function dropStaleBotNotesIfNeeded() {
      await githubApi.dropStaleBotNotes({
        owner,
        repo,
        ids: [legacyEnforcerComment?.id, legacyReadinessComment?.id],
        gateCommentId,
        deleteComment: (...args) => github.rest.issues.deleteComment(...args),
        core,
      });
    }

    async function putGateComment(state, opts) {
      let body = composeGateBody(state, opts).join("\n");
      body = githubApi.keepHygieneFromPrior(body, gateComment?.body, {
        readHygienePayload,
        HYGIENE_OPEN,
        HYGIENE_HTML,
        HYGIENE_CLOSE,
      });
      if (gateCommentId && gateComment?.body === body) {
        await dropStaleBotNotesIfNeeded();
        return;
      }
      const written = await githubApi.putIssueComment({
        github,
        owner,
        repo,
        issue_number: pull_number,
        comment_id: gateCommentId,
        body,
      });
      gateCommentId = written.id;
      gateComment = { id: gateCommentId, body };
      await dropStaleBotNotesIfNeeded();
    }

    let body = pr.body ?? "";
    if (plan.injectChecklist) body = ensureReadiness(body);
    if (plan.dropChecklist) body = dropReadiness(body);
    if (plan.resetChecklist) body = restoreBlankReadiness(body);
    if (plan.untickClaims.length) body = clearClaimTicks(body, plan.untickClaims);
    if (body !== (pr.body ?? "")) {
      await github.rest.pulls.update({ owner, repo, pull_number, body });
    }
    readiness = inspectReadiness(body);

    if (plan.stripOwnedPrefix) {
      await github.rest.pulls.update({
        owner,
        repo,
        pull_number,
        title: pr.title.slice(WRONG_BASE_PREFIX.length),
      });
    }
    if (plan.prefixTitle) {
      await github.rest.pulls.update({
        owner,
        repo,
        pull_number,
        title: `${WRONG_BASE_PREFIX}${pr.title}`,
      });
    }

    const hasLabel = (pr.labels ?? []).some((label) => label.name === REVIEW_READY);
    if (plan.wantReadyLabel !== hasLabel) {
      await githubApi.syncReviewReady({
        owner,
        repo,
        issue_number: pull_number,
        shouldHave: plan.wantReadyLabel,
        hasLabel,
        label: REVIEW_READY,
        addLabels: (...args) => github.rest.issues.addLabels(...args),
        removeLabel: (...args) => github.rest.issues.removeLabel(...args),
        core,
        wrapInline,
      });
    }

    const screenshotWaiverNotice = waiverNote({
      waived: screenshotWaived,
      byLabel: screenshotWaivedByLabel,
      label: GUI_WAIVER_LABEL,
    });
    const revalidationNotice = renderRevalidationNotice({
      staleHead: plan.staleHead,
      completionHeadSha: stored.completedAtHeadSha,
      liveHeadSha: pr.head.sha,
      eventAction: context.payload.action,
      freshness: plan.freshness,
      findings,
      threadsUnreadable,
    });
    const actions = actionLines({
      failures: plan.failures,
      DEFAULT_INTEGRATION,
      HYGIENE_HINTS,
      checklistRequired: plan.contributor,
      checklistComplete: readiness.present && readiness.complete,
      readiness,
      revalidationNotice,
    });

    let convertDraftErrored = false;
    let readyConversionFailed = false;
    let readyConverted = false;
    if (plan.convertToDraft) {
      try {
        await githubApi.convertDraft(github, pr.node_id);
      } catch (error) {
        convertDraftErrored = true;
        core.warning(`Could not convert pull request to draft: ${error.message}`);
      }
    }
    if (plan.markReady) {
      try {
        await githubApi.markReady(github, pr.node_id);
        readyConverted = true;
      } catch (error) {
        readyConversionFailed = true;
        core.warning(`Could not mark pull request ready for review: ${error.message}`);
      }
    }

    const state = {
      ...plan.nextState,
      autoDraftedByBot: botDraftOwnershipAfterExecute({
        plan,
        convertDraftErrored,
        readyConverted,
      }),
      titlePrefixedByBot: plan.prefixTitle || (plan.nextState.titlePrefixedByBot && !plan.stripOwnedPrefix),
    };

    if (plan.kind === "fail") {
      await putGateComment(state, {
        status: "DRAFT",
        statusReason: failureReasonLine(plan.failures, { pr, DEFAULT_INTEGRATION, HYGIENE_HINTS }),
        actions,
        readiness,
        checklistRequired: plan.contributor,
        notices: qualityDraftNotices({
          failureNotices: screenshotWaiverNotice ? [screenshotWaiverNotice, ...revalidationNotice] : revalidationNotice,
          wantsDraftConversion: plan.convertToDraft,
          convertDraftErrored,
          checklistRequired: plan.contributor,
          checklistComplete: readiness.present && readiness.complete,
          authorLogin: pr.user.login,
          DEFAULT_INTEGRATION,
        }),
      });
      core.setFailed(`PR quality gate failed: ${summarizeFailures(plan.failures, { pr })}`);
      return;
    }

    if (plan.kind === "hold") {
      await putGateComment(state, {
        status: "DRAFT",
        statusReason: `review readiness checklist open (${readiness.checked}/${readiness.total} boxes ticked).`,
        actions,
        readiness,
        checklistRequired: true,
        notices: checklistDraftNotices({
          revalidationNotice,
          screenshotWaiverNotice,
          conversionFailed: convertDraftErrored,
        }),
      });
      return;
    }

    if (plan.kind === "ready") {
      const maintainers = maintainerLogins.filter((login) => login !== pr.user.login);
      await putGateComment(state, {
        status: "READY",
        statusReason: "all PR quality gates passed; the review readiness checklist is complete.",
        actions: [],
        readiness,
        checklistRequired: true,
        notices: readyNotices({
          screenshotWaiverNotice,
          readyConversionFailed,
          readyConverted,
          contributorReady: true,
          REVIEW_READY,
          notified: plan.pingMaintainers,
          maintainers,
        }),
      });
      return;
    }

    if (plan.kind === "recover" || plan.kind === "clear") {
      await putGateComment(
        { ...state, active: readyConversionFailed },
        {
          status: "READY",
          statusReason: readyConversionFailed
            ? "ready-for-review conversion failed; will retry on the next run."
            : "all PR quality gates passed.",
          actions: [],
          readiness,
          checklistRequired: false,
          notices: screenshotWaiverNotice ? [screenshotWaiverNotice] : [],
        },
      );
      return;
    }

    core.info("All PR quality gates passed and there is no active bot state.");
  },
};
