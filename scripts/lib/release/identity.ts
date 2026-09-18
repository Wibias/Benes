import {
  assertVersionForChannel,
  distTagForChannel,
  gitTagForVersion,
  parsePublishVersion,
  releaseChannel,
  ReleaseError,
  type DistTag,
  type ReleaseChannel,
} from "./semver.ts";

export type PublishableBranch = "main" | "preview";

export type ReleaseIdentity = {
  packageName: string;
  version: string;
  gitTag: string;
  distTag: DistTag;
  channel: ReleaseChannel;
  sourceSha: string;
  branch: PublishableBranch;
};

const FULL_SHA = /^[0-9a-f]{40}$/;
const PACKAGE_NAME = /^[a-z0-9][a-z0-9._-]*$/;
const REPOSITORY = /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/;

export function parseFullSha(value: string): string {
  const sha = value.trim();
  if (!FULL_SHA.test(sha)) {
    throw new ReleaseError(
      "invalid_sha",
      "expected-sha must be a 40-character lowercase hex commit",
    );
  }
  return sha;
}

export function parsePackageName(value: string): string {
  const name = value.trim();
  if (!PACKAGE_NAME.test(name)) {
    throw new ReleaseError("invalid_package", "package.json is missing a valid name");
  }
  return name;
}

export function parseRepository(value: string): string {
  const repo = value.trim();
  if (!REPOSITORY.test(repo)) {
    throw new ReleaseError("invalid_repository", `Invalid repository id: ${repo}`);
  }
  return repo;
}

export function branchFromRef(ref: string): PublishableBranch {
  if (ref === "main" || ref === "refs/heads/main") return "main";
  if (ref === "preview" || ref === "refs/heads/preview") return "preview";
  throw new ReleaseError(
    "invalid_branch",
    `Release is limited to main and preview (received ${ref || "nothing"})`,
  );
}

export function channelForBranch(branch: PublishableBranch): ReleaseChannel {
  return branch === "preview" ? "preview" : "stable";
}

export function resolveIdentity(input: {
  packageName: string;
  version: string;
  sourceSha: string;
  branch: string;
  distTag?: DistTag | null;
}): ReleaseIdentity {
  const branch = branchFromRef(input.branch);
  const channel = channelForBranch(branch);
  const version = parsePublishVersion(input.version).text;
  assertVersionForChannel(version, channel);
  const distTag = input.distTag ?? distTagForChannel(channel);
  if (distTag !== distTagForChannel(channel)) {
    throw new ReleaseError(
      "dist_tag_mismatch",
      `${branch} releases must use npm dist-tag '${distTagForChannel(channel)}' (got '${distTag}')`,
    );
  }
  return {
    packageName: parsePackageName(input.packageName),
    version,
    gitTag: gitTagForVersion(version),
    distTag,
    channel,
    sourceSha: parseFullSha(input.sourceSha),
    branch,
  };
}

export function assertPackageVersion(identity: ReleaseIdentity, packageVersion: string): void {
  if (packageVersion.trim() !== identity.version) {
    throw new ReleaseError(
      "package_version_mismatch",
      `package.json (${packageVersion}) does not match requested ${identity.version}`,
    );
  }
}
