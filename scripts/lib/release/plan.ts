import { assertPackageVersion, type ReleaseIdentity } from "./identity.ts";
import { buildNotes, type HistoryCommit, type NotesModel } from "./notes.ts";
import { compareReleaseVersions, ReleaseError } from "./semver.ts";
import type { GithubReleaseState, PublicSnapshot } from "./world.ts";

export type MutationKind = "stage-npm" | "create-tag" | "create-github-release";

export type PlanIntent =
  | { kind: "fresh"; mutations: MutationKind[] }
  | { kind: "recover"; mutations: MutationKind[] }
  | { kind: "complete"; mutations: [] }
  | { kind: "conflict"; reasons: string[] };

export type PublicationPlan = {
  identity: ReleaseIdentity;
  repository: string;
  notes: NotesModel;
  intent: PlanIntent;
  ciPushUrl: string | null;
  serviceLifecycleRequired: boolean;
  serviceLifecycleUrl: string | null;
};

const SERVICE_FILES = new Set([
  "cmd/benes/service.go",
  "cmd/benes/service_launchd.go",
  "cmd/benes/service_native.go",
  "cmd/benes/stop.go",
  "go.mod",
  "go.sum",
  ".github/workflows/service-lifecycle.yml",
]);

const SERVICE_PREFIXES = ["internal/servicectl/", "internal/winsw/"];

export function serviceFilesChanged(paths: string[]): boolean {
  return paths.some((path) => {
    const normalized = path.replace(/\\/g, "/");
    return SERVICE_FILES.has(normalized) || SERVICE_PREFIXES.some((prefix) => normalized.startsWith(prefix));
  });
}

export function githubReleaseMismatch(
  identity: ReleaseIdentity,
  release: GithubReleaseState | null,
): string | null {
  if (!release) return "GitHub Release is missing";
  if (release.tag !== identity.gitTag) {
    return `GitHub Release tag ${JSON.stringify(release.tag)} does not match ${identity.gitTag}`;
  }
  if (release.draft) {
    return `GitHub Release ${release.tag} is still a draft`;
  }
  if (identity.channel === "preview" && !release.prerelease) {
    return `Preview GitHub Release ${release.tag} must be marked prerelease`;
  }
  if (identity.channel === "stable" && release.prerelease) {
    return `Stable GitHub Release ${release.tag} must not be marked prerelease`;
  }
  return null;
}

export function classifyPublic(identity: ReleaseIdentity, snapshot: PublicSnapshot): PlanIntent {
  if (snapshot.tagSha && snapshot.tagSha !== identity.sourceSha) {
    return {
      kind: "conflict",
      reasons: [`${identity.gitTag} already points at ${snapshot.tagSha}, not ${identity.sourceSha}`],
    };
  }
  if (snapshot.npmHasVersion) {
    if (!snapshot.npmGitHead) {
      return {
        kind: "conflict",
        reasons: [
          `npm already has ${identity.packageName}@${identity.version}, but gitHead is missing so this SHA cannot be proven`,
        ],
      };
    }
    if (snapshot.npmGitHead !== identity.sourceSha) {
      return {
        kind: "conflict",
        reasons: [
          `npm ${identity.packageName}@${identity.version} was published from ${snapshot.npmGitHead}, not ${identity.sourceSha}`,
        ],
      };
    }
  }
  if (snapshot.githubRelease) {
    const mismatch = githubReleaseMismatch(identity, snapshot.githubRelease);
    if (mismatch) {
      return { kind: "conflict", reasons: [mismatch] };
    }
  }
  if (snapshot.githubRelease && !snapshot.npmHasVersion && !snapshot.tagSha) {
    return {
      kind: "conflict",
      reasons: [
        `GitHub Release ${identity.gitTag} exists without npm ${identity.packageName}@${identity.version}`,
      ],
    };
  }

  const mutations: MutationKind[] = [];
  if (!snapshot.npmHasVersion) mutations.push("stage-npm");
  if (!snapshot.tagSha) mutations.push("create-tag");
  if (!snapshot.githubRelease) mutations.push("create-github-release");

  if (mutations.length === 0) return { kind: "complete", mutations: [] };
  if (snapshot.npmHasVersion || snapshot.tagSha || snapshot.githubRelease) {
    return { kind: "recover", mutations };
  }
  return { kind: "fresh", mutations };
}

export function assertDistTagAdvance(
  identity: ReleaseIdentity,
  distTags: Record<string, string>,
  willPublishNpm: boolean,
): void {
  if (!willPublishNpm) return;
  const current = distTags[identity.distTag];
  if (!current) return;
  let forward: number;
  try {
    forward = compareReleaseVersions(identity.version, current);
  } catch (error) {
    throw new ReleaseError(
      "dist_tag_unreadable",
      `Cannot compare ${identity.version} with ${identity.distTag} tip ${JSON.stringify(current)}: ${
        error instanceof Error ? error.message : String(error)
      }`,
    );
  }
  if (forward <= 0) {
    throw new ReleaseError(
      "dist_tag_regression",
      `Version ${identity.version} does not move dist-tag '${identity.distTag}' forward (current: ${current})`,
    );
  }
}

export function assertCiEvidence(input: {
  ciPushUrl: string | null;
  serviceLifecycleRequired: boolean;
  serviceLifecycleUrl: string | null;
  sourceSha: string;
  branch: string;
}): void {
  if (!input.ciPushUrl) {
    throw new ReleaseError(
      "ci_evidence_missing",
      `No successful Cross-platform CI (ci.yml, push event) for ${input.sourceSha} on ${input.branch}. Hosted Actions are the release gate; a pull-request run or local ci:pr is not a substitute.`,
    );
  }
  if (input.serviceLifecycleRequired && !input.serviceLifecycleUrl) {
    throw new ReleaseError(
      "service_lifecycle_missing",
      `Service-control files changed, but no successful service-lifecycle.yml run exists for ${input.sourceSha}.`,
    );
  }
}

export function assertNotes(notes: NotesModel): void {
  if (notes.errors.length > 0) {
    throw new ReleaseError("notes_coverage", notes.errors.join("\n"));
  }
  if (!notes.markdown.trim()) {
    throw new ReleaseError("notes_empty", "Release notes were empty");
  }
}

export function formatPlan(plan: PublicationPlan, dryRun: boolean): string {
  const mutations =
    plan.intent.kind === "conflict" ? "none (conflict)" : plan.intent.mutations.join(", ") || "none";
  return [
    `package: ${plan.identity.packageName}@${plan.identity.version}`,
    `git tag: ${plan.identity.gitTag}`,
    `dist-tag: ${plan.identity.distTag}`,
    `source SHA: ${plan.identity.sourceSha}`,
    `branch: ${plan.identity.branch}`,
    `intent: ${plan.intent.kind}${plan.intent.kind === "conflict" ? `\n${plan.intent.reasons.join("\n")}` : ""}`,
    `mutations: ${mutations}`,
    `dry-run: ${dryRun}`,
    `notes baseline: ${plan.notes.baseline ?? "(initial)"}`,
  ].join("\n");
}

export function assemblePublicationPlan(input: {
  identity: ReleaseIdentity;
  packageVersion: string;
  repository: string;
  snapshot: PublicSnapshot;
  tags: string[];
  commits: HistoryCommit[];
  generatedNotes: string;
  changedPaths: string[];
  isAncestor: (tag: string) => boolean;
}): PublicationPlan {
  assertPackageVersion(input.identity, input.packageVersion);
  const intent = classifyPublic(input.identity, input.snapshot);
  if (intent.kind === "conflict") {
    throw new ReleaseError("public_conflict", intent.reasons.join("\n"));
  }
  const serviceLifecycleRequired = serviceFilesChanged(input.changedPaths);
  assertCiEvidence({
    ciPushUrl: input.snapshot.ciPushUrl,
    serviceLifecycleRequired,
    serviceLifecycleUrl: input.snapshot.serviceLifecycleUrl,
    sourceSha: input.identity.sourceSha,
    branch: input.identity.branch,
  });
  assertDistTagAdvance(
    input.identity,
    input.snapshot.distTags,
    intent.mutations.includes("stage-npm"),
  );
  const notes = buildNotes({
    version: input.identity.version,
    packageName: input.identity.packageName,
    distTag: input.identity.distTag,
    repository: input.repository,
    tags: input.tags,
    commits: input.commits,
    generatedNotes: input.generatedNotes,
    isAncestor: input.isAncestor,
  });
  assertNotes(notes);
  return {
    identity: input.identity,
    repository: input.repository,
    notes,
    intent,
    ciPushUrl: input.snapshot.ciPushUrl,
    serviceLifecycleRequired,
    serviceLifecycleUrl: input.snapshot.serviceLifecycleUrl,
  };
}
