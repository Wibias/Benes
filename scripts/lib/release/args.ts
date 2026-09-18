import { parseFullSha } from "./identity.ts";
import { parsePublishVersion, ReleaseError, type DistTag } from "./semver.ts";

export type CutCommand = {
  kind: "cut";
  version: string;
  distTag: DistTag | null;
  publish: boolean;
  planOnly: boolean;
};

export type PublishCommand = {
  kind: "publish";
  version: string;
  distTag: DistTag;
  expectedSha: string;
  dryRun: boolean;
};

export type CliCommand = { kind: "watch" } | CutCommand | PublishCommand;

function flagValue(argv: string[], name: string): string | null {
  const index = argv.indexOf(name);
  if (index < 0) return null;
  const value = argv[index + 1];
  if (!value || value.startsWith("--")) {
    throw new ReleaseError("invalid_args", `Missing value for ${name}`);
  }
  return value;
}

function parseDistTag(value: string | null): DistTag | null {
  if (value === null) return null;
  if (value !== "latest" && value !== "preview") {
    throw new ReleaseError("invalid_args", "`--tag` must be latest or preview.");
  }
  return value;
}

function env(name: string, source: NodeJS.ProcessEnv): string {
  return source[name]?.trim() ?? "";
}

function parseBoolean(value: string | undefined, fallback: boolean): boolean {
  if (value === undefined || value === "") return fallback;
  if (value === "true" || value === "1") return true;
  if (value === "false" || value === "0") return false;
  throw new ReleaseError("invalid_args", `Expected true/false, got ${value}`);
}

export function parseReleaseArgv(
  argv: string[],
  source: NodeJS.ProcessEnv = process.env,
): CliCommand {
  if (argv[0] === "watch") return { kind: "watch" };

  if (argv[0] === "publish") {
    const version = flagValue(argv, "--version") || env("RELEASE_VERSION", source);
    const tag = parseDistTag(flagValue(argv, "--tag") || env("NPM_DIST_TAG", source) || null);
    const expectedSha =
      flagValue(argv, "--expected-sha") || env("EXPECTED_SHA", source) || env("GITHUB_SHA", source);
    const dryFlag = argv.includes("--dry-run")
      ? true
      : argv.includes("--publish")
        ? false
        : parseBoolean(env("RELEASE_DRY_RUN", source) || undefined, true);
    if (!version || !tag || !expectedSha) {
      throw new ReleaseError(
        "invalid_args",
        "publish requires --version, --tag, and --expected-sha (or RELEASE_VERSION, NPM_DIST_TAG, EXPECTED_SHA)",
      );
    }
    return {
      kind: "publish",
      version: parsePublishVersion(version).text,
      distTag: tag,
      expectedSha: parseFullSha(expectedSha),
      dryRun: dryFlag,
    };
  }

  const rest = argv[0] === "cut" ? argv.slice(1) : argv;
  const version = rest[0];
  if (!version || version.startsWith("--")) {
    throw new ReleaseError(
      "invalid_args",
      "Usage: release <version> [--tag latest|preview] [--publish] [--plan-only]\n       release publish --version <version> --tag <latest|preview> --expected-sha <sha> [--dry-run]\n       release watch",
    );
  }
  return {
    kind: "cut",
    version: parsePublishVersion(version).text,
    distTag: parseDistTag(flagValue(rest, "--tag")),
    publish: rest.includes("--publish"),
    planOnly: rest.includes("--plan-only"),
  };
}
