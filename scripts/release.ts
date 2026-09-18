#!/usr/bin/env node
/**
 * Benes release coordinator.
 *
 *   node --experimental-strip-types scripts/release.ts <version> [--tag latest|preview] [--publish] [--plan-only]
 *   node --experimental-strip-types scripts/release.ts publish --version <v> --tag <latest|preview> --expected-sha <sha> [--dry-run]
 *   node --experimental-strip-types scripts/release.ts watch
 *
 * `cut` (default) bumps package.json on main/preview, waits for hosted push CI,
 * and dispatches `.github/workflows/release.yml`. `publish` is what that
 * workflow runs: it inspects public state, writes a plan, then either packs
 * (dry-run) or mutates npm/tag/GitHub Release.
 *
 * npm publish is OIDC trusted publishing. There is no npm token path.
 */

import { isMainModule } from "./node-runtime.ts";
import { parseReleaseArgv } from "./lib/release/args.ts";
import { runCut, runPublish } from "./lib/release/execute.ts";
import { branchFromRef, parseFullSha, resolveIdentity } from "./lib/release/identity.ts";
import { createLiveWorld, resolveRepository, watchLatestReleaseRun } from "./lib/release/live.ts";
import { ReleaseError } from "./lib/release/semver.ts";

export { compareReleaseVersions } from "./lib/release/semver.ts";

function fail(message: string): never {
  console.error(`✗ ${message}`);
  process.exit(1);
}

export async function main(argv: string[], env: NodeJS.ProcessEnv = process.env): Promise<void> {
  let command;
  try {
    command = parseReleaseArgv(argv, env);
  } catch (error) {
    fail(error instanceof Error ? error.message : String(error));
  }

  if (command.kind === "watch") {
    await watchLatestReleaseRun();
    return;
  }

  const repository = await resolveRepository(env);
  const world = createLiveWorld({ repository });

  if (command.kind === "cut") {
    const result = await runCut({
      world,
      version: command.version,
      distTag: command.distTag,
      publish: command.publish,
      planOnly: command.planOnly,
    });
    if (result.outcome === "plan-only") {
      console.log("Plan only. Re-run without --plan-only to bump and dispatch.");
      return;
    }
    console.log(
      command.publish
        ? "\nPublished dispatch complete. Try:  npm install -g benes"
        : "\nDry-run dispatch complete. Re-run with --publish to publish for real.",
    );
    return;
  }

  const observed = env.GITHUB_SHA?.trim() ? parseFullSha(env.GITHUB_SHA) : await world.currentSha();
  if (observed !== command.expectedSha) {
    throw new ReleaseError(
      "sha_mismatch",
      `The branch moved after the audit (expected ${command.expectedSha}, got ${observed})`,
    );
  }
  const branch = env.GITHUB_REF?.trim()
    ? branchFromRef(env.GITHUB_REF)
    : branchFromRef(await world.currentBranch());
  const identity = resolveIdentity({
    packageName: await world.packageName(),
    version: command.version,
    sourceSha: command.expectedSha,
    branch,
    distTag: command.distTag,
  });
  const result = await runPublish({
    world,
    identity,
    repository,
    dryRun: command.dryRun,
  });
  console.log(`\n${result.outcome}.`);
}

if (isMainModule(import.meta.url)) {
  main(process.argv.slice(2)).catch((error) => {
    fail(error instanceof Error ? error.message : String(error));
  });
}
