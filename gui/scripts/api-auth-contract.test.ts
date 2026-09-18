import assert from "node:assert/strict";
import test from "node:test";

import {
  installApiAuthFetch,
  resetApiAuthFetchForTests,
  setRebootstrapTimeoutForTests,
  setResolutionWatchdogForTests,
} from "../src/api.ts";
import { addMeta, installGuiDom } from "./gui-dom-harness.ts";

const API_KEY = "X-Benes-API-Key";
const ORIGIN = "X-Benes-GUI-Origin";
const CSRF = "X-Benes-CSRF-Token";

function pathnameOf(input: RequestInfo | URL): string {
  return new URL(input instanceof Request ? input.url : String(input), "http://127.0.0.1:23100/").pathname;
}

function sessionHtml(token: string, csrf: string, origin: string): string {
  return `<html><head><meta name="benes-session-token" content="${token}"><meta name="benes-session-csrf" content="${csrf}"><meta name="benes-session-origin" content="${origin}"></head></html>`;
}

function headersOf(input: RequestInfo | URL, init?: RequestInit): Headers {
  return new Headers(init?.headers ?? (input instanceof Request ? input.headers : undefined));
}

async function withAuthDom(run: (dom: ReturnType<typeof installGuiDom>) => Promise<void> | void): Promise<void> {
  const dom = installGuiDom();
  resetApiAuthFetchForTests(async () => null);
  try {
    await run(dom);
  } finally {
    resetApiAuthFetchForTests();
    setRebootstrapTimeoutForTests(10_000);
    setResolutionWatchdogForTests(15_000);
    dom.restore();
  }
}

function installMock(dom: ReturnType<typeof installGuiDom>, handler: typeof fetch): void {
  dom.window.fetch = handler;
  installApiAuthFetch();
}

test("installation is idempotent and captures raw fetch once", async () => {
  await withAuthDom(async (dom) => {
    let calls = 0;
    const handler = (async () => {
      calls += 1;
      return new Response("{}", { status: 200 });
    }) as typeof fetch;
    installMock(dom, handler);
    const wrapped = dom.window.fetch;
    installApiAuthFetch();
    assert.equal(dom.window.fetch, wrapped);
    await wrapped("/api/config");
    assert.equal(calls, 1);
  });
});

test("non-API, cross-origin, and malformed URLs never receive local credentials", async () => {
  await withAuthDom(async (dom) => {
    const seen: Array<string | null> = [];
    const handler = (async (input: RequestInfo | URL, init?: RequestInit) => {
      seen.push(headersOf(input, init).get(API_KEY));
      return new Response("{}", { status: 200 });
    }) as typeof fetch;
    addMeta("benes-session-token", "benes_session_live");
    addMeta("benes-session-csrf", "csrf");
    addMeta("benes-session-origin", "http://127.0.0.1:23100");
    installMock(dom, handler);

    await dom.window.fetch("/v1/models");
    await dom.window.fetch("https://evil.example/api/config");
    await dom.window.fetch("http://[::not-a-url");
    assert.deepEqual(seen, [null, null, null]);
  });
});

test("injected session metadata is consumed, origin-checked, and used on local API requests", async () => {
  await withAuthDom(async (dom) => {
    const seen: Array<[string | null, string | null, string | null]> = [];
    const handler = (async (input: RequestInfo | URL, init?: RequestInit) => {
      const headers = headersOf(input, init);
      seen.push([headers.get(API_KEY), headers.get(ORIGIN), headers.get(CSRF)]);
      return new Response("{}", { status: 200 });
    }) as typeof fetch;
    addMeta("benes-session-token", "benes_session_live");
    addMeta("benes-session-csrf", "csrf-1");
    addMeta("benes-session-origin", "http://127.0.0.1:23100");
    installMock(dom, handler);
    assert.equal(dom.document.querySelector('meta[name="benes-session-token"]'), null);

    await dom.window.fetch("/api/config");
    await dom.window.fetch("/api/providers", { method: "PATCH" });
    assert.deepEqual(seen, [
      ["benes_session_live", "http://127.0.0.1:23100", null],
      ["benes_session_live", "http://127.0.0.1:23100", "csrf-1"],
    ]);
  });
});

test("mismatched injected session origin is rejected", async () => {
  await withAuthDom(async (dom) => {
    const seen: Array<string | null> = [];
    const handler = (async (input: RequestInfo | URL, init?: RequestInit) => {
      seen.push(headersOf(input, init).get(API_KEY));
      return new Response("{}", { status: 200 });
    }) as typeof fetch;
    addMeta("benes-session-token", "benes_session_live");
    addMeta("benes-session-csrf", "csrf-1");
    addMeta("benes-session-origin", "http://192.0.2.10:23100");
    installMock(dom, handler);
    await dom.window.fetch("/api/config");
    assert.deepEqual(seen, [null]);
  });
});

test("legacy sessionStorage token is deleted and never read as a secret", async () => {
  await withAuthDom(async (dom) => {
    dom.sessionStorage.setItem("benes-api-token", "legacy-secret");
    let reads = 0;
    const original = dom.sessionStorage.getItem.bind(dom.sessionStorage);
    dom.sessionStorage.getItem = ((key: string) => {
      reads += 1;
      return original(key);
    }) as typeof dom.sessionStorage.getItem;
    installMock(dom, (async () => new Response("{}", { status: 200 })) as typeof fetch);
    assert.equal(reads, 0);
    assert.equal(original("benes-api-token"), null);
    assert.equal(dom.sessionStorage.length, 0);
  });
});

test("Request method and headers follow init-overrides-Request fetch semantics", async () => {
  await withAuthDom(async (dom) => {
    const seen: Array<{ method: string; csrf: string | null; extra: string | null }> = [];
    const handler = (async (input: RequestInfo | URL, init?: RequestInit) => {
      const method = init?.method ?? (input instanceof Request ? input.method : "GET");
      const headers = headersOf(input, init);
      seen.push({ method: method.toUpperCase(), csrf: headers.get(CSRF), extra: headers.get("X-Extra") });
      return new Response("{}", { status: 200 });
    }) as typeof fetch;
    addMeta("benes-session-token", "benes_session_live");
    addMeta("benes-session-csrf", "csrf-1");
    addMeta("benes-session-origin", "http://127.0.0.1:23100");
    installMock(dom, handler);

    await dom.window.fetch(new Request("http://127.0.0.1:23100/api/config"));
    await dom.window.fetch(new Request("http://127.0.0.1:23100/api/providers", { method: "POST" }));
    await dom.window.fetch(new Request("http://127.0.0.1:23100/api/config", { method: "GET" }), { method: "PATCH" });
    await dom.window.fetch(
      new Request("http://127.0.0.1:23100/api/providers", { method: "POST", headers: { "X-Extra": "from-request" } }),
      { method: "POST", headers: { "X-Extra": "from-init" } },
    );
    assert.deepEqual(seen, [
      { method: "GET", csrf: null, extra: null },
      { method: "POST", csrf: "csrf-1", extra: null },
      { method: "PATCH", csrf: "csrf-1", extra: null },
      { method: "POST", csrf: "csrf-1", extra: "from-init" },
    ]);
  });
});

test("authenticated GET Request retries; body-bearing Request is not a dashboard retry contract", async () => {
  await withAuthDom(async (dom) => {
    let calls = 0;
    const handler = (async (input: RequestInfo | URL, init?: RequestInit) => {
      calls += 1;
      const key = headersOf(input, init).get(API_KEY);
      if (pathnameOf(input) === "/benes-session") return new Response("no", { status: 401 });
      if (key === "admin") return new Response("{}", { status: 200 });
      return new Response("unauthorized", { status: 401 });
    }) as typeof fetch;
    resetApiAuthFetchForTests(async () => "admin");
    installMock(dom, handler);
    const getRequest = new Request("http://127.0.0.1:23100/api/config");
    assert.equal((await dom.window.fetch(getRequest)).status, 200);
    assert.equal(calls >= 2, true);

    const postRequest = new Request("http://127.0.0.1:23100/api/providers", {
      method: "POST",
      body: JSON.stringify({ name: "x" }),
    });
    const posted = await dom.window.fetch(postRequest);
    assert.equal([200, 401].includes(posted.status), true);
  });
});

test("GET/HEAD omit CSRF; Request inputs keep required headers", async () => {
  await withAuthDom(async (dom) => {
    const csrf: Array<string | null> = [];
    const handler = (async (input: RequestInfo | URL, init?: RequestInit) => {
      csrf.push(headersOf(input, init).get(CSRF));
      return new Response("{}", { status: 200 });
    }) as typeof fetch;
    addMeta("benes-session-token", "benes_session_live");
    addMeta("benes-session-csrf", "csrf-1");
    addMeta("benes-session-origin", "http://127.0.0.1:23100");
    installMock(dom, handler);
    await dom.window.fetch("/api/config", { method: "HEAD" });
    await dom.window.fetch(new Request("http://127.0.0.1:23100/api/providers", { method: "POST" }));
    assert.deepEqual(csrf, [null, "csrf-1"]);
  });
});

test("concurrent 401s share one prompt and retry with the minted admin token", async () => {
  await withAuthDom(async (dom) => {
    let prompts = 0;
    resetApiAuthFetchForTests(async (verify) => {
      prompts += 1;
      assert.equal(await verify("fresh-token"), "accepted");
      return "fresh-token";
    });
    const handler = (async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = pathnameOf(input);
      const key = headersOf(input, init).get(API_KEY);
      if (path === "/benes-session") return new Response("no", { status: 401 });
      if (path === "/api/settings" && key === "fresh-token") return new Response("{}", { status: 200 });
      if (key === "fresh-token") return new Response("{}", { status: 200 });
      return new Response("unauthorized", { status: 401 });
    }) as typeof fetch;
    installMock(dom, handler);
    const statuses = await Promise.all([
      dom.window.fetch("/api/config"),
      dom.window.fetch("/api/providers"),
      dom.window.fetch("/api/models"),
    ].map(async (p) => (await p).status));
    assert.equal(prompts, 1);
    assert.deepEqual([...new Set(statuses)], [200]);
    assert.equal(dom.sessionStorage.length, 0);
  });
});

test("cancelling the prompt suppresses later prompt storms", async () => {
  await withAuthDom(async (dom) => {
    let prompts = 0;
    resetApiAuthFetchForTests(async () => {
      prompts += 1;
      return null;
    });
    const handler = (async (input: RequestInfo | URL) => {
      if (pathnameOf(input) === "/benes-session") return new Response("no", { status: 401 });
      return new Response("unauthorized", { status: 401 });
    }) as typeof fetch;
    installMock(dom, handler);
    assert.equal((await dom.window.fetch("/api/config")).status, 401);
    assert.equal((await dom.window.fetch("/api/providers")).status, 401);
    assert.equal(prompts, 1);
  });
});

test("silent rebootstrap retries with a minted session and does not prompt", async () => {
  await withAuthDom(async (dom) => {
    let prompts = 0;
    resetApiAuthFetchForTests(async () => {
      prompts += 1;
      return null;
    });
    addMeta("benes-session-token", "benes_session_stale");
    addMeta("benes-session-csrf", "stale");
    addMeta("benes-session-origin", "http://127.0.0.1:23100");
    const keys: string[] = [];
    const handler = (async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = pathnameOf(input);
      if (path === "/benes-session") {
        return new Response(sessionHtml("benes_session_fresh", "fresh", "http://127.0.0.1:23100"), { status: 200 });
      }
      keys.push(headersOf(input, init).get(API_KEY) ?? "");
      if (headersOf(input, init).get(API_KEY) === "benes_session_fresh") return new Response("{}", { status: 200 });
      return new Response("unauthorized", { status: 401 });
    }) as typeof fetch;
    installMock(dom, handler);
    assert.equal((await dom.window.fetch("/api/config")).status, 200);
    assert.equal(prompts, 0);
    assert.deepEqual(keys, ["benes_session_stale", "benes_session_fresh"]);
  });
});

test("transient bootstrap 5xx/timeout does not prompt; 4xx may fall through", async () => {
  await withAuthDom(async (dom) => {
    let prompts = 0;
    resetApiAuthFetchForTests(async () => {
      prompts += 1;
      return "admin";
    });
    let bootstrapStatus = 503;
    const handler = (async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = pathnameOf(input);
      if (path === "/benes-session") return new Response("down", { status: bootstrapStatus });
      if (headersOf(input, init).get(API_KEY) === "admin") return new Response("{}", { status: 200 });
      return new Response("unauthorized", { status: 401 });
    }) as typeof fetch;
    installMock(dom, handler);
    assert.equal((await dom.window.fetch("/api/config")).status, 401);
    assert.equal(prompts, 0);

    bootstrapStatus = 401;
    assert.equal((await dom.window.fetch("/api/providers")).status, 200);
    assert.equal(prompts, 1);
  });
});

test("one caller abort does not cancel shared resolution for others", async () => {
  await withAuthDom(async (dom) => {
    let prompts = 0;
    let releasePrompt: (token: string) => void = () => {};
    resetApiAuthFetchForTests(async () => {
      prompts += 1;
      return await new Promise<string>((resolve) => {
        releasePrompt = resolve;
      });
    });
    const handler = (async (input: RequestInfo | URL, init?: RequestInit) => {
      if (pathnameOf(input) === "/benes-session") return new Response("no", { status: 401 });
      if (headersOf(input, init).get(API_KEY) === "shared") return new Response("{}", { status: 200 });
      return new Response("unauthorized", { status: 401 });
    }) as typeof fetch;
    installMock(dom, handler);
    const abort = new AbortController();
    const first = dom.window.fetch("/api/config", { signal: abort.signal });
    const second = dom.window.fetch("/api/providers");
    for (let i = 0; i < 20 && prompts === 0; i += 1) await Promise.resolve();
    abort.abort();
    assert.equal((await first).status, 401);
    releasePrompt("shared");
    assert.equal((await second).status, 200);
    assert.equal(prompts, 1);
  });
});

test("bootstrap uses raw fetch and a hung bootstrap cannot pin callers forever", async () => {
  await withAuthDom(async (dom) => {
    let prompts = 0;
    resetApiAuthFetchForTests(async () => {
      prompts += 1;
      return null;
    });
    setRebootstrapTimeoutForTests(30);
    setResolutionWatchdogForTests(40);
    const handler = (async (input: RequestInfo | URL, init?: RequestInit) => {
      if (pathnameOf(input) === "/benes-session") {
        return await new Promise<Response>((_, reject) => {
          init?.signal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")), { once: true });
        });
      }
      return new Response("unauthorized", { status: 401 });
    }) as typeof fetch;
    installMock(dom, handler);
    const started = Date.now();
    assert.equal((await dom.window.fetch("/api/config")).status, 401);
    assert.equal(Date.now() - started < 1000, true);
    assert.equal(prompts, 0);
  });
});
