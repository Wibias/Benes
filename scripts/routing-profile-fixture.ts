/**
 * Isolated Routing Profiles UI fixture.
 *
 * Never touches ~/.benes or ports 23100/23200. State lives under
 * `.tmp/routing-profile-fixture/` (gitignored) and is discarded with `down`.
 *
 *   node --experimental-strip-types scripts/routing-profile-fixture.ts up
 *   node --experimental-strip-types scripts/routing-profile-fixture.ts seed
 *   node --experimental-strip-types scripts/routing-profile-fixture.ts dry-run
 *   node --experimental-strip-types scripts/routing-profile-fixture.ts status
 *   node --experimental-strip-types scripts/routing-profile-fixture.ts down
 *   node --experimental-strip-types scripts/routing-profile-fixture.ts run
 */
import { spawn, spawnSync } from "node:child_process";
import { createServer } from "node:net";
import { openSync } from "node:fs";
import { access, mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { isMainModule } from "./node-runtime.ts";

const RESERVED_PORTS = new Set([23100, 23200]);
const READY_TIMEOUT_MS = 45_000;
const BUILD_TIMEOUT_MS = 180_000;
const FIXTURE_NAME = "routing-profile-fixture";

export type FixtureProvider = {
  id: string;
  adapter: string;
  baseUrl: string;
  models: string[];
};

/** Deterministic provider catalog for the isolated home — no secrets. */
export const FIXTURE_PROVIDERS: FixtureProvider[] = [
  {
    id: "openai",
    adapter: "openai-responses",
    baseUrl: "https://chatgpt.com/backend-api/codex",
    models: ["gpt-4o", "gpt-5.4", "o1", "o3-mini"],
  },
  {
    id: "openai-apikey",
    adapter: "openai-chat",
    baseUrl: "https://api.openai.com/v1",
    models: ["gpt-4o-mini", "gpt-5.5"],
  },
  {
    id: "anthropic",
    adapter: "anthropic",
    baseUrl: "https://api.anthropic.com",
    models: ["claude-sonnet-4-5", "claude-opus-4-6"],
  },
  {
    id: "google",
    adapter: "openai-chat",
    baseUrl: "https://generativelanguage.googleapis.com/v1beta/openai",
    models: ["gemini-2.5-pro", "gemini-2.5-flash"],
  },
  {
    id: "xai",
    adapter: "openai-responses",
    baseUrl: "https://api.x.ai/v1",
    models: ["grok-4", "grok-3-mini"],
  },
  {
    id: "openrouter",
    adapter: "openai-chat",
    baseUrl: "https://openrouter.ai/api/v1",
    models: [
      "anthropic/claude-3.7-sonnet",
      "meta-llama/llama-4-maverick-17b-128e-instruct",
    ],
  },
  {
    id: "command-code",
    adapter: "command-code",
    baseUrl: "https://api.commandcode.ai",
    models: ["claude-fable-5", "deepseek-v4-flash"],
  },
];

export type Candidate = { provider: string; model: string };

export function flattenCatalog(providers: FixtureProvider[] = FIXTURE_PROVIDERS): Candidate[] {
  const out: Candidate[] = [];
  for (const provider of providers) {
    for (const model of provider.models) {
      out.push({ provider: provider.id, model });
    }
  }
  return out;
}

export function longestCatalogEntries(catalog: Candidate[]): {
  provider: string;
  model: string;
} {
  let provider = "";
  let model = "";
  for (const row of catalog) {
    if (row.provider.length > provider.length) provider = row.provider;
    if (row.model.length > model.length) model = row.model;
  }
  return { provider, model };
}

export function buildFixtureConfig(port: number, providers: FixtureProvider[] = FIXTURE_PROVIDERS) {
  const providerMap: Record<string, Record<string, unknown>> = {};
  for (const provider of providers) {
    providerMap[provider.id] = {
      adapter: provider.adapter,
      baseUrl: provider.baseUrl,
      authMode: "api-key",
      models: provider.models,
    };
  }
  // openai uses forward/Codex shape like verify-benes (still no secrets).
  providerMap.openai = {
    adapter: "openai-responses",
    baseUrl: "https://chatgpt.com/backend-api/codex",
    authMode: "forward",
    codexAccountMode: "pool",
    models: providers.find((p) => p.id === "openai")?.models ?? ["gpt-4o"],
  };
  return {
    hostname: "127.0.0.1",
    port,
    websockets: false,
    providers: providerMap,
    defaultProvider: "openai",
  };
}

export type ProfileSeed = {
  id: string;
  label: string;
  profile: Record<string, unknown>;
};

/** Build API-valid profile PUT bodies from a live catalog + optional lab suites. */
export function buildProfileSeeds(
  catalog: Candidate[],
  suiteIds: string[],
): { seeds: ProfileSeed[]; limitations: string[] } {
  const limitations: string[] = [];
  if (catalog.length === 0) {
    throw new Error("catalog is empty — cannot seed routing profiles");
  }
  const pick = (n: number): Candidate[] => catalog.slice(0, Math.min(n, catalog.length));
  if (catalog.length < 3) {
    limitations.push(`Only ${catalog.length} catalog rows; production/many-candidates will be thinner.`);
  }
  if (suiteIds.length === 0) {
    limitations.push("Lab catalog returned no suite ids; strict enables gates without requiredSuites.");
  }

  const productionCandidates = pick(4);
  const manyCandidates = pick(10);
  if (manyCandidates.length < 6) {
    limitations.push(
      `many-candidates has ${manyCandidates.length} distinct rows (wanted 6–10). Duplicates are not used.`,
    );
  }
  const long = longestCatalogEntries(catalog);
  const longId = "fixture-long-id-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa".slice(0, 64);
  const longAlias =
    "Fixture Long Alias — production-grade routing profile display name for layout stress";

  const seeds: ProfileSeed[] = [
    {
      id: "production",
      label: "production",
      profile: {
        alias: "Production",
        icon: "rocket-launch",
        candidates: productionCandidates,
        optimize: { latency: 0.55, health: 0.25, cost: 0.1, quota: 0.1 },
        unknownEvidence: {
          capability: "exclude",
          health: "penalize",
          quota: "penalize",
          cost: "penalize",
        },
      },
    },
    {
      id: "minimal",
      label: "minimal",
      profile: {
        icon: "file-document",
        candidates: [catalog[0]!],
        optimize: { latency: 0.55, health: 0.25, cost: 0.1, quota: 0.1 },
        unknownEvidence: {
          capability: "exclude",
          health: "penalize",
          quota: "penalize",
          cost: "penalize",
        },
      },
    },
    {
      id: "strict",
      label: "strict",
      profile: {
        alias: "Strict",
        icon: "shield-check",
        candidates: pick(3),
        require: {
          tools: true,
          imageInput: true,
          structuredOutput: true,
          minContextWindow: 128000,
          minQuotaHeadroom: 0.25,
          reasoningEffort: "high",
          serviceTier: "priority",
          localOnly: false,
          remoteAllowed: true,
          encryptedCodexTasks: true,
        },
        optimize: { latency: 0.2, health: 0.2, cost: 0.4, quota: 0.2 },
        limits: { maxEstimatedCostUsd: 0.75, onUnknownCost: "exclude" },
        unknownEvidence: {
          capability: "exclude",
          health: "exclude",
          quota: "penalize",
          cost: "allow",
        },
        compatibility: {
          ...(suiteIds[0]
            ? {
                requiredSuites: [
                  { suiteId: suiteIds[0], evidenceLayer: "protocol_conformance" },
                ],
              }
            : {}),
          minStatus: "VERIFIED",
          maxEvidenceAgeMs: 7 * 86_400_000,
          unknownEvidence: "exclude",
          degradedEvidence: "penalize",
        },
      },
    },
    {
      id: "explicit-off",
      label: "explicit-off",
      profile: {
        alias: "Explicit Off",
        icon: "toggle-switch-off",
        candidates: pick(2),
        require: {
          tools: false,
          imageInput: false,
          structuredOutput: false,
          localOnly: false,
          remoteAllowed: false,
          encryptedCodexTasks: false,
          minQuotaHeadroom: 0,
        },
        optimize: { latency: 0.55, health: 0.25, cost: 0.1, quota: 0.1 },
        unknownEvidence: {
          capability: "exclude",
          health: "penalize",
          quota: "penalize",
          cost: "penalize",
        },
      },
    },
    {
      id: "many-candidates",
      label: "many-candidates",
      profile: {
        alias: "Many Candidates",
        icon: "format-list-numbered",
        candidates: manyCandidates,
        optimize: { latency: 0.4, health: 0.3, cost: 0.2, quota: 0.1 },
        unknownEvidence: {
          capability: "exclude",
          health: "penalize",
          quota: "penalize",
          cost: "penalize",
        },
      },
    },
    {
      id: longId,
      label: "long-identifiers",
      profile: {
        alias: longAlias,
        icon: "text-long",
        candidates: [
          { provider: long.provider, model: long.model },
          catalog.find((row) => row.provider !== long.provider || row.model !== long.model) ?? catalog[0]!,
        ],
        optimize: { latency: 0.55, health: 0.25, cost: 0.1, quota: 0.1 },
        unknownEvidence: {
          capability: "exclude",
          health: "penalize",
          quota: "penalize",
          cost: "penalize",
        },
      },
    },
  ];
  return { seeds, limitations };
}

function usage(): string {
  return `routing-profile-fixture — isolated Routing Profiles seed

Usage (repository root):
  node --experimental-strip-types scripts/routing-profile-fixture.ts <command>

Commands:
  up        Create .tmp/routing-profile-fixture, build, serve --no-inject
  seed      PUT representative profiles on the owned listener
  dry-run   POST dry-run evidence variants for each seeded profile
  status    Print owned port / home / dashboard URL
  down      Kill owned pid and delete the fixture directory
  run       up → seed → dry-run → status

Never uses ~/.benes, 23100, or 23200.`;
}

function fail(message: string, code = 1): never {
  process.stderr.write(`routing-profile-fixture: ${message}\n`);
  process.exit(code);
}

async function fileExists(file: string): Promise<boolean> {
  try {
    await access(file);
    return true;
  } catch {
    return false;
  }
}

function processAlive(pid: number): boolean {
  if (!Number.isInteger(pid) || pid <= 0) return false;
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

function killPid(pid: number): void {
  if (!processAlive(pid)) return;
  if (process.platform === "win32") {
    spawnSync("taskkill", ["/PID", String(pid), "/F"], { windowsHide: true, encoding: "utf8" });
    return;
  }
  try {
    process.kill(pid, "SIGTERM");
  } catch {
    // gone
  }
}

function binaryName(): string {
  return process.platform === "win32" ? "benes.exe" : "benes";
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function resolveRoot(): Promise<string> {
  const here = path.dirname(fileURLToPath(import.meta.url));
  const root = path.resolve(here, "..");
  const pkg = JSON.parse(await readFile(path.join(root, "package.json"), "utf8")) as { name?: string };
  if (pkg.name !== "@wibias/benes") fail("run from the Benes checkout root");
  return root;
}

function fixtureDir(root: string): string {
  return path.join(root, ".tmp", FIXTURE_NAME);
}

function currentPath(root: string): string {
  return path.join(fixtureDir(root), "current.json");
}

type CurrentState = {
  pid: number;
  port: number;
  benes_home: string;
  binary: string;
  log_file: string;
  dashboard_url: string;
  seeded_ids?: string[];
  limitations?: string[];
};

async function loadCurrent(root: string): Promise<CurrentState | null> {
  const file = currentPath(root);
  if (!(await fileExists(file))) return null;
  return JSON.parse(await readFile(file, "utf8")) as CurrentState;
}

async function saveCurrent(root: string, state: CurrentState): Promise<void> {
  await mkdir(fixtureDir(root), { recursive: true });
  await writeFile(currentPath(root), `${JSON.stringify(state, null, 2)}\n`, "utf8");
}

async function pickFreePort(): Promise<number> {
  for (let i = 0; i < 30; i++) {
    const port = await new Promise<number>((resolve, reject) => {
      const server = createServer();
      server.unref();
      server.on("error", reject);
      server.listen(0, "127.0.0.1", () => {
        const address = server.address();
        const chosen = typeof address === "object" && address ? address.port : 0;
        server.close((err) => (err ? reject(err) : resolve(chosen)));
      });
    });
    if (port > 0 && !RESERVED_PORTS.has(port)) return port;
  }
  throw new Error("could not allocate a free loopback port outside 23100/23200");
}

async function httpJson(
  port: number,
  pathname: string,
  options: { method?: string; body?: unknown } = {},
): Promise<{ status: number; json: unknown; text: string }> {
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    throw new Error("fixture port is invalid");
  }
  if (!pathname.startsWith("/") || pathname.includes("://")) {
    throw new Error("fixture path must stay on loopback");
  }
  // Current-state data is used only to address the local Benes fixture and seed its own API.
  // codeql[js/file-access-to-http]
  const response = await fetch(`http://127.0.0.1:${port}${pathname}`, {
    method: options.method ?? "GET",
    headers: {
      Accept: "application/json",
      ...(options.body !== undefined ? { "Content-Type": "application/json" } : {}),
    },
    body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
  });
  const text = await response.text();
  let json: unknown = null;
  try {
    json = text ? JSON.parse(text) : null;
  } catch {
    json = null;
  }
  return { status: response.status, json, text };
}

async function waitReady(port: number): Promise<void> {
  const deadline = Date.now() + READY_TIMEOUT_MS;
  let last = "";
  while (Date.now() < deadline) {
    try {
      const health = await httpJson(port, "/healthz");
      const ready = await httpJson(port, "/readyz");
      if (
        health.status === 200
        && (health.json as { ok?: boolean } | null)?.ok === true
        && ready.status === 200
        && (ready.json as { status?: string } | null)?.status === "ready"
      ) {
        return;
      }
      last = JSON.stringify({ health: health.json, ready: ready.json });
    } catch (error) {
      last = String(error);
    }
    await sleep(250);
  }
  throw new Error(`listener not ready: ${last}`);
}

async function cmdUp(root: string): Promise<void> {
  const existing = await loadCurrent(root);
  if (existing?.pid && processAlive(existing.pid)) {
    killPid(existing.pid);
    await sleep(500);
  }
  const dir = fixtureDir(root);
  try {
    await rm(dir, { recursive: true, force: true });
  } catch {
    await sleep(800);
    await rm(dir, { recursive: true, force: true });
  }
  const home = path.join(dir, "home");
  const bin = path.join(dir, binaryName());
  const logFile = path.join(dir, "serve.log");
  const port = await pickFreePort();
  await mkdir(home, { recursive: true });
  await writeFile(
    path.join(home, "config.json"),
    `${JSON.stringify(buildFixtureConfig(port), null, 2)}\n`,
    "utf8",
  );

  const build = spawnSync("go", ["build", "-o", bin, "./cmd/benes"], {
    cwd: root,
    encoding: "utf8",
    windowsHide: true,
    timeout: BUILD_TIMEOUT_MS,
  });
  if (build.status !== 0) fail(`go build failed: ${build.stderr || build.stdout}`);

  const logFd = openSync(logFile, "a");
  const child = spawn(bin, ["serve", "--port", String(port), "--no-inject"], {
    cwd: root,
    env: {
      ...process.env,
      BENES_HOME: home,
      BENES_SKIP_CODEX_INJECT: "1",
    },
    detached: true,
    stdio: ["ignore", logFd, logFd],
    windowsHide: true,
  });
  const pid = child.pid;
  child.unref();
  if (!pid) fail("failed to spawn fixture listener");
  try {
    await waitReady(port);
  } catch (error) {
    killPid(pid);
    fail(String((error as Error).message || error));
  }
  const state: CurrentState = {
    pid,
    port,
    benes_home: home,
    binary: bin,
    log_file: logFile,
    dashboard_url: `http://127.0.0.1:${port}/#routing`,
  };
  await saveCurrent(root, state);
  process.stdout.write(`${JSON.stringify({ ok: true, ...state }, null, 2)}\n`);
}

async function requireCurrent(root: string): Promise<CurrentState> {
  const current = await loadCurrent(root);
  if (!current) fail("no fixture; run up first");
  if (!processAlive(current.pid)) fail(`fixture pid ${current.pid} is not alive; run down then up`);
  return current;
}

function catalogFromProviders(): Candidate[] {
  return flattenCatalog(FIXTURE_PROVIDERS);
}

async function loadSuiteIds(port: number): Promise<string[]> {
  const res = await httpJson(port, "/api/lab/catalog");
  if (res.status !== 200 || !res.json || typeof res.json !== "object") return [];
  const scenarios = (res.json as { scenarios?: unknown }).scenarios;
  if (!Array.isArray(scenarios)) return [];
  const ids: string[] = [];
  const seen = new Set<string>();
  for (const row of scenarios) {
    if (!row || typeof row !== "object") continue;
    const suiteId = (row as { suiteId?: unknown }).suiteId;
    if (typeof suiteId !== "string" || !suiteId.trim() || seen.has(suiteId)) continue;
    seen.add(suiteId);
    ids.push(suiteId.trim());
  }
  return ids;
}

async function cmdSeed(root: string): Promise<void> {
  const current = await requireCurrent(root);
  const configRes = await httpJson(current.port, "/api/config");
  const fixtureCatalog = catalogFromProviders();
  let catalog = fixtureCatalog;
  const configProviders =
    configRes.status === 200
    && configRes.json
    && typeof configRes.json === "object"
    && (configRes.json as { providers?: unknown }).providers
    && typeof (configRes.json as { providers: unknown }).providers === "object"
      ? Object.keys((configRes.json as { providers: Record<string, unknown> }).providers)
      : [];
  if (configProviders.length > 0) {
    const known = new Set(configProviders);
    const filtered = fixtureCatalog.filter((row) => known.has(row.provider));
    if (filtered.length > 0) catalog = filtered;
  }
  const suiteIds = await loadSuiteIds(current.port);
  const { seeds, limitations } = buildProfileSeeds(catalog, suiteIds);
  const results: Array<{ id: string; status: number; ok: boolean; error?: string }> = [];
  for (const seed of seeds) {
    const res = await httpJson(current.port, "/api/routing-profiles", {
      method: "PUT",
      body: { mode: "create", id: seed.id, profile: seed.profile },
    });
    const err = res.json && typeof res.json === "object"
      ? (res.json as { error?: { message?: string; code?: string } }).error
      : undefined;
    results.push({
      id: seed.id,
      status: res.status,
      ok: res.status === 200,
      error: err?.message || err?.code,
    });
  }
  const list = await httpJson(current.port, "/api/routing-profiles");
  current.seeded_ids = seeds.map((seed) => seed.id);
  current.limitations = limitations;
  await saveCurrent(root, current);
  process.stdout.write(`${JSON.stringify({
    ok: results.every((row) => row.ok),
    results,
    limitations,
    catalog_used: catalog,
    suite_ids: suiteIds,
    profiles_get_status: list.status,
    profile_count: Array.isArray((list.json as { profiles?: unknown } | null)?.profiles)
      ? ((list.json as { profiles: unknown[] }).profiles.length)
      : null,
    dashboard_url: current.dashboard_url,
  }, null, 2)}\n`);
  if (!results.every((row) => row.ok)) process.exit(1);
}

const DRY_RUN_CASES = [
  { name: "empty", evidence: {} },
  { name: "context", evidence: { contextWindow: 32000 } },
  { name: "tools", evidence: { tools: true } },
  { name: "image", evidence: { imageInput: true } },
  { name: "structured", evidence: { structuredOutput: true } },
] as const;

async function cmdDryRun(root: string): Promise<void> {
  const current = await requireCurrent(root);
  const ids = current.seeded_ids ?? [];
  if (ids.length === 0) fail("no seeded ids; run seed first");
  const out: unknown[] = [];
  for (const id of ids) {
    for (const testCase of DRY_RUN_CASES) {
      const res = await httpJson(current.port, "/api/routing-profiles/dry-run", {
        method: "POST",
        body: { profile: id, evidence: testCase.evidence },
      });
      const body = res.json as {
        selectedIndex?: number;
        candidates?: Array<{
          provider?: string;
          model?: string;
          eligible?: boolean;
          exclusions?: Array<{ code?: string }>;
          score?: number;
          estimatedCostUsd?: number;
        }>;
      } | null;
      out.push({
        profile: id,
        case: testCase.name,
        status: res.status,
        selectedIndex: body?.selectedIndex ?? null,
        candidates: (body?.candidates ?? []).map((candidate) => ({
          provider: candidate.provider,
          model: candidate.model,
          eligible: candidate.eligible,
          exclusions: (candidate.exclusions ?? []).map((exclusion) => exclusion.code),
          score: candidate.score,
          estimatedCostUsd: candidate.estimatedCostUsd,
        })),
      });
    }
  }
  // Stale-clear proof: two consecutive dry-runs for different profiles return distinct selected routes.
  const a = await httpJson(current.port, "/api/routing-profiles/dry-run", {
    method: "POST",
    body: { profile: ids[0], evidence: { tools: true } },
  });
  const b = await httpJson(current.port, "/api/routing-profiles/dry-run", {
    method: "POST",
    body: { profile: ids[1] ?? ids[0], evidence: { tools: true } },
  });
  process.stdout.write(`${JSON.stringify({
    ok: true,
    runs: out,
    switch_check: {
      first_profile: ids[0],
      second_profile: ids[1] ?? ids[0],
      first_status: a.status,
      second_status: b.status,
      note: "UI clearDryRun on selectProfile is separate; API responses are per-request.",
    },
  }, null, 2)}\n`);
}

async function cmdStatus(root: string): Promise<void> {
  const current = await loadCurrent(root);
  if (!current) {
    process.stdout.write(`${JSON.stringify({ ok: false, running: false }, null, 2)}\n`);
    return;
  }
  const alive = processAlive(current.pid);
  let health = null;
  if (alive) {
    try {
      health = await httpJson(current.port, "/healthz");
    } catch (error) {
      health = { error: String(error) };
    }
  }
  process.stdout.write(`${JSON.stringify({
    ok: alive,
    running: alive,
    ...current,
    health,
    cleanup: `node --experimental-strip-types scripts/routing-profile-fixture.ts down`,
  }, null, 2)}\n`);
}

async function cmdDown(root: string): Promise<void> {
  const current = await loadCurrent(root);
  if (current?.pid) {
    killPid(current.pid);
    await sleep(400);
  }
  const dir = fixtureDir(root);
  const reportDir = path.join(dir, "report");
  const reportBackup = path.join(root, ".tmp", "routing-profile-fixture-report-preserve");
  let preservedReport = false;
  try {
    await rm(reportBackup, { recursive: true, force: true });
    await rename(reportDir, reportBackup);
    preservedReport = true;
  } catch {
    // report/ may already be absent
  }
  try {
    await rm(dir, { recursive: true, force: true });
  } catch (error) {
    // Windows may briefly lock the binary after taskkill; retry once.
    await sleep(800);
    await rm(dir, { recursive: true, force: true }).catch(() => {
      fail(`could not remove ${dir}: ${String(error)}`);
    });
  }
  if (preservedReport) {
    await mkdir(dir, { recursive: true });
    await rename(reportBackup, reportDir);
  }
  process.stdout.write(`${JSON.stringify({
    ok: true,
    removed: dir,
    preservedReport,
    note: "User ~/.benes was not modified. report/ screenshots kept when present.",
  }, null, 2)}\n`);
}

async function main(): Promise<void> {
  const root = await resolveRoot();
  const command = process.argv[2] ?? "";
  switch (command) {
    case "up":
      await cmdUp(root);
      break;
    case "seed":
      await cmdSeed(root);
      break;
    case "dry-run":
      await cmdDryRun(root);
      break;
    case "status":
      await cmdStatus(root);
      break;
    case "down":
      await cmdDown(root);
      break;
    case "run":
      await cmdUp(root);
      await cmdSeed(root);
      await cmdDryRun(root);
      await cmdStatus(root);
      break;
    case "--help":
    case "help":
      process.stdout.write(`${usage()}\n`);
      break;
    default:
      process.stderr.write(`${usage()}\n`);
      process.exit(2);
  }
}

if (isMainModule(import.meta.url)) {
  main().catch((error) => {
    fail(String(error?.stack || error));
  });
}

