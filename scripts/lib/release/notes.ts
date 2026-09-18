import { compareReleaseVersions, parseSemver, ReleaseError } from "./semver.ts";
import type { DistTag } from "./semver.ts";

/**
 * Benes release-note evidence.
 *
 * The module is a pipeline. Each stage hands the next one a narrower shape:
 *
 *   normalize   - external text becomes HistoryCommit / AssociatedPull rows
 *   attribute   - every releasable commit is matched to its evidence
 *   decide      - skip-changelog and version-bump rows leave the visible set
 *   model       - the visible set becomes VisibleChange rows
 *   render      - the change model becomes the published markdown
 *   verify      - coverage errors are read back off the rendered markdown
 *
 * Nothing here publishes a partial narrative: a commit that cannot be traced
 * into the markdown is reported as a coverage error instead of being dropped.
 */

export const NOTE_CATEGORIES = [
  "Features",
  "Fixes",
  "Documentation",
  "Maintenance",
  "Other",
] as const;

export type NoteCategory = (typeof NOTE_CATEGORIES)[number];

export type AssociatedPull = {
  number: number;
  title: string;
  author: string;
  labels: string[];
  merged: boolean;
};

export type HistoryCommit = {
  sha: string;
  subject: string;
  body: string;
  pulls: AssociatedPull[];
};

export type VisibleChange =
  | {
      kind: "pr";
      category: NoteCategory;
      number: number;
      title: string;
      author: string;
    }
  | {
      kind: "commit";
      category: NoteCategory;
      sha: string;
      title: string;
    };

export type NotesModel = {
  baseline: string | null;
  changes: VisibleChange[];
  skipped: Array<{ sha: string; reason: string }>;
  markdown: string;
  errors: string[];
};

const CATEGORY_BY_TYPE: Record<string, NoteCategory> = {
  feat: "Features",
  fix: "Fixes",
  docs: "Documentation",
  chore: "Maintenance",
  build: "Maintenance",
  ci: "Maintenance",
  test: "Maintenance",
  refactor: "Maintenance",
  style: "Maintenance",
};

/**
 * Recognized generated-note headings. Only the current Benes vocabulary maps;
 * a historical tool's wording (for example "New Features") is not a substitute,
 * so such a heading leaves the category untouched.
 */
const CATEGORY_BY_HEADING: Record<string, NoteCategory> = {
  features: "Features",
  fixes: "Fixes",
  documentation: "Documentation",
  maintenance: "Maintenance",
  other: "Other",
};

const BUMP_SUBJECT =
  /^(?:release|chore\(release\)):\s*v?\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?\s*$/i;
const CONVENTIONAL = /^([a-z]+)(?:\([^)]+\))?!?:\s*(.+)$/i;
const TRAILING_PR = /\(#(\d+)\)\s*$/;
const GENERATED_LINE =
  /^\*\s*(.+?)\s+by\s+@([A-Za-z0-9-]+(?:\[bot\])?).*\/pull\/(\d+)\s*$/;

export function isVersionBumpCommit(subject: string): boolean {
  return BUMP_SUBJECT.test(subject.trim());
}

export function gitTagName(version: string): string {
  return version.startsWith("v") ? version : `v${parseSemver(version).text}`;
}

export function categoryFromTitle(title: string): NoteCategory {
  const conventional = CONVENTIONAL.exec(title.trim());
  const type = conventional?.[1]?.toLowerCase();
  return (type && CATEGORY_BY_TYPE[type]) || "Other";
}

export function categoryFromPull(pull: AssociatedPull): NoteCategory {
  const labels = new Set(pull.labels.map((label) => label.toLowerCase()));
  if (labels.has("enhancement")) return "Features";
  if (labels.has("bug")) return "Fixes";
  if (labels.has("documentation")) return "Documentation";
  if (labels.has("chore")) return "Maintenance";
  return categoryFromTitle(pull.title);
}

export function hasSkipChangelog(pull: AssociatedPull): boolean {
  return pull.labels.some((label) => label.toLowerCase() === "skip-changelog");
}

export function trailingPullNumber(subject: string): number | null {
  const matched = TRAILING_PR.exec(subject.trim());
  if (!matched) return null;
  const landing = Number(matched[1]);
  return Number.isInteger(landing) && landing > 0 ? landing : null;
}

export function mentionsPull(markdown: string, number: number): boolean {
  return new RegExp(`#${number}(?!\\d)`).test(markdown);
}

/**
 * Flattens one release-note cell. Newlines and control characters cannot break
 * out of a bullet, markdown punctuation that could open a link or a code span is
 * escaped, and an `@` is neutralized so a subject can never mention anyone.
 */
export function sanitizeNoteText(text: string): string {
  return text
    .replace(/\r?\n/g, " ")
    .replace(/[\u0000-\u001f]/g, " ")
    .replace(/([`<>|[\]\\])/g, "\\$1")
    .replace(/@(?=[A-Za-z0-9_-])/g, "@\u200b")
    .replace(/\s+/g, " ")
    .trim();
}

export function cleanChangeTitle(title: string, prNumber?: number): string {
  let text = title.trim();
  const conventional = CONVENTIONAL.exec(text);
  if (conventional?.[2]) text = conventional[2];
  if (prNumber !== undefined) {
    text = text.replace(new RegExp(`\\s*\\(#${prNumber}\\)\\s*$`), "");
  } else {
    text = text.replace(/\s*\(#\d+\)\s*$/, "");
  }
  text = text.trim();
  if (!text) return title.trim();
  return text.charAt(0).toUpperCase() + text.slice(1);
}

/** Stage: normalize generated GitHub notes into the pull requests they cover. */
export function parseGeneratedNotePulls(body: string): Map<number, VisibleChange> {
  const covered = new Map<number, VisibleChange>();
  let category: NoteCategory = "Other";
  for (const raw of body.replace(/\r\n/g, "\n").split("\n")) {
    const heading = /^(?:##|###)\s+(.+?)\s*$/.exec(raw);
    if (heading) {
      const mapped = CATEGORY_BY_HEADING[heading[1].trim().toLowerCase()];
      if (mapped) category = mapped;
      continue;
    }
    const line = GENERATED_LINE.exec(raw);
    if (!line) continue;
    const number = Number(line[3]);
    if (!Number.isInteger(number) || covered.has(number)) continue;
    covered.set(number, {
      kind: "pr",
      category,
      number,
      title: line[1].trim(),
      author: line[2],
    });
  }
  return covered;
}

/** Stage: normalize GitHub's associated-pull payload. Never throws on a row. */
export function parseAssociatedPulls(data: unknown): AssociatedPull[] {
  if (!Array.isArray(data)) {
    throw new ReleaseError("malformed_pulls", "commit PR lookup returned non-array JSON");
  }
  const pulls: AssociatedPull[] = [];
  for (const item of data) {
    if (!item || typeof item !== "object") continue;
    const row = item as {
      number?: unknown;
      title?: unknown;
      merged_at?: unknown;
      user?: { login?: unknown } | null;
      labels?: Array<{ name?: unknown }>;
    };
    if (typeof row.number !== "number" || typeof row.title !== "string") continue;
    pulls.push({
      number: row.number,
      title: row.title,
      author: typeof row.user?.login === "string" ? row.user.login : "unknown",
      labels: Array.isArray(row.labels)
        ? row.labels
            .map((label) => label?.name)
            .filter((name): name is string => typeof name === "string")
        : [],
      merged: typeof row.merged_at === "string" && row.merged_at.length > 0,
    });
  }
  return pulls;
}

/** Stage: normalize `git log` records. A malformed record fails closed. */
export function parseGitLogRecords(raw: string): Array<Omit<HistoryCommit, "pulls">> {
  const commits: Array<Omit<HistoryCommit, "pulls">> = [];
  for (const record of raw.split("\x1e")) {
    if (!record.trim()) continue;
    const [sha, subject, ...bodyParts] = record.replace(/^\n+/, "").split("\x1f");
    if (!sha?.trim() || !subject?.trim()) {
      throw new ReleaseError("malformed_git_log", "git log produced a malformed commit record");
    }
    commits.push({
      sha: sha.trim(),
      subject: subject.trim(),
      body: bodyParts.join("\x1f").trim(),
    });
  }
  return commits;
}

function rankAgainstSelf(tag: string, self: string): number | null {
  try {
    return compareReleaseVersions(tag.slice(1), self.slice(1));
  } catch {
    return null;
  }
}

/**
 * Stage: pick the floor for the new notes.
 *
 * A preview release reconstructs from the newest older reachable release on
 * either channel; a stable release reconstructs from the newest older reachable
 * *stable* release, so preview work is folded back into the stable narrative.
 */
export function selectNotesBaseline(
  version: string,
  tags: string[],
  isAncestor?: (tag: string) => boolean,
): string | null {
  const self = gitTagName(version);
  const previewTarget = parseSemver(version).pre !== null;
  const eligible: string[] = [];
  for (const raw of tags) {
    const tag = raw.trim();
    if (!/^v\d/.test(tag)) continue;
    const order = rankAgainstSelf(tag, self);
    if (order === null || order >= 0) continue;
    if (!previewTarget && parseSemver(tag.slice(1)).pre !== null) continue;
    if (isAncestor && !isAncestor(tag)) continue;
    eligible.push(tag);
  }
  if (eligible.length === 0) return null;
  eligible.sort((left, right) => compareReleaseVersions(left.slice(1), right.slice(1)));
  return eligible[eligible.length - 1]!;
}

type RenderContext = {
  packageName: string;
  version: string;
  distTag: DistTag;
  repository: string;
  baseline: string | null;
  gitTag: string;
};

function commitUrl(repository: string, sha: string): string {
  return `https://github.com/${repository}/commit/${sha}`;
}

function renderChange(change: VisibleChange, repository: string): string {
  if (change.kind === "pr") {
    return `- ${sanitizeNoteText(cleanChangeTitle(change.title, change.number))} (#${change.number})`;
  }
  const short = change.sha.slice(0, 8);
  return `- ${sanitizeNoteText(cleanChangeTitle(change.title))} ([${short}](${commitUrl(repository, change.sha)}))`;
}

/** Stage: change model -> published markdown bytes. */
function renderMarkdown(context: RenderContext, changes: VisibleChange[]): string {
  const body: string[] = [
    `Published to npm as \`${context.packageName}@${context.version}\` with dist-tag \`${context.distTag}\`.`,
  ];

  const present = new Set(changes.map((change) => change.category));
  for (const category of NOTE_CATEGORIES.filter((name) => present.has(name))) {
    const lines = [`## ${category}`, ""];
    for (const change of changes.filter((item) => item.category === category)) {
      lines.push(renderChange(change, context.repository));
    }
    body.push(lines.join("\n"));
  }

  const catalog = ["## Changelog", ""];
  if (context.baseline) {
    catalog.push(
      `Full Changelog: https://github.com/${context.repository}/compare/${context.baseline}...${context.gitTag}`,
      "",
    );
  }
  const pullChanges = changes
    .filter((change): change is Extract<VisibleChange, { kind: "pr" }> => change.kind === "pr")
    .sort((left, right) => left.number - right.number);
  for (const pull of pullChanges) {
    catalog.push(
      `- #${pull.number} ${sanitizeNoteText(pull.title.trim())} @\u200b${sanitizeNoteText(pull.author || "unknown").replace(/^@\u200b/, "")}`,
    );
  }
  const commitChanges = changes.filter(
    (change): change is Extract<VisibleChange, { kind: "commit" }> => change.kind === "commit",
  );
  for (const commit of commitChanges) {
    catalog.push(
      `- [${commit.sha.slice(0, 8)}](${commitUrl(context.repository, commit.sha)}) ${sanitizeNoteText(commit.title)}`,
    );
  }
  body.push(catalog.join("\n").replace(/\n+$/, ""));

  return `${body.join("\n\n").replace(/\n+$/, "")}\n`;
}

type Coverage =
  | { kind: "pr"; ids: number[] }
  | { kind: "commit" }
  | { kind: "skip" };

type CommitEvidence =
  | { via: "generated"; ids: number[] }
  | { via: "fallback"; pulls: AssociatedPull[] }
  | { via: "skipped"; pulls: AssociatedPull[] }
  | { via: "direct" };

/**
 * Stage: attribute one commit to its evidence, in precedence order. Generated
 * notes win over fallback association so a pull request is never listed twice.
 */
function attributeCommit(
  commit: HistoryCommit,
  generated: Map<number, VisibleChange>,
): CommitEvidence {
  const landing = trailingPullNumber(commit.subject);
  if (landing !== null && generated.has(landing)) return { via: "generated", ids: [landing] };

  const merged = commit.pulls.filter((pull) => pull.merged);
  const visible = merged.filter((pull) => !hasSkipChangelog(pull));
  const alreadyGenerated = visible.filter((pull) => generated.has(pull.number));
  if (alreadyGenerated.length > 0) {
    return { via: "generated", ids: alreadyGenerated.map((pull) => pull.number) };
  }
  if (visible.length > 0) return { via: "fallback", pulls: visible };
  if (merged.length > 0 && merged.every(hasSkipChangelog)) return { via: "skipped", pulls: merged };
  return { via: "direct" };
}

function pullChange(pull: AssociatedPull): VisibleChange {
  return {
    kind: "pr",
    category: categoryFromPull(pull),
    number: pull.number,
    title: pull.title,
    author: pull.author,
  };
}

export function buildNotes(input: {
  version: string;
  packageName: string;
  distTag: DistTag;
  repository: string;
  tags: string[];
  commits: HistoryCommit[];
  generatedNotes?: string;
  isAncestor?: (tag: string) => boolean;
}): NotesModel {
  const baseline = selectNotesBaseline(input.version, input.tags, input.isAncestor);
  const gitTag = gitTagName(input.version);
  const generated = parseGeneratedNotePulls(input.generatedNotes ?? "");
  const fallback = new Map<number, VisibleChange>();
  const direct: VisibleChange[] = [];
  const skipped: NotesModel["skipped"] = [];
  const coverage = new Map<string, Coverage>();

  const releasable = input.commits.filter((commit) => !isVersionBumpCommit(commit.subject));
  for (const commit of releasable) {
    const evidence = attributeCommit(commit, generated);
    switch (evidence.via) {
      case "generated":
        coverage.set(commit.sha, { kind: "pr", ids: evidence.ids });
        break;
      case "fallback":
        for (const pull of evidence.pulls) {
          if (fallback.has(pull.number) || generated.has(pull.number)) continue;
          fallback.set(pull.number, pullChange(pull));
        }
        coverage.set(commit.sha, { kind: "pr", ids: evidence.pulls.map((pull) => pull.number) });
        break;
      case "skipped":
        skipped.push({
          sha: commit.sha,
          reason: `${evidence.pulls.map((pull) => `#${pull.number}`).join(", ")} labeled skip-changelog`,
        });
        coverage.set(commit.sha, { kind: "skip" });
        break;
      default:
        direct.push({
          kind: "commit",
          category: categoryFromTitle(commit.subject),
          sha: commit.sha,
          title: commit.subject,
        });
        coverage.set(commit.sha, { kind: "commit" });
    }
  }

  const changes: VisibleChange[] = [...generated.values(), ...fallback.values(), ...direct];
  const markdown = renderMarkdown(
    {
      packageName: input.packageName,
      version: parseSemver(input.version).text,
      distTag: input.distTag,
      repository: input.repository,
      baseline,
      gitTag,
    },
    changes,
  );

  // Stage: verify. Every releasable commit must be traceable in the markdown.
  const errors: string[] = [];
  for (const commit of releasable) {
    const covered = coverage.get(commit.sha);
    if (!covered) {
      errors.push(`commit ${commit.sha.slice(0, 12)} is not represented`);
      continue;
    }
    if (covered.kind === "commit" && !markdown.includes(commit.sha.slice(0, 8))) {
      errors.push(`direct commit ${commit.sha.slice(0, 12)} is missing from the notes`);
    }
    if (covered.kind === "pr" && !covered.ids.some((id) => mentionsPull(markdown, id))) {
      errors.push(
        `commit ${commit.sha.slice(0, 12)} maps to ${covered.ids.map((id) => `#${id}`).join(", ")}, but none were rendered`,
      );
    }
  }
  if (releasable.length > 0 && changes.length === 0) {
    errors.push(`${releasable.length} changed commit(s) exist, but the notes have no visible entries`);
  }

  return { baseline, changes, skipped, markdown, errors };
}
