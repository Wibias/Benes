import { branchFromRef, parseRepository, resolveIdentity, type ReleaseIdentity } from "./identity.ts";
import { selectNotesBaseline, type HistoryCommit } from "./notes.ts";
import {
  assemblePublicationPlan,
  assertDistTagAdvance,
  formatPlan,
  githubReleaseMismatch,
  type PublicationPlan,
} from "./plan.ts";
import { ReleaseError, type DistTag } from "./semver.ts";
import type { HistorySpan, PublicSnapshot, ReleaseWorld } from "./world.ts";

export type NpmIdentityProbe = {
  version: string | null;
  gitHead: string | null;
};

export type NpmIdentityClass = "missing" | "pending-gitHead" | "match" | "foreign";

export function classifyNpmIdentity(
  probe: NpmIdentityProbe,
  expected: { version: string; sourceSha: string },
): NpmIdentityClass {
  if (!probe.version || probe.version !== expected.version) return "missing";
  if (!probe.gitHead) return "pending-gitHead";
  if (probe.gitHead === expected.sourceSha) return "match";
  return "foreign";
}

export async function waitForPublishedIdentity(input: {
  read: () => Promise<NpmIdentityProbe>;
  version: string;
  sourceSha: string;
  attempts: number;
  sleep: () => Promise<void>;
  log?: (message: string) => void;
}): Promise<void> {
  const expected = { version: input.version, sourceSha: input.sourceSha };
  for (let attempt = 1; attempt <= input.attempts; attempt += 1) {
    const verdict = classifyNpmIdentity(await input.read(), expected);
    if (verdict === "match") return;
    if (verdict === "foreign") {
      throw new ReleaseError(
        "npm_githead_mismatch",
        `npm ${input.version} is published from a different gitHead than ${input.sourceSha}`,
      );
    }
    input.log?.(
      verdict === "pending-gitHead"
        ? `npm ${input.version} is visible; waiting for gitHead (${attempt}/${input.attempts})`
        : `npm ${input.version} not visible yet (${attempt}/${input.attempts})`,
    );
    if (attempt < input.attempts) await input.sleep();
  }
  throw new ReleaseError(
    "npm_verify_failed",
    `npm ${input.version} did not show gitHead ${input.sourceSha}`,
  );
}

export function ancestorFromExitCode(code: number): boolean {
  if (code === 0) return true;
  if (code === 1) return false;
  throw new ReleaseError("git_failed", `git merge-base --is-ancestor failed (exit ${code})`);
}

export async function readPublicSnapshot(
  world: ReleaseWorld,
  identity: ReleaseIdentity,
): Promise<PublicSnapshot> {
  const [npmHasVersion, npmGitHead, distTags, tagSha, githubRelease, ciPushUrl, serviceLifecycleUrl] =
    await Promise.all([
      world.npmHasVersion(identity.packageName, identity.version),
      world.npmGitHead(identity.packageName, identity.version),
      world.npmDistTags(identity.packageName),
      world.remoteTagSha(identity.gitTag),
      world.readGithubRelease(identity.gitTag),
      world.ciPushUrl(identity.sourceSha, identity.branch),
      world.serviceLifecycleUrl(identity.sourceSha),
    ]);
  return {
    npmHasVersion,
    npmGitHead,
    distTags,
    tagSha,
    githubRelease,
    ciPushUrl,
    serviceLifecycleUrl,
  };
}

export async function executePlan(
  plan: PublicationPlan,
  world: ReleaseWorld,
  dryRun: boolean,
): Promise<{ outcome: string }> {
  world.log(formatPlan(plan, dryRun));
  if (dryRun) {
    await world.preparePackage();
    await world.packDryRun();
    return { outcome: "dry-run" };
  }
  if (plan.intent.kind === "complete") {
    world.log("Public metadata already matches this SHA; nothing to mutate.");
    return { outcome: "already-complete" };
  }
  for (const mutation of plan.intent.mutations) {
    if (mutation === "stage-npm") {
      await world.stageNpm(plan.identity.distTag);
      world.log(
        `npm staged ${plan.identity.packageName}@${plan.identity.version}. Review and approve the staged package with 2FA, then rerun this exact release workflow to finalize the Git tag and GitHub Release.`,
      );
      return { outcome: "staged-awaiting-approval" };
    } else if (mutation === "create-tag") {
      await world.createTag(plan.identity.gitTag, plan.identity.sourceSha);
    } else if (mutation === "create-github-release") {
      await world.createGithubRelease({
        tag: plan.identity.gitTag,
        sha: plan.identity.sourceSha,
        notes: plan.notes.markdown,
        prerelease: plan.identity.channel === "preview",
      });
      const release = await world.readGithubRelease(plan.identity.gitTag);
      const mismatch = githubReleaseMismatch(plan.identity, release);
      if (mismatch) {
        throw new ReleaseError("github_release_mismatch", mismatch);
      }
    }
  }
  return { outcome: plan.intent.kind };
}

export async function runPublish(input: {
  world: ReleaseWorld;
  identity: ReleaseIdentity;
  repository: string;
  dryRun: boolean;
}): Promise<{ plan: PublicationPlan; outcome: string }> {
  const observed = await input.world.currentSha();
  if (observed !== input.identity.sourceSha) {
    throw new ReleaseError(
      "sha_mismatch",
      `The branch moved after the audit (expected ${input.identity.sourceSha}, got ${observed})`,
    );
  }
  const repository = parseRepository(input.repository);
  const snapshot = await readPublicSnapshot(input.world, input.identity);
  const tags = await input.world.listTags();
  const ancestry = new Map<string, boolean>();
  for (const tag of tags) {
    ancestry.set(tag, await input.world.isAncestor(tag, input.identity.sourceSha));
  }
  const isAncestor = (tag: string): boolean => ancestry.get(tag) === true;
  const baseline = selectNotesBaseline(input.identity.version, tags, isAncestor);
  const span: HistorySpan = { baseline, target: input.identity.sourceSha };
  const rows = await input.world.firstParentLog(span);
  const commits: HistoryCommit[] = [];
  for (const row of rows) {
    commits.push({ ...row, pulls: await input.world.associatedPulls(row.sha) });
  }
  let generatedNotes = "";
  try {
    generatedNotes = await input.world.generatedNotes({
      repository,
      gitTag: input.identity.gitTag,
      target: input.identity.sourceSha,
      baseline,
    });
  } catch {
    input.world.log("GitHub generate-notes is unavailable; notes continue from git history.");
  }
  const changedPaths = await input.world.changedFiles(span);
  const plan = assemblePublicationPlan({
    identity: input.identity,
    packageVersion: await input.world.packageVersion(),
    repository,
    snapshot,
    tags,
    commits,
    generatedNotes,
    changedPaths,
    isAncestor,
  });
  const { outcome } = await executePlan(plan, input.world, input.dryRun);
  return { plan, outcome };
}

export async function runCut(input: {
  world: ReleaseWorld;
  version: string;
  distTag: DistTag | null;
  publish: boolean;
  planOnly: boolean;
}): Promise<{ sha?: string; outcome: string }> {
  const branch = branchFromRef(await input.world.currentBranch());
  if (await input.world.workingTreeDirty()) {
    throw new ReleaseError("dirty_tree", "Working tree is not clean — commit or stash first.");
  }
  const packageName = await input.world.packageName();
  const placeholderSha = await input.world.currentSha();
  const identity = resolveIdentity({
    packageName,
    version: input.version,
    sourceSha: placeholderSha,
    branch,
    distTag: input.distTag,
  });
  input.world.log(
    [
      `cut ${identity.packageName}@${identity.version}`,
      `branch ${identity.branch}`,
      `dist-tag ${identity.distTag}`,
      `workflow dry-run ${!input.publish}`,
    ].join("\n"),
  );
  if (input.planOnly) {
    return { outcome: "plan-only" };
  }

  const alreadyAtVersion = (await input.world.packageVersion()) === identity.version;
  const [npmHasVersion, tagSha, githubRelease, distTags] = await Promise.all([
    input.world.npmHasVersion(identity.packageName, identity.version),
    input.world.remoteTagSha(identity.gitTag),
    input.world.readGithubRelease(identity.gitTag),
    input.world.npmDistTags(identity.packageName),
  ]);
  const approvedStageReadyToFinalize =
    alreadyAtVersion && npmHasVersion && !tagSha && !githubRelease;
  if ((npmHasVersion || tagSha || githubRelease) && !approvedStageReadyToFinalize) {
    const reasons = [
      npmHasVersion ? `npm already has ${identity.packageName}@${identity.version}` : "",
      tagSha ? `remote tag ${identity.gitTag} exists at ${tagSha}` : "",
      githubRelease ? `GitHub Release ${identity.gitTag} already exists` : "",
    ].filter(Boolean);
    throw new ReleaseError(
      "already_used",
      `${reasons.join("\n")}\nFor an approved staged package on this exact branch version, rerun the release command to finalize GitHub metadata.`,
    );
  }
  if (approvedStageReadyToFinalize) {
    input.world.log(
      `npm already has approved ${identity.packageName}@${identity.version}; dispatching recovery to create the audited Git tag and GitHub Release`,
    );
  }
  assertDistTagAdvance(identity, distTags, !npmHasVersion);

  await input.world.runLocalChecks();
  let sha: string;
  if (alreadyAtVersion) {
    input.world.log(
      `package.json is already ${identity.version}; dispatching on the current SHA instead of cutting another bump`,
    );
    sha = await input.world.currentSha();
  } else {
    await input.world.bumpPackageVersion(identity.version);
    sha = await input.world.commitAndPush({
      version: identity.version,
      branch: identity.branch,
    });
  }
  await input.world.waitForWorkflow(sha, "ci.yml", "Cross-platform CI");
  const live = await input.world.remoteHead(identity.branch);
  if (live !== sha) {
    throw new ReleaseError(
      "branch_moved",
      `origin/${identity.branch} moved while waiting for CI (${live} != ${sha})`,
    );
  }
  await input.world.dispatchRelease({
    version: identity.version,
    distTag: identity.distTag,
    sha,
    branch: identity.branch,
    dryRun: !input.publish,
  });
  await input.world.watchDispatchedRelease(sha, identity.branch);
  return { sha, outcome: input.publish ? "dispatched-stage-or-finalize" : "dispatched-dry-run" };
}
