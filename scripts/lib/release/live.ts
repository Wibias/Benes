import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { npmInvocation, readTextFile, runCommand, sleep } from "../../node-runtime.ts";
import { commandInvocation } from "../../win-exec.ts";
import { parsePackageName, parseRepository } from "./identity.ts";
import { ancestorFromExitCode, waitForPublishedIdentity } from "./execute.ts";
import { parseAssociatedPulls, parseGitLogRecords } from "./notes.ts";
import { ReleaseError, type DistTag } from "./semver.ts";
import type { GithubReleaseState, ReleaseWorld } from "./world.ts";

const CI_DEADLINE_MS = 20 * 60 * 1000;
const CI_POLL_MS = 10_000;
const DISPATCH_DEADLINE_MS = 2 * 60 * 1000;
const DISPATCH_POLL_MS = 5_000;
const NPM_VISIBLE_ATTEMPTS = 30;

type ExecResult = { exitCode: number; stdout: string; stderr: string };

export type LiveWorldOptions = {
  repository: string;
  log?: (message: string) => void;
};

function registryEnv(): NodeJS.ProcessEnv {
  const env = { ...process.env };
  delete env.GH_TOKEN;
  delete env.GITHUB_TOKEN;
  delete env.NODE_AUTH_TOKEN;
  return env;
}

async function exec(argv: string[], env?: NodeJS.ProcessEnv): Promise<ExecResult> {
  const [bin, ...rest] = argv;
  const invocation = commandInvocation(bin ?? "", rest);
  const result = await runCommand([invocation.file, ...invocation.args], {
    windowsVerbatimArguments: invocation.options.windowsVerbatimArguments,
    env,
  });
  return {
    exitCode: result.exitCode,
    stdout: result.stdout.trim(),
    stderr: result.stderr.trim(),
  };
}

async function requireStdout(argv: string[]): Promise<string> {
  const result = await exec(argv);
  if (result.exitCode !== 0) {
    const detail = result.stderr || `exit ${result.exitCode}`;
    throw new ReleaseError("command_failed", `${argv[0]} failed: ${detail}`);
  }
  return result.stdout;
}

async function attach(argv: string[], env?: NodeJS.ProcessEnv): Promise<void> {
  const [bin, ...rest] = argv;
  const invocation = commandInvocation(bin ?? "", rest);
  const result = await runCommand([invocation.file, ...invocation.args], {
    inherit: true,
    windowsVerbatimArguments: invocation.options.windowsVerbatimArguments,
    env,
  });
  if (result.exitCode !== 0) {
    throw new ReleaseError("command_failed", `${argv[0]} failed (exit ${result.exitCode})`);
  }
}

function npmArgv(args: string[]): string[] {
  const invocation = npmInvocation(args);
  return [invocation.command, ...invocation.args];
}

export function npmVersionAlreadyOnRegistry(output: string): boolean {
  const text = output.toLowerCase();
  return text.includes("cannot publish over") || text.includes("previously published versions");
}

export function gitRefAlreadyExists(output: string): boolean {
  return /already exists/i.test(output);
}

export async function resolveRepository(env: NodeJS.ProcessEnv = process.env): Promise<string> {
  const fromEnv = env.GITHUB_REPOSITORY?.trim();
  if (fromEnv) return parseRepository(fromEnv);
  const raw = await requireStdout(["gh", "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner"]);
  return parseRepository(raw);
}

export function createLiveWorld(options: LiveWorldOptions): ReleaseWorld {
  const log = options.log ?? ((message: string) => console.log(message));
  const repository = options.repository;

  return {
    log,
    async packageName() {
      const pkg = JSON.parse(await readTextFile("package.json")) as { name?: unknown };
      if (typeof pkg.name !== "string") {
        throw new ReleaseError("invalid_package", "package.json is missing a valid name");
      }
      return parsePackageName(pkg.name);
    },
    async packageVersion() {
      const pkg = JSON.parse(await readTextFile("package.json")) as { version?: unknown };
      if (typeof pkg.version !== "string") {
        throw new ReleaseError("invalid_package", "package.json is missing a version");
      }
      return pkg.version;
    },
    async currentBranch() {
      return await requireStdout(["git", "rev-parse", "--abbrev-ref", "HEAD"]);
    },
    async currentSha() {
      return await requireStdout(["git", "rev-parse", "HEAD"]);
    },
    async workingTreeDirty() {
      const status = await requireStdout(["git", "status", "--porcelain"]);
      return status.length > 0;
    },
    async remoteHead(branch) {
      const out = await requireStdout(["git", "ls-remote", "origin", `refs/heads/${branch}`]);
      const sha = out.split(/\s+/)[0];
      if (!sha) throw new ReleaseError("git_failed", `Could not resolve origin/${branch}`);
      return sha;
    },
    async listTags() {
      await requireStdout(["git", "fetch", "--force", "--tags", "origin"]);
      const out = await requireStdout(["git", "tag", "--list", "v[0-9]*"]);
      return out.split(/\r?\n/).filter(Boolean);
    },
    async isAncestor(tag, sha) {
      const result = await exec(["git", "merge-base", "--is-ancestor", tag, sha]);
      return ancestorFromExitCode(result.exitCode);
    },
    async firstParentLog(span) {
      const spec = span.baseline ? [`${span.baseline}..${span.target}`] : [span.target];
      const raw = await requireStdout([
        "git",
        "log",
        "--first-parent",
        "--reverse",
        "--format=%H%x1f%s%x1f%B%x1e",
        ...spec,
      ]);
      return parseGitLogRecords(raw);
    },
    async associatedPulls(sha) {
      const result = await exec(["gh", "api", `repos/${repository}/commits/${sha}/pulls`]);
      if (result.exitCode !== 0) return [];
      try {
        return parseAssociatedPulls(JSON.parse(result.stdout));
      } catch {
        return [];
      }
    },
    async generatedNotes(input) {
      if (!input.baseline) return "";
      const result = await exec([
        "gh",
        "api",
        `repos/${input.repository}/releases/generate-notes`,
        "-f",
        `tag_name=${input.gitTag}`,
        "-f",
        `target_commitish=${input.target}`,
        "-f",
        `previous_tag_name=${input.baseline}`,
        "--jq",
        ".body",
      ]);
      return result.exitCode === 0 ? result.stdout : "";
    },
    async changedFiles(span) {
      const argv = span.baseline
        ? ["git", "diff", "--name-only", `${span.baseline}..${span.target}`]
        : ["git", "ls-tree", "-r", "--name-only", span.target];
      const out = await requireStdout(argv);
      return out.split(/\r?\n/).filter(Boolean);
    },
    async npmHasVersion(name, version) {
      const result = await exec(npmArgv(["view", `${name}@${version}`, "version"]), registryEnv());
      if (result.exitCode === 0) return true;
      const output = `${result.stdout}\n${result.stderr}`;
      if (output.includes("E404") || output.includes("No match found")) return false;
      throw new ReleaseError("npm_query_failed", `Failed to check npm ${name}@${version}`);
    },
    async npmGitHead(name, version) {
      const result = await exec(npmArgv(["view", `${name}@${version}`, "gitHead"]), registryEnv());
      if (result.exitCode !== 0) return null;
      const sha = result.stdout.trim().replace(/^"|"$/g, "");
      return /^[0-9a-f]{40}$/.test(sha) ? sha : null;
    },
    async npmDistTags(name) {
      const result = await exec(npmArgv(["view", name, "dist-tags", "--json"]), registryEnv());
      if (result.exitCode !== 0) {
        const output = `${result.stdout}\n${result.stderr}`;
        if (output.includes("E404") || output.includes("No match found")) return {};
        throw new ReleaseError("npm_query_failed", `Failed to read npm dist-tags for ${name}`);
      }
      return JSON.parse(result.stdout) as Record<string, string>;
    },
    async waitUntilPublishedIdentity(input) {
      await waitForPublishedIdentity({
        version: input.version,
        sourceSha: input.sourceSha,
        attempts: NPM_VISIBLE_ATTEMPTS,
        sleep: async () => {
          await sleep(10_000);
        },
        log,
        read: async () => {
          const versionResult = await exec(
            npmArgv(["view", `${input.name}@${input.version}`, "version"]),
            registryEnv(),
          );
          const version =
            versionResult.exitCode === 0 ? versionResult.stdout.trim().replace(/^"|"$/g, "") : null;
          return {
            version,
            gitHead: await this.npmGitHead(input.name, input.version),
          };
        },
      });
    },
    async remoteTagSha(tag) {
      const result = await exec([
        "git",
        "ls-remote",
        "origin",
        `refs/tags/${tag}`,
        `refs/tags/${tag}^{}`,
      ]);
      if (result.exitCode !== 0) {
        throw new ReleaseError("git_failed", `Failed to check remote tag ${tag}`);
      }
      const lines = result.stdout.split("\n").filter(Boolean);
      const peeled = lines.find((line) => line.endsWith(`refs/tags/${tag}^{}`));
      const exact = lines.find((line) => line.endsWith(`refs/tags/${tag}`));
      const selected = peeled ?? exact;
      return selected ? selected.split(/\s+/)[0] ?? null : null;
    },
    async readGithubRelease(tag) {
      const result = await exec([
        "gh",
        "release",
        "view",
        tag,
        "--json",
        "tagName,isDraft,isPrerelease",
      ]);
      if (result.exitCode !== 0) {
        const output = `${result.stdout}\n${result.stderr}`.toLowerCase();
        if (output.includes("release not found") || output.includes("not found")) return null;
        throw new ReleaseError("github_query_failed", `Failed to read GitHub Release ${tag}`);
      }
      const parsed = JSON.parse(result.stdout) as {
        tagName?: unknown;
        isDraft?: unknown;
        isPrerelease?: unknown;
      };
      if (typeof parsed.tagName !== "string") {
        throw new ReleaseError("github_query_failed", `GitHub Release ${tag} returned no tag name`);
      }
      return {
        tag: parsed.tagName,
        draft: parsed.isDraft === true,
        prerelease: parsed.isPrerelease === true,
      } satisfies GithubReleaseState;
    },
    async ciPushUrl(sha, branch) {
      const raw = await requireStdout([
        "gh",
        "run",
        "list",
        "--workflow",
        "ci.yml",
        "--branch",
        branch,
        "--commit",
        sha,
        "--event",
        "push",
        "--limit",
        "10",
        "--json",
        "conclusion,url",
      ]);
      const runs = JSON.parse(raw) as Array<{ conclusion?: string; url?: string }>;
      return runs.find((run) => run.conclusion === "success")?.url ?? null;
    },
    async serviceLifecycleUrl(sha) {
      const raw = await requireStdout([
        "gh",
        "run",
        "list",
        "--workflow",
        "service-lifecycle.yml",
        "--commit",
        sha,
        "--limit",
        "10",
        "--json",
        "conclusion,url",
      ]);
      const runs = JSON.parse(raw) as Array<{ conclusion?: string; url?: string }>;
      return runs.find((run) => run.conclusion === "success")?.url ?? null;
    },
    async waitForWorkflow(sha, workflow, label) {
      const deadline = Date.now() + CI_DEADLINE_MS;
      while (Date.now() < deadline) {
        const raw = await requireStdout([
          "gh",
          "run",
          "list",
          "--workflow",
          workflow,
          "--commit",
          sha,
          "--limit",
          "20",
          "--json",
          "conclusion,status,url",
        ]);
        const runs = JSON.parse(raw) as Array<{
          conclusion?: string | null;
          status?: string;
          url?: string;
        }>;
        const successful = runs.find((run) => run.status === "completed" && run.conclusion === "success");
        if (successful?.url) {
          log(`${label} passed: ${successful.url}`);
          return successful.url;
        }
        const failed = runs.find(
          (run) => run.status === "completed" && run.conclusion && run.conclusion !== "success",
        );
        if (failed?.url) {
          throw new ReleaseError("ci_failed", `${label} failed for ${sha}: ${failed.url}`);
        }
        log(`waiting for ${label} on ${sha.slice(0, 7)}`);
        await sleep(CI_POLL_MS);
      }
      throw new ReleaseError("ci_timeout", `Timed out waiting for ${label} on ${sha}`);
    },
    async runLocalChecks() {
      const npmEnv = registryEnv();
      log("dependency audit");
      await attach(npmArgv(["run", "audit:high"]), npmEnv);
      log("dashboard lint");
      await attach(npmArgv(["run", "lint:gui"]), npmEnv);
      log("Go tests");
      await attach(["go", "test", "./..."]);
      log("privacy scan");
      await attach(npmArgv(["run", "privacy:scan"]), npmEnv);
    },
    async bumpPackageVersion(version) {
      await attach(npmArgv(["version", version, "--no-git-tag-version"]), registryEnv());
    },
    async commitAndPush(input) {
      await attach(["git", "add", "--", "package.json", "package-lock.json"]);
      await attach(["git", "commit", "-m", `release: v${input.version}`]);
      const sha = await requireStdout(["git", "rev-parse", "HEAD"]);
      await attach(["git", "push", "origin", input.branch]);
      return sha;
    },
    async dispatchRelease(input) {
      await attach([
        "gh",
        "workflow",
        "run",
        "release.yml",
        "--ref",
        input.branch,
        "-f",
        `version=${input.version}`,
        "-f",
        `tag=${input.distTag}`,
        "-f",
        `expected-sha=${input.sha}`,
        "-f",
        `dry-run=${String(input.dryRun)}`,
      ]);
    },
    async watchDispatchedRelease(sha, branch) {
      const createdAfter = new Date(Date.now() - 5_000).toISOString();
      const deadline = Date.now() + DISPATCH_DEADLINE_MS;
      while (Date.now() < deadline) {
        const raw = await requireStdout([
          "gh",
          "run",
          "list",
          "--workflow",
          "release.yml",
          "--branch",
          branch,
          "--commit",
          sha,
          "--limit",
          "20",
          "--json",
          "createdAt,databaseId,headSha,url",
        ]);
        const runs = (
          JSON.parse(raw) as Array<{
            createdAt?: string;
            databaseId?: number;
            headSha?: string;
            url?: string;
          }>
        )
          .filter((run) => run.headSha === sha)
          .filter((run) => !run.createdAt || run.createdAt >= createdAfter);
        const run = runs[0];
        if (run?.databaseId) {
          log(`Release workflow run found: ${run.url ?? run.databaseId}`);
          await attach(["gh", "run", "watch", String(run.databaseId), "--exit-status", "--interval", "10"]);
          return;
        }
        await sleep(DISPATCH_POLL_MS);
      }
      throw new ReleaseError("dispatch_timeout", `Timed out waiting for dispatched Release run on ${sha}`);
    },
    async preparePackage() {
      await attach(npmArgv(["run", "prepublishOnly"]), registryEnv());
    },
    async packDryRun() {
      await attach(npmArgv(["pack", "--dry-run"]), registryEnv());
    },
    async publishNpm(distTag: DistTag) {
      const result = await exec(npmArgv(["publish", "--tag", distTag, "--access", "public"]), registryEnv());
      if (result.exitCode === 0) return;
      const output = `${result.stdout}\n${result.stderr}`;
      if (npmVersionAlreadyOnRegistry(output)) {
        log(`npm already has this version; continuing with remaining publication steps`);
        return;
      }
      throw new ReleaseError("npm_publish_failed", "npm publish failed");
    },
    async createTag(tag, sha) {
      const result = await exec([
        "gh",
        "api",
        "--method",
        "POST",
        `repos/${repository}/git/refs`,
        "-f",
        `ref=refs/tags/${tag}`,
        "-f",
        `sha=${sha}`,
      ]);
      if (result.exitCode === 0) return;
      const output = `${result.stdout}\n${result.stderr}`;
      if (gitRefAlreadyExists(output)) {
        const existing = await this.remoteTagSha(tag);
        if (existing === sha) return;
      }
      throw new ReleaseError("tag_create_failed", `Failed to create ${tag} at the audited SHA`);
    },
    async createGithubRelease(input) {
      const dir = await mkdtemp(join(tmpdir(), "benes-release-notes-"));
      const file = join(dir, "notes.md");
      try {
        await writeFile(file, input.notes, "utf8");
        const argv = [
          "gh",
          "release",
          "create",
          input.tag,
          "--target",
          input.sha,
          "--title",
          input.tag,
          "--notes-file",
          file,
        ];
        if (input.prerelease) argv.push("--prerelease");
        const result = await exec(argv);
        if (result.exitCode === 0) return;
        const output = `${result.stdout}\n${result.stderr}`;
        if (!gitRefAlreadyExists(output)) {
          throw new ReleaseError("github_release_failed", `Failed to create GitHub Release ${input.tag}`);
        }
        // Race: re-read; executePlan verifies type/draft against the audited identity.
      } finally {
        await rm(dir, { recursive: true, force: true });
      }
    },
  };
}

export async function watchLatestReleaseRun(): Promise<void> {
  const id = await requireStdout([
    "gh",
    "run",
    "list",
    "--workflow",
    "release.yml",
    "--limit",
    "1",
    "--json",
    "databaseId",
    "-q",
    ".[0].databaseId",
  ]);
  if (!id) throw new ReleaseError("no_runs", "No Release runs found yet.");
  await attach(["gh", "run", "watch", id, "--exit-status", "--interval", "10"]);
}
