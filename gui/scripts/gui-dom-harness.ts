type Listener = (event: { type: string; preventDefault(): void; cancelable?: boolean }) => void;

export class FakeElement {
  readonly children: FakeElement[] = [];
  parent: FakeElement | null = null;
  id = "";
  className = "";
  textContent = "";
  hidden = false;
  disabled = false;
  readOnly = false;
  required = false;
  spellcheck = true;
  autocapitalize = "";
  autocomplete = "";
  type = "";
  name = "";
  value = "";
  htmlFor = "";
  method = "";
  action = "";
  open = false;
  style: Record<string, string> = {};
  content = "";
  readonly dataset: Record<string, string> = {};
  private attrs = new Map<string, string>();
  private listeners = new Map<string, Set<Listener>>();

  tagName: string;

  constructor(tagName: string) {
    this.tagName = tagName;
  }

  setAttribute(name: string, value: string): void {
    this.attrs.set(name.toLowerCase(), value);
    if (name.toLowerCase() === "id") this.id = value;
    if (name.toLowerCase() === "name") this.name = value;
    if (name.toLowerCase() === "content") this.content = value;
    if (name.toLowerCase() === "open") this.open = true;
  }

  getAttribute(name: string): string | null {
    return this.attrs.get(name.toLowerCase()) ?? null;
  }

  append(...nodes: FakeElement[]): void {
    for (const node of nodes) {
      node.parent = this;
      this.children.push(node);
    }
  }

  appendChild(node: FakeElement): FakeElement {
    this.append(node);
    return node;
  }

  remove(): void {
    if (!this.parent) return;
    const index = this.parent.children.indexOf(this);
    if (index >= 0) this.parent.children.splice(index, 1);
    this.parent = null;
  }

  focus(): void {
    currentDocument.activeElement = this;
  }

  reportValidity(): boolean {
    return this.value.trim().length > 0 || !this.required;
  }

  showModal(): void {
    this.open = true;
  }

  close(): void {
    this.open = false;
  }

  addEventListener(type: string, listener: Listener, _options?: { once?: boolean }): void {
    const set = this.listeners.get(type) ?? new Set();
    set.add(listener);
    this.listeners.set(type, set);
  }

  removeEventListener(type: string, listener: Listener): void {
    this.listeners.get(type)?.delete(listener);
  }

  dispatchEvent(event: { type: string; preventDefault(): void; cancelable?: boolean }): boolean {
    for (const listener of [...(this.listeners.get(event.type) ?? [])]) listener(event);
    return true;
  }

  set innerHTML(html: string) {
    this.children.length = 0;
    parseMarkup(html, this);
  }

  querySelector(selector: string): FakeElement | null {
    return this.querySelectorAll(selector)[0] ?? null;
  }

  querySelectorAll(selector: string): FakeElement[] {
    const matches: FakeElement[] = [];
    walk(this, (node) => {
      if (node !== this && matchesSelector(node, selector)) matches.push(node);
    });
    return matches;
  }

  get elements() {
    const owner = this;
    return {
      namedItem(name: string): FakeElement | null {
        return owner.querySelector(`[name="${name}"]`);
      },
    };
  }
}

function applyAttrs(node: FakeElement, raw: string): void {
  const attrRe = /([a-zA-Z_:][\w:.-]*)(?:=("([^"]*)"|'([^']*)'|([^\s>]+)))?/g;
  let match: RegExpExecArray | null;
  while ((match = attrRe.exec(raw))) {
    const name = match[1]!;
    const value = match[3] ?? match[4] ?? match[5] ?? "";
    const lower = name.toLowerCase();
    node.setAttribute(lower, value);
    if (lower === "class") node.className = value;
    if (lower === "id") node.id = value;
    if (lower === "name") node.name = value;
    if (lower === "type") node.type = value;
    if (lower === "value") node.value = value;
    if (lower === "for") node.htmlFor = value;
    if (lower === "method") node.method = value;
    if (lower === "action") node.action = value;
    if (lower === "autocomplete") node.autocomplete = value;
    if (lower === "readonly") node.readOnly = true;
    if (lower === "required") node.required = true;
    if (lower === "hidden") node.hidden = true;
    if (lower === "spellcheck") node.spellcheck = value !== "false";
    if (lower === "autocapitalize") node.autocapitalize = value;
    if (lower === "style") {
      for (const part of value.split(";")) {
        const [prop, val] = part.split(":").map((piece) => piece.trim());
        if (prop && val) node.style[prop] = val;
      }
    }
    if (lower.startsWith("data-")) node.dataset[lower.slice(5)] = value;
  }
}

function parseMarkup(html: string, parent: FakeElement): void {
  const tokenRe = /<!--[\s\S]*?-->|<([a-zA-Z][\w-]*)([^>]*)\/?>|<\/([a-zA-Z][\w-]*)>|([^<]+)/g;
  const stack: FakeElement[] = [parent];
  let match: RegExpExecArray | null;
  while ((match = tokenRe.exec(html))) {
    if (match[0].startsWith("<!--")) continue;
    if (match[4]) continue;
    if (match[3]) {
      if (stack.length > 1) stack.pop();
      continue;
    }
    const tag = match[1]!;
    const node = new FakeElement(tag.toLowerCase());
    applyAttrs(node, match[2] ?? "");
    stack[stack.length - 1]!.append(node);
    const voidish = /^(input|br|hr|meta|img)$/.test(tag.toLowerCase()) || /\/>$/.test(match[0]);
    if (!voidish) stack.push(node);
  }
}

function walk(node: FakeElement, visit: (node: FakeElement) => void): void {
  visit(node);
  for (const child of node.children) walk(child, visit);
}

function matchesSelector(node: FakeElement, selector: string): boolean {
  const meta = selector.match(/^meta\[name="([^"]+)"\]$/);
  if (meta) return node.tagName === "meta" && node.getAttribute("name") === meta[1];
  const attr = selector.match(/^\[name="([^"]+)"\]$/);
  if (attr) return node.name === attr[1] || node.getAttribute("name") === attr[1];
  const id = selector.match(/^#(.+)$/);
  if (id) return node.id === id[1];
  const taggedId = selector.match(/^([a-z0-9]+)\[id="([^"]+)"\]$/i);
  if (taggedId) return node.tagName === taggedId[1] && node.id === taggedId[2];
  if (selector === "h3") return node.tagName === "h3";
  if (selector === "form") return node.tagName === "form";
  if (selector === "dialog") return node.tagName === "dialog";
  if (selector === '[role="alert"]') return node.getAttribute("role") === "alert";
  const data = selector.match(/^\[data-([a-z0-9-]+)\]$/i);
  if (data) return node.getAttribute(`data-${data[1]}`) !== null || Object.prototype.hasOwnProperty.call(node.dataset, data[1]!);
  const labelled = selector.match(/^label\[for="([^"]+)"\]$/);
  if (labelled) return node.tagName === "label" && node.htmlFor === labelled[1];
  return false;
}

class FakeStorage {
  private data = new Map<string, string>();
  get length(): number { return this.data.size; }
  getItem(key: string): string | null { return this.data.has(key) ? this.data.get(key)! : null; }
  setItem(key: string, value: string): void { this.data.set(key, String(value)); }
  removeItem(key: string): void { this.data.delete(key); }
  clear(): void { this.data.clear(); }
}

type InstalledDom = {
  document: FakeElement & {
    body: FakeElement;
    head: FakeElement;
    documentElement: FakeElement;
    activeElement: FakeElement | null;
    visibilityState: DocumentVisibilityState;
    createElement(tag: string): FakeElement;
  };
  window: {
    location: URL;
    fetch: typeof fetch;
    sessionStorage: FakeStorage;
    localStorage: FakeStorage;
  };
  sessionStorage: FakeStorage;
  restore: () => void;
};

let currentDocument: InstalledDom["document"];

const previous = new Map<string, PropertyDescriptor | undefined>();

function defineGlobal(name: string, value: unknown): void {
  previous.set(name, Object.getOwnPropertyDescriptor(globalThis, name));
  Object.defineProperty(globalThis, name, { configurable: true, value });
}

export function installGuiDom(origin = "http://127.0.0.1:23100/"): InstalledDom {
  const documentElement = new FakeElement("html");
  const head = new FakeElement("head");
  const body = new FakeElement("body");
  documentElement.append(head, body);

  const document = Object.assign(new FakeElement("#document"), {
    body,
    head,
    documentElement,
    activeElement: null as FakeElement | null,
    visibilityState: "visible" as DocumentVisibilityState,
    createElement(tag: string) {
      return new FakeElement(tag);
    },
  });
  document.append(documentElement);
  currentDocument = document;

  const sessionStorage = new FakeStorage();
  const localStorage = new FakeStorage();
  const location = new URL(origin);
  const win = {
    location,
    fetch: globalThis.fetch.bind(globalThis),
    sessionStorage,
    localStorage,
  };

  defineGlobal("document", document);
  defineGlobal("window", win);
  defineGlobal("sessionStorage", sessionStorage);
  defineGlobal("localStorage", localStorage);
  defineGlobal("HTMLElement", FakeElement);
  defineGlobal("HTMLDialogElement", FakeElement);

  return {
    document,
    window: win,
    sessionStorage,
    restore() {
      for (const [name, descriptor] of previous) {
        if (descriptor) Object.defineProperty(globalThis, name, descriptor);
        else delete (globalThis as Record<string, unknown>)[name];
      }
      previous.clear();
    },
  };
}

export function setDocumentHidden(hidden: boolean): void {
  currentDocument.visibilityState = hidden ? "hidden" : "visible";
  currentDocument.dispatchEvent({ type: "visibilitychange", preventDefault() {} });
}

export function addMeta(name: string, content: string): void {
  const meta = currentDocument.createElement("meta");
  meta.setAttribute("name", name);
  meta.setAttribute("content", content);
  meta.content = content;
  currentDocument.head.append(meta);
}
