import { promptForAdminToken, type AdminTokenVerifier } from "./admin-token-dialog.ts";
import { createBoundedFetch } from "./bounded-fetch.ts";

const API_KEY_HEADER = "X-Benes-API-Key";
const GUI_ORIGIN_HEADER = "X-Benes-GUI-Origin";
const CSRF_HEADER = "X-Benes-CSRF-Token";
const SESSION_TOKEN_PREFIX = "benes_session_";
const SESSION_META = {
  token: "benes-session-token",
  csrf: "benes-session-csrf",
  origin: "benes-session-origin",
} as const;
const SESSION_BOOTSTRAP_PATH = "/benes-session";
const ADMIN_VALIDATE_PATH = "/api/settings";
const LEGACY_TOKEN_STORAGE_KEY = "benes-api-token";
const DEFAULT_BOOTSTRAP_MS = 10_000;
const DEFAULT_WATCHDOG_MS = 15_000;

type AdminTokenPrompt = (verifyToken: AdminTokenVerifier) => Promise<string | null>;
type MaybeSecret = string | null;
type FetchCall = { target: RequestInfo | URL; init?: RequestInit };
type Rebootstrap =
  | { kind: "minted"; token: string }
  | { kind: "unavailable" }
  | { kind: "failed" };

class CredentialVault {
  secret: MaybeSecret = null;
  csrf: MaybeSecret = null;
  origin: MaybeSecret = null;

  reset(): void {
    this.secret = null;
    this.csrf = null;
    this.origin = null;
  }

  dropIfCurrent(expected: MaybeSecret): void {
    if (expected != null && this.secret === expected) this.reset();
  }

  putAdmin(secret: string): void {
    this.secret = secret;
  }

  putSession(secret: MaybeSecret, csrf: MaybeSecret, origin: MaybeSecret): boolean {
    if (!secret?.startsWith(SESSION_TOKEN_PREFIX) || !csrf || origin !== window.location.origin) return false;
    this.secret = secret;
    this.csrf = csrf;
    this.origin = origin;
    return true;
  }
}

function hrefOf(target: RequestInfo | URL): string {
  return target instanceof Request ? target.url : String(target);
}

function sameOriginManagement(target: RequestInfo | URL): boolean {
  try {
    const url = new URL(hrefOf(target), window.location.href);
    return url.origin === window.location.origin && url.pathname.startsWith("/api/");
  } catch {
    return false;
  }
}

function methodOf(call: FetchCall): string {
  if (call.init?.method) return call.init.method.toUpperCase();
  return (call.target instanceof Request ? call.target.method : "GET").toUpperCase();
}

function headerSeed(call: FetchCall): HeadersInit | undefined {
  if (call.init?.headers) return call.init.headers;
  return call.target instanceof Request ? call.target.headers : undefined;
}

function dispatch(send: typeof fetch, call: FetchCall): Promise<Response> {
  return send(call.target, call.init);
}

function blankToNull(value: string | null | undefined): MaybeSecret {
  const trimmed = value?.trim();
  return trimmed ? trimmed : null;
}

function takeMeta(name: string): MaybeSecret {
  const element = document.querySelector(`meta[name="${name}"]`) as HTMLMetaElement | null;
  const content = blankToNull(element?.content);
  element?.remove();
  return content;
}

function metaFromHtml(html: string, name: string): MaybeSecret {
  for (const tag of html.match(/<meta\b[^>]*>/gi) ?? []) {
    const nameMatch = tag.match(/\bname="([^"]+)"/i);
    if (nameMatch?.[1] !== name) continue;
    const contentMatch = tag.match(/\bcontent="([^"]*)"/i);
    return blankToNull(contentMatch?.[1]);
  }
  return null;
}

function firstOf<T>(left: Promise<T>, right: Promise<T>): Promise<T> {
  return Promise.race([left, right]);
}

function dropLegacyStoredToken(): void {
  try {
    sessionStorage.removeItem(LEGACY_TOKEN_STORAGE_KEY);
  } catch {
    /* storage may be unavailable */
  }
}

class GuiAuthRuntime {
  installed = false;
  raw: typeof fetch | null = null;
  promptClosed = false;
  shared: Promise<MaybeSecret> | null = null;
  askForToken: AdminTokenPrompt = promptForAdminToken;
  bootstrapMs = DEFAULT_BOOTSTRAP_MS;
  watchdogMs = DEFAULT_WATCHDOG_MS;
  readonly vault = new CredentialVault();

  authorize(call: FetchCall, token: string): FetchCall {
    const headers = new Headers(headerSeed(call));
    headers.set(API_KEY_HEADER, token);
    if (this.vault.origin && this.vault.csrf && token.startsWith(SESSION_TOKEN_PREFIX)) {
      headers.set(GUI_ORIGIN_HEADER, this.vault.origin);
      const method = methodOf(call);
      if (method !== "GET" && method !== "HEAD") headers.set(CSRF_HEADER, this.vault.csrf);
    }
    if (call.target instanceof Request) {
      return {
        target: new Request(call.target, { headers }),
        init: call.init ? { ...call.init, headers } : undefined,
      };
    }
    return { target: call.target, init: { ...call.init, headers } };
  }

  async mintSession(): Promise<Rebootstrap> {
    if (!this.raw) return { kind: "failed" };
    const deadline = createBoundedFetch(this.bootstrapMs);
    try {
      const response = await this.raw(SESSION_BOOTSTRAP_PATH, { cache: "no-store", signal: deadline.signal });
      if (!response.ok) return response.status >= 400 && response.status < 500 ? { kind: "unavailable" } : { kind: "failed" };
      const html = await response.text();
      const stored = this.vault.putSession(
        metaFromHtml(html, SESSION_META.token),
        metaFromHtml(html, SESSION_META.csrf),
        metaFromHtml(html, SESSION_META.origin),
      );
      const token = this.vault.secret;
      if (stored && token) return { kind: "minted", token };
      return { kind: "unavailable" };
    } catch {
      return { kind: "failed" };
    } finally {
      deadline.clear();
    }
  }

  async verifyAdmin(token: string): ReturnType<AdminTokenVerifier> {
    if (!this.raw) return "unavailable";
    try {
      const response = await dispatch(this.raw, this.authorize({ target: ADMIN_VALIDATE_PATH, init: { cache: "no-store" } }, token));
      if (response.status === 401) return "rejected";
      return response.ok ? "accepted" : "unavailable";
    } catch {
      return "unavailable";
    }
  }

  async guardedMint(): Promise<Rebootstrap> {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const timeout = new Promise<Rebootstrap>((resolve) => {
      timer = setTimeout(() => resolve({ kind: "failed" }), this.watchdogMs);
    });
    return firstOf(this.mintSession(), timeout).finally(() => clearTimeout(timer));
  }

  async runShared(failedToken: MaybeSecret): Promise<MaybeSecret> {
    if (this.promptClosed) return null;
    const already = this.vault.secret;
    if (already && already !== failedToken) return already;
    const renewed = await this.guardedMint();
    if (renewed.kind === "minted") return renewed.token;
    if (renewed.kind === "failed") return null;
    const prompted = await this.askForToken((token) => this.verifyAdmin(token));
    if (prompted) {
      this.vault.putAdmin(prompted);
      return prompted;
    }
    this.promptClosed = true;
    return null;
  }

  joinShared(shared: Promise<MaybeSecret>, callerSignal?: AbortSignal): Promise<MaybeSecret> {
    if (!callerSignal) return shared;
    let onAbort: (() => void) | undefined;
    const aborted = new Promise<null>((resolve) => {
      onAbort = () => resolve(null);
      callerSignal.addEventListener("abort", onAbort, { once: true });
    });
    return firstOf(shared, aborted).finally(() => {
      if (onAbort) callerSignal.removeEventListener("abort", onAbort);
    });
  }

  afterUnauthorized(failedToken: MaybeSecret, callerSignal?: AbortSignal): Promise<MaybeSecret> {
    if (this.promptClosed) return Promise.resolve(null);
    if (callerSignal?.aborted) return Promise.resolve(null);
    if (!this.shared) {
      const body = this.runShared(failedToken);
      const tracked = body.finally(() => {
        if (this.shared === tracked) this.shared = null;
      });
      this.shared = tracked;
    }
    return this.joinShared(this.shared, callerSignal);
  }

  callerSignal(call: FetchCall): AbortSignal | undefined {
    return call.init?.signal ?? (call.target instanceof Request ? call.target.signal : undefined);
  }

  async retry(send: typeof fetch, call: FetchCall, token: string): Promise<Response> {
    const retry = await dispatch(send, this.authorize(call, token));
    if (retry.status === 401) this.vault.dropIfCurrent(token);
    return retry;
  }

  async recover(send: typeof fetch, call: FetchCall, failedToken: MaybeSecret, original401: Response): Promise<Response> {
    const latest = this.vault.secret;
    if (latest && latest !== failedToken) {
      const raced = await this.retry(send, call, latest);
      if (raced.status !== 401) return raced;
    } else {
      this.vault.dropIfCurrent(failedToken);
    }
    const nextToken = await this.afterUnauthorized(failedToken, this.callerSignal(call));
    if (!nextToken) return original401;
    return this.retry(send, call, nextToken);
  }

  async intercept(send: typeof fetch, call: FetchCall): Promise<Response> {
    const token = this.vault.secret;
    const first = token ? this.authorize(call, token) : call;
    const response = await dispatch(send, first);
    if (response.status !== 401) return response;
    return this.recover(send, call, token, response);
  }

  install(): void {
    if (this.installed) return;
    this.installed = true;
    dropLegacyStoredToken();
    this.vault.putSession(takeMeta(SESSION_META.token), takeMeta(SESSION_META.csrf), takeMeta(SESSION_META.origin));
    const originalFetch = window.fetch.bind(window);
    this.raw = originalFetch;
    window.fetch = (target: RequestInfo | URL, init?: RequestInit) => {
      if (!sameOriginManagement(target)) return originalFetch(target, init);
      return this.intercept(originalFetch, { target, init });
    };
  }

  reset(adminTokenPrompt: AdminTokenPrompt = promptForAdminToken): void {
    this.installed = false;
    this.vault.reset();
    this.shared = null;
    this.raw = null;
    this.promptClosed = false;
    this.askForToken = adminTokenPrompt;
    this.bootstrapMs = DEFAULT_BOOTSTRAP_MS;
    this.watchdogMs = DEFAULT_WATCHDOG_MS;
  }
}

const runtime = new GuiAuthRuntime();

export function installApiAuthFetch(): void {
  runtime.install();
}

export function resetApiAuthFetchForTests(adminTokenPrompt: AdminTokenPrompt = promptForAdminToken): void {
  runtime.reset(adminTokenPrompt);
}

export function setRebootstrapTimeoutForTests(ms: number): void {
  runtime.bootstrapMs = ms;
}

export function setResolutionWatchdogForTests(ms: number): void {
  runtime.watchdogMs = ms;
}
