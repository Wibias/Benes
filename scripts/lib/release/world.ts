import type { AssociatedPull, HistoryCommit } from "./notes.ts";
import type { DistTag } from "./semver.ts";

export type GithubReleaseState = {
  tag: string;
  draft: boolean;
  prerelease: boolean;
};

export type HistorySpan = {
  baseline: string | null;
  target: string;
};

export type PublicSnapshot = {
  npmHasVersion: boolean;
  npmGitHead: string | null;
  distTags: Record<string, string>;
  tagSha: string | null;
  githubRelease: GithubReleaseState | null;
  ciPushUrl: string | null;
  serviceLifecycleUrl: string | null;
};

export type ReleaseWorld = {
  packageName(): Promise<string>;
  packageVersion(): Promise<string>;
  currentBranch(): Promise<string>;
  currentSha(): Promise<string>;
  workingTreeDirty(): Promise<boolean>;
  remoteHead(branch: string): Promise<string>;
  listTags(): Promise<string[]>;
  isAncestor(tag: string, sha: string): Promise<boolean>;
  firstParentLog(span: HistorySpan): Promise<Array<Omit<HistoryCommit, "pulls">>>;
  associatedPulls(sha: string): Promise<AssociatedPull[]>;
  generatedNotes(input: {
    repository: string;
    gitTag: string;
    target: string;
    baseline: string | null;
  }): Promise<string>;
  changedFiles(span: HistorySpan): Promise<string[]>;
  npmHasVersion(name: string, version: string): Promise<boolean>;
  npmGitHead(name: string, version: string): Promise<string | null>;
  npmDistTags(name: string): Promise<Record<string, string>>;
  waitUntilPublishedIdentity(input: {
    name: string;
    version: string;
    sourceSha: string;
  }): Promise<void>;
  remoteTagSha(tag: string): Promise<string | null>;
  readGithubRelease(tag: string): Promise<GithubReleaseState | null>;
  ciPushUrl(sha: string, branch: string): Promise<string | null>;
  serviceLifecycleUrl(sha: string): Promise<string | null>;
  waitForWorkflow(sha: string, workflow: string, label: string): Promise<string>;
  runLocalChecks(): Promise<void>;
  bumpPackageVersion(version: string): Promise<void>;
  commitAndPush(input: { version: string; branch: string }): Promise<string>;
  dispatchRelease(input: {
    version: string;
    distTag: DistTag;
    sha: string;
    branch: string;
    dryRun: boolean;
  }): Promise<void>;
  watchDispatchedRelease(sha: string, branch: string): Promise<void>;
  preparePackage(): Promise<void>;
  packDryRun(): Promise<void>;
  publishNpm(distTag: DistTag): Promise<void>;
  createTag(tag: string, sha: string): Promise<void>;
  createGithubRelease(input: {
    tag: string;
    sha: string;
    notes: string;
    prerelease: boolean;
  }): Promise<void>;
  log(message: string): void;
};
