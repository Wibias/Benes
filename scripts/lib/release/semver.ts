/**
 * SemVer comparison plus the stricter Benes publication grammar.
 *
 * Comparison understands SemVer 2.0.0, including build metadata (ignored for
 * rank) so npm dist-tag tips can be compared. Publication identity is only:
 *   X.Y.Z
 *   X.Y.Z-preview.<id>[.<id>…]
 * Numeric identifiers reject leading zeroes except the literal 0.
 */

export type DistTag = "latest" | "preview";
export type ReleaseChannel = "stable" | "preview";

export type ParsedReleaseVersion = {
  text: string;
  major: number;
  minor: number;
  patch: number;
  pre: string[] | null;
};

export class ReleaseError extends Error {
  readonly code: string;

  constructor(code: string, message: string) {
    super(message);
    this.name = "ReleaseError";
    this.code = code;
  }
}

const RELEASE_FORM =
  /^(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z.-]+))?$/;

/** A numeric identifier is digits with no redundant leading zero. */
const NUMERIC_IDENTIFIER = /^(?:0|[1-9]\d*)$/;

type Core = { major: number; minor: number; patch: number };

type Comparison = { text: string; core: Core; pre: string[] | null };

function numericComponent(raw: string, label: string): number {
  if (!NUMERIC_IDENTIFIER.test(raw)) {
    throw new ReleaseError(
      "invalid_version",
      `${label} must be a SemVer numeric identifier without leading zeroes (got ${JSON.stringify(raw)})`,
    );
  }
  return Number(raw);
}

function prereleaseIdentifiers(raw: string): string[] {
  const identifiers = raw.split(".");
  for (const identifier of identifiers) {
    if (/^\d+$/.test(identifier) && !NUMERIC_IDENTIFIER.test(identifier)) {
      throw new ReleaseError(
        "invalid_version",
        `Prerelease identifier ${JSON.stringify(identifier)} has a leading zero`,
      );
    }
  }
  return identifiers;
}

/** Shared reader: validates the comparison grammar and normalizes the text. */
function readVersion(input: string): Comparison {
  const text = input.trim();
  const matched = RELEASE_FORM.exec(text);
  if (!matched) {
    throw new ReleaseError("invalid_version", `Unsupported release version: ${text}`);
  }
  const digits = `${matched[1]}.${matched[2]}.${matched[3]}`;
  const suffix = matched[4];
  return {
    text: suffix ? `${digits}-${suffix}` : digits,
    core: {
      major: numericComponent(matched[1]!, "major"),
      minor: numericComponent(matched[2]!, "minor"),
      patch: numericComponent(matched[3]!, "patch"),
    },
    pre: suffix ? prereleaseIdentifiers(suffix) : null,
  };
}

export function parseSemver(text: string): ParsedReleaseVersion {
  const version = readVersion(text);
  return {
    text: version.text,
    major: version.core.major,
    minor: version.core.minor,
    patch: version.core.patch,
    pre: version.pre,
  };
}

export function parsePublishVersion(text: string): ParsedReleaseVersion {
  const trimmed = text.trim();
  if (trimmed.includes("+")) {
    throw new ReleaseError(
      "invalid_version",
      `Benes releases cannot include build metadata (got ${trimmed})`,
    );
  }
  return parseSemver(trimmed);
}

/** Numeric identifiers rank below alphanumeric ones. */
function rankIdentifier(left: string, right: string): number {
  const leftIsNumeric = /^\d+$/.test(left);
  const rightIsNumeric = /^\d+$/.test(right);
  if (leftIsNumeric && rightIsNumeric) {
    const delta = Number(left) - Number(right);
    if (delta !== 0) return delta;
  }
  if (leftIsNumeric && !rightIsNumeric) return -1;
  if (!leftIsNumeric && rightIsNumeric) return 1;
  if (left === right) return 0;
  return left < right ? -1 : 1;
}

/** Walks prerelease identifiers left to right; a shorter list sorts first. */
function rankPrerelease(left: string[], right: string[]): number {
  const shared = Math.min(left.length, right.length);
  for (let index = 0; index < shared; index += 1) {
    const verdict = rankIdentifier(left[index]!, right[index]!);
    if (verdict !== 0) return verdict;
  }
  if (left.length === right.length) return 0;
  return left.length < right.length ? -1 : 1;
}

export function compareReleaseVersions(left: string, right: string): number {
  const a = readVersion(left);
  const b = readVersion(right);
  const core =
    a.core.major - b.core.major ||
    a.core.minor - b.core.minor ||
    a.core.patch - b.core.patch;
  if (core !== 0) return core;
  if (a.pre === null && b.pre === null) return 0;
  if (a.pre === null) return 1;
  if (b.pre === null) return -1;
  return rankPrerelease(a.pre, b.pre);
}

export function releaseChannel(text: string): ReleaseChannel {
  const version = parsePublishVersion(text);
  if (version.pre === null) return "stable";
  if (version.pre[0] === "preview" && version.pre.length >= 2) return "preview";
  throw new ReleaseError(
    "invalid_channel",
    `Version ${text} is not a stable X.Y.Z or X.Y.Z-preview.<id> release`,
  );
}

export function distTagForChannel(channel: ReleaseChannel): DistTag {
  return channel === "preview" ? "preview" : "latest";
}

export function gitTagForVersion(text: string): string {
  return `v${parsePublishVersion(text).text}`;
}

export function assertVersionForChannel(text: string, channel: ReleaseChannel): void {
  if (releaseChannel(text) === channel) return;
  throw new ReleaseError(
    "channel_mismatch",
    channel === "stable"
      ? `Stable releases must use X.Y.Z (got ${text})`
      : `Preview releases must use X.Y.Z-preview.<id> (got ${text})`,
  );
}
