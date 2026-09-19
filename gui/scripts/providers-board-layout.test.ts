/**
 * Providers fleet-board layout regression.
 *
 * Issue #305: the aggregate Providers board is a fixed-height flex column in which the two
 * fact sections keep their natural height and the two list sections take what is left. A
 * section could be sized below the height its own head plus slot needed, so at short
 * viewport heights its content painted over the next section, and the pane clipped whatever
 * reached its edge instead of scrolling it.
 *
 * The contract is geometric, so it is asserted in a real browser against the built
 * dashboard: sections stay in order, no section's content reaches the next section's box,
 * each list section keeps its declared slot minimum, and a pane that cannot fit its
 * sections scrolls them instead of hiding them. Chrome/Edge is driven through the DevTools
 * Protocol over Node's built-in WebSocket, so this check adds no dashboard dependency. It
 * skips with a reason when gui/dist is not built, no Chromium-family browser is installed,
 * or the running Node has no global WebSocket.
 */
import assert from "node:assert/strict";
import { spawn, spawnSync, type ChildProcess } from "node:child_process";
import { existsSync, readFileSync, statSync } from "node:fs";
import { mkdtemp, rm } from "node:fs/promises";
import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const guiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const distRoot = path.join(guiRoot, "dist");
const probeFile = path.join(guiRoot, "scripts", "providers-board-layout-probe.js");
/** Sub-pixel slack: the assertions are about boxes, not about float rounding. */
const EDGE = 0.5;
const BROWSER_START_MS = 30_000;
const PROBE_TIMEOUT_MS = 40_000;

/** Supported viewports from issue #305, plus one shorter stress case. */
const VIEWPORTS = [
  { name: "1920x1080", width: 1920, height: 1080, paneMustFit: true },
  { name: "1440x900", width: 1440, height: 900, paneMustFit: true },
  { name: "1280x800", width: 1280, height: 800, paneMustFit: false },
  { name: "760x900", width: 760, height: 900, paneMustFit: false },
  { name: "1280x700", width: 1280, height: 700, paneMustFit: false },
] as const;

/** Section order and the aria-labels the English catalogue renders for each section. */
const SECTION_LABELS = ["Needs attention", "Availability", "Downstream impact", "Recent events"];

/** The aggregate the listener serves for a fleet whose providers all need attention. */
const WORKSPACE_FIXTURE = {
  summary: { totalProviders: 5, healthy: 0, attention: 5, disabled: 0, exposedModels: 0 },
  providers: [
    { id: "openai", connections: ["openai"], hidden: [], lifecycle: "attention", modelCount: 0, access: { defaultAccess: true, activitySupported: true }, disabled: false, lastValidated: null, downstream: { harnesses: 4, routes: 0, subagents: 0 } },
    { id: "anthropic", connections: ["anthropic"], hidden: [], lifecycle: "attention", modelCount: 0, access: { defaultAccess: false, activitySupported: true }, disabled: false, lastValidated: null, downstream: { harnesses: 3, routes: 0, subagents: 0 } },
    { id: "deepseek", connections: ["deepseek"], hidden: [], lifecycle: "attention", modelCount: 0, access: { defaultAccess: false, activitySupported: true }, disabled: false, lastValidated: null, downstream: { harnesses: 1, routes: 0, subagents: 0 } },
    { id: "openrouter", connections: ["openrouter"], hidden: [], lifecycle: "attention", modelCount: 0, access: { defaultAccess: false, activitySupported: true }, disabled: false, lastValidated: null, downstream: { harnesses: 1, routes: 0, subagents: 0 } },
    { id: "xai", connections: ["xai"], hidden: [], lifecycle: "attention", modelCount: 0, access: { defaultAccess: false, activitySupported: true }, disabled: false, lastValidated: null, downstream: { harnesses: 1, routes: 0, subagents: 0 } },
  ],
  attention: [
    { provider: "openai", code: "quota_exhausted", severity: "warn", timestamp: 1789518979395 },
    { provider: "anthropic", code: "credential_missing", severity: "warn" },
    { provider: "deepseek", code: "credential_missing", severity: "warn" },
    { provider: "openrouter", code: "credential_missing", severity: "warn" },
    { provider: "xai", code: "credential_missing", severity: "warn" },
  ],
  availability: { modelsAvailable: 0, modelsUnavailable: 0, staleProviderCatalogues: null, lastModelSync: null },
  downstream: { harnessCount: 10, routeCount: 0, subAgentModelCount: 0, affectedRouteCount: 0 },
  recentEvents: [
    { provider: "openai", type: "provider_health_failure", severity: "error", timestamp: 1789518000000 },
    { provider: "anthropic", type: "credentials_validated", severity: "info", timestamp: 1789514400000 },
    { provider: "openrouter", type: "model_catalogue_stale", severity: "warn", timestamp: 1789507200000 },
    { provider: "deepseek", type: "model_catalogue_synchronized", severity: "info", timestamp: 1789500000000 },
  ],
};

const CONFIG_FIXTURE = {
  defaultProvider: "openai",
  providers: {
    anthropic: { adapter: "anthropic-messages", authMode: "key", baseUrl: "https://api.anthropic.com", hasApiKey: false },
    deepseek: { adapter: "openai-chat", authMode: "key", baseUrl: "https://api.deepseek.com/v1", hasApiKey: false },
    openai: { adapter: "openai-responses", authMode: "forward", baseUrl: "https://chatgpt.com/backend-api/codex", codexAccountMode: "pool", hasApiKey: false },
    openrouter: { adapter: "openai-chat", authMode: "key", baseUrl: "https://openrouter.ai/api/v1", hasApiKey: false },
    xai: { adapter: "openai-chat", authMode: "key", baseUrl: "https://api.x.ai/v1", hasApiKey: false },
  },
};

const CONTENT_TYPES: Record<string, string> = {
  ".css": "text/css; charset=utf-8",
  ".html": "text/html; charset=utf-8",
  ".ico": "image/x-icon",
  ".js": "text/javascript; charset=utf-8",
  ".json": "application/json",
  ".map": "application/json",
  ".png": "image/png",
  ".svg": "image/svg+xml",
  ".woff2": "font/woff2",
};

type Box = { top: number; bottom: number; left: number; right: number; width: number; height: number };
type SlotReceipt = { box: Box; minHeight: number; clientHeight: number; scrollHeight: number; overflowY: string };
type SectionReceipt = {
  label: string | null;
  heading: string | null;
  box: Box;
  clientHeight: number;
  scrollHeight: number;
  minHeight: string;
  flexGrow: string;
  flexShrink: string;
  flexBasis: string;
  overflowY: string;
  contentBottom: number;
  contentTail: string;
  slot: SlotReceipt | null;
};
type Receipt = {
  viewport: { width: number; height: number; devicePixelRatio: number };
  board: { box: Box; clientHeight: number; scrollHeight: number; scrollTop: number; overflowY: string; minHeight: string };
  document: { scrollWidth: number; clientWidth: number; scrollHeight: number; clientHeight: number; horizontalOverflow: boolean };
  sections: SectionReceipt[];
};
type Viewport = (typeof VIEWPORTS)[number];
type CdpReply = { id?: number; result?: Record<string, unknown>; error?: { message?: string } };

function deferred<T>(): { promise: Promise<T>; resolve: (value: T) => void } {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((settle) => { resolve = settle; });
  return { promise, resolve };
}

function browserCandidates(): string[] {
  const names = ["google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome", "msedge"];
  const fromPath = (process.env.PATH ?? "")
    .split(path.delimiter)
    .filter(Boolean)
    .flatMap((directory) => names.flatMap((name) => [
      path.join(directory, name),
      path.join(directory, name + ".exe"),
    ]));
  const fromSystem = process.platform === "win32"
    ? [
      path.join(process.env.PROGRAMFILES ?? "", "Google", "Chrome", "Application", "chrome.exe"),
      path.join(process.env["PROGRAMFILES(X86)"] ?? "", "Google", "Chrome", "Application", "chrome.exe"),
      path.join(process.env.LOCALAPPDATA ?? "", "Google", "Chrome", "Application", "chrome.exe"),
      path.join(process.env.PROGRAMFILES ?? "", "Microsoft", "Edge", "Application", "msedge.exe"),
      path.join(process.env["PROGRAMFILES(X86)"] ?? "", "Microsoft", "Edge", "Application", "msedge.exe"),
    ]
    : ["/usr/bin/google-chrome", "/usr/bin/google-chrome-stable", "/usr/bin/chromium", "/usr/bin/chromium-browser", "/opt/google/chrome/chrome", "/snap/bin/chromium"];
  const requested = (process.env.BENES_LAYOUT_BROWSER ?? "").trim();
  return [requested, ...fromPath, ...fromSystem].filter((candidate) => candidate.length > 0);
}

function findBrowser(): string | null {
  for (const candidate of browserCandidates()) {
    try {
      if (statSync(candidate).isFile()) return candidate;
    } catch {
      // Not this candidate; keep looking.
    }
  }
  return null;
}

/** One DevTools Protocol connection: flat sessions, id-matched replies. */
class DevtoolsConnection {
  readonly #socket: WebSocket;
  readonly #pending = new Map<number, { settle: (reply: CdpReply) => void }>();
  #nextId = 1;

  constructor(socket: WebSocket) {
    this.#socket = socket;
    socket.addEventListener("message", (event: MessageEvent) => {
      const reply = JSON.parse(String(event.data)) as CdpReply;
      if (typeof reply.id !== "number") return;
      const waiter = this.#pending.get(reply.id);
      if (!waiter) return;
      this.#pending.delete(reply.id);
      waiter.settle(reply);
    });
  }

  static async connect(endpoint: string, timeoutMs: number): Promise<DevtoolsConnection> {
    const socket = new WebSocket(endpoint);
    await new Promise<void>((ready, failed) => {
      const timer = setTimeout(() => { failed(new Error("DevTools socket did not open within " + timeoutMs + "ms")); }, timeoutMs);
      socket.addEventListener("open", () => { clearTimeout(timer); ready(); });
      socket.addEventListener("error", () => { clearTimeout(timer); failed(new Error("DevTools socket failed to open")); });
    });
    return new DevtoolsConnection(socket);
  }

  async send(method: string, params: Record<string, unknown> = {}, sessionId?: string, timeoutMs = PROBE_TIMEOUT_MS): Promise<Record<string, unknown>> {
    const id = this.#nextId++;
    const waiter = deferred<CdpReply>();
    this.#pending.set(id, { settle: waiter.resolve });
    // This test sends repository-owned probe source only to a loopback Chromium DevTools socket.
    this.#socket.send(JSON.stringify(sessionId ? { id, method, params, sessionId } : { id, method, params }));
    let timer: NodeJS.Timeout | undefined;
    const expired = new Promise<CdpReply>((resolve) => {
      timer = setTimeout(() => { resolve({ id, error: { message: method + " did not answer within " + timeoutMs + "ms" } }); }, timeoutMs);
    });
    try {
      const reply = await Promise.race([waiter.promise, expired]);
      if (reply.error) throw new Error(method + " failed: " + (reply.error.message ?? "unknown error"));
      return reply.result ?? {};
    } finally {
      clearTimeout(timer);
      this.#pending.delete(id);
    }
  }

  close(): void {
    this.#socket.close();
  }
}

function listeningEndpoint(child: ChildProcess): Promise<string> {
  return new Promise<string>((resolve, reject) => {
    let buffered = "";
    const inspect = (chunk: unknown): void => {
      buffered += String(chunk);
      const match = /DevTools listening on (ws:\/\/\S+)/.exec(buffered);
      if (match) {
        clearTimeout(timer);
        resolve(match[1]!);
      }
    };
    const timer = setTimeout(() => { reject(new Error("browser did not report a DevTools endpoint within " + BROWSER_START_MS + "ms")); }, BROWSER_START_MS);
    child.stderr?.on("data", inspect);
    child.stdout?.on("data", inspect);
    child.on("exit", () => { clearTimeout(timer); reject(new Error("browser exited before reporting a DevTools endpoint")); });
  });
}

function launchBrowser(browser: string, profile: string): ChildProcess {
  return spawn(browser, [
    "--headless=new",
    "--remote-debugging-port=0",
    "--user-data-dir=" + profile,
    "--no-first-run",
    "--no-default-browser-check",
    "--disable-extensions",
    "--disable-background-networking",
    "--disable-component-update",
    "--disable-sync",
    "--disable-gpu",
    "--disable-dev-shm-usage",
    "--force-device-scale-factor=1",
    "--lang=en-US",
    "about:blank",
  ], { stdio: ["ignore", "pipe", "pipe"], windowsHide: true });
}

/**
 * Stop only the browser this check started. Chrome parents a process tree, and on Windows
 * the launcher's own handle keeps the test process alive until that tree is gone.
 */
async function stopBrowser(child: ChildProcess): Promise<void> {
  if (!child.pid) return;
  if (process.platform === "win32") {
    spawnSync("taskkill", ["/PID", String(child.pid), "/T", "/F"], { stdio: "ignore", windowsHide: true });
  } else {
    child.kill("SIGKILL");
  }
  if (child.exitCode !== null || child.signalCode !== null) return;
  await new Promise<void>((done) => {
    const timer = setTimeout(done, 8_000);
    child.once("exit", () => { clearTimeout(timer); done(); });
  });
}

function sendJson(res: ServerResponse, payload: unknown): void {
  const body = JSON.stringify(payload);
  res.writeHead(200, { "content-type": "application/json", "content-length": Buffer.byteLength(body) });
  res.end(body);
}

function sendFile(res: ServerResponse, file: string): void {
  if (!path.resolve(file).startsWith(path.resolve(distRoot) + path.sep) || !existsSync(file)) {
    res.writeHead(404).end("not found");
    return;
  }
  res.writeHead(200, { "content-type": CONTENT_TYPES[path.extname(file)] ?? "application/octet-stream" });
  res.end(readFileSync(file));
}

function handleRequest(req: IncomingMessage, res: ServerResponse): void {
  const url = new URL(req.url ?? "/", "http://127.0.0.1");
  if (url.pathname === "/api/config") return sendJson(res, CONFIG_FIXTURE);
  if (url.pathname === "/api/providers/workspace") return sendJson(res, WORKSPACE_FIXTURE);
  if (url.pathname.startsWith("/api/")) return sendJson(res, {});
  if (url.pathname === "/") {
    res.writeHead(200, { "content-type": CONTENT_TYPES[".html"]! });
    res.end(readFileSync(path.join(distRoot, "index.html")));
    return;
  }
  sendFile(res, path.join(distRoot, url.pathname));
}

/** Serves the built dashboard plus the deterministic reader fixtures it needs. */
function fixtureServer(): Server {
  return createServer((req, res) => { handleRequest(req, res); });
}

async function measureViewport(
  connection: DevtoolsConnection,
  origin: string,
  viewport: Viewport,
  probe: string,
): Promise<Receipt> {
  const target = await connection.send("Target.createTarget", { url: "about:blank" });
  const attached = await connection.send("Target.attachToTarget", { targetId: target.targetId, flatten: true });
  const sessionId = String(attached.sessionId);
  try {
    await connection.send("Emulation.setDeviceMetricsOverride", {
      width: viewport.width,
      height: viewport.height,
      deviceScaleFactor: 1,
      mobile: false,
    }, sessionId);
    await connection.send("Page.enable", {}, sessionId);
    await connection.send("Page.navigate", { url: origin + "/#providers" }, sessionId);
    const evaluated = await connection.send("Runtime.evaluate", {
      expression: "(" + probe + ")()",
      awaitPromise: true,
      returnByValue: true,
    }, sessionId);
    const details = evaluated.exceptionDetails as { text?: string; exception?: { description?: string } } | undefined;
    if (details) {
      throw new Error(viewport.name + ": the layout probe failed: " + (details.exception?.description ?? details.text ?? "unknown error"));
    }
    return (evaluated.result as { value?: unknown }).value as Receipt;
  } finally {
    await connection.send("Target.closeTarget", { targetId: target.targetId }).catch(() => undefined);
  }
}

function assertViewportWasHonored(receipt: Receipt, viewport: Viewport): void {
  const actual = receipt.viewport;
  assert.ok(
    Math.abs(actual.width - viewport.width) <= 1 && Math.abs(actual.height - viewport.height) <= 1,
    viewport.name + ": the browser rendered a " + actual.width + "x" + actual.height + " viewport",
  );
}

function assertSectionOrder(receipt: Receipt, where: string): void {
  assert.deepEqual(receipt.sections.map((section) => section.label), SECTION_LABELS, where + ": four sections in order");
}

function assertNoSectionReachesTheNext(receipt: Receipt, where: string): void {
  for (let index = 0; index + 1 < receipt.sections.length; index += 1) {
    const previous = receipt.sections[index]!;
    const next = receipt.sections[index + 1]!;
    assert.ok(
      previous.box.bottom <= next.box.top + EDGE,
      where + ": " + previous.label + " box ends at " + previous.box.bottom + ", past " + next.label + " top " + next.box.top,
    );
    assert.ok(
      previous.contentBottom <= next.box.top + EDGE,
      where + ": " + previous.label + " content reaches " + previous.contentBottom + " (" + previous.contentTail + "), past " + next.label + " top " + next.box.top,
    );
  }
}

function assertSectionsHoldTheirOwnContent(receipt: Receipt, where: string): void {
  for (const section of receipt.sections) {
    assert.ok(
      section.contentBottom <= section.box.bottom + EDGE,
      where + ": " + section.label + " content reaches " + section.contentBottom + " (" + section.contentTail + "), past its own box bottom " + section.box.bottom + " (min-height " + section.minHeight + ")",
    );
    if (section.slot) {
      assert.ok(
        section.slot.box.height >= section.slot.minHeight - EDGE,
        where + ": " + section.label + " slot is " + section.slot.box.height + "px tall, below its declared minimum " + section.slot.minHeight + "px",
      );
    }
  }
}

function assertPaneOwnsItsOverflow(receipt: Receipt, viewport: Viewport): void {
  const clipped = receipt.board.scrollHeight - receipt.board.clientHeight;
  if (clipped > EDGE) {
    assert.ok(
      receipt.board.overflowY === "auto" || receipt.board.overflowY === "scroll",
      viewport.name + ": the pane hides " + clipped + "px of its sections with overflow-y " + receipt.board.overflowY,
    );
  }
  if (viewport.paneMustFit) {
    assert.ok(clipped <= EDGE, viewport.name + ": the pane overflows by " + clipped + "px, which is a scrollbar at the reference desktop size");
  }
}

function assertReceipt(receipt: Receipt, viewport: Viewport): void {
  if (!receipt || !receipt.sections) assert.fail(viewport.name + ": the layout probe returned no receipt");
  assertViewportWasHonored(receipt, viewport);
  assertSectionOrder(receipt, viewport.name);
  assertNoSectionReachesTheNext(receipt, viewport.name);
  assertSectionsHoldTheirOwnContent(receipt, viewport.name);
  assertPaneOwnsItsOverflow(receipt, viewport);
  assert.equal(receipt.document.horizontalOverflow, false, viewport.name + ": the page has no horizontal overflow");
}

const browser = findBrowser();
const distBuilt = existsSync(path.join(distRoot, "index.html"));
const skipReason = !distBuilt
  ? "gui/dist is not built; run npm run build first"
  : !browser
    ? "no Chromium-family browser found; install Chrome/Edge or set BENES_LAYOUT_BROWSER"
    : typeof WebSocket !== "function"
      ? "this Node has no global WebSocket, so the layout probe cannot drive the browser"
      : false;

test("the Providers fleet board keeps every section inside its own box at short viewports", { skip: skipReason, timeout: 240_000 }, async () => {
  const probe = readFileSync(probeFile, "utf8");
  const server = fixtureServer();
  await new Promise<void>((ready) => { server.listen(0, "127.0.0.1", ready); });
  const address = server.address();
  const origin = "http://127.0.0.1:" + (typeof address === "object" && address ? address.port : 0);
  const profile = await mkdtemp(path.join(tmpdir(), "benes-board-layout-"));
  const child = launchBrowser(browser!, profile);
  try {
    const connection = await DevtoolsConnection.connect(await listeningEndpoint(child), BROWSER_START_MS);
    try {
      for (const viewport of VIEWPORTS) {
        assertReceipt(await measureViewport(connection, origin, viewport, probe), viewport);
      }
    } finally {
      connection.close();
    }
  } finally {
    await stopBrowser(child);
    // The dashboard keeps reader connections open, so the server needs them dropped
    // before close() rather than waiting for them to idle out.
    server.closeAllConnections();
    server.close();
    await new Promise((pause) => { setTimeout(pause, 200); });
    await rm(profile, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 }).catch(() => undefined);
  }
});
