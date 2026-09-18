/**
 * Scan tracked text for material that must not land in the public tree:
 * home paths, emails, bearer tokens, and token-shaped literals.
 * Findings never include the matched secret — only file, line, and kind.
 */
import { spawnSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { isMainModule } from "./node-runtime.ts";

export type Finding = {
  file: string;
  line: number;
  kind: string;
};

const SCANNABLE = /\.(?:cjs|css|html|js|json|jsonc|md|mjs|ps1|sh|toml|ts|tsx|txt|yml|yaml)$/;
const SKIP_PREFIX = ["gui/dist/", "node_modules/", "tests/.tmp-"];
const SKIP_SUFFIX = ["package-lock.json"];

const MAC_HOME_PATH = /\/Users\/([A-Za-z0-9_-]+)\//g;
const EMAIL_ADDR = /[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}/gi;
const BEARER_TOKEN = /Bearer\s+([A-Za-z0-9._-]{24,})/g;
const OPENAI_STYLE = /\bsk-[A-Za-z0-9_-]{20,}\b/g;
const GITHUB_PAT = /\bghp_[A-Za-z0-9_]{20,}\b/g;
const JWT_PREFIX = /\beyJ[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}/g;

export function shouldScan(file: string): boolean {
  if (!SCANNABLE.test(file)) return false;
  if (SKIP_PREFIX.some((prefix) => file.startsWith(prefix))) return false;
  if (SKIP_SUFFIX.some((suffix) => file.endsWith(suffix))) return false;
  return true;
}

function inTests(file: string): boolean {
  return file.startsWith("tests/");
}

function emailAllowed(email: string): boolean {
  const domain = email.split("@")[1]?.toLowerCase() ?? "";
  if (domain === "example.test" || domain === "example.com" || domain === "test.com") return true;
  return domain.endsWith(".test");
}

function homeAllowed(file: string, user: string): boolean {
  if (inTests(file) && (user === "example" || user === "test" || user === "x")) return true;
  return file.startsWith("docs/") && user === "example";
}

function bearerAllowed(file: string, token: string): boolean {
  return inTests(file) && /^(?:access|stack|usage-debug)-token(?:-value)?-[A-Za-z0-9-]+$/.test(token);
}

function openaiAllowed(file: string, token: string): boolean {
  return inTests(file) && /^sk-(?:rawsentinel|test-)\d+[a-z]*$/.test(token);
}

function emailFixtureAllowed(file: string, email: string): boolean {
  if (emailAllowed(email)) return true;
  if (!inTests(file)) return false;
  return email === ["pw", "chatgpt.com"].join("@") || email === ["a", "b.com"].join("@");
}

function collect(pattern: RegExp, line: string): RegExpExecArray[] {
  pattern.lastIndex = 0;
  return [...line.matchAll(pattern)];
}

export function scanText(file: string, text: string): Finding[] {
  const findings: Finding[] = [];
  const lines = text.split(/\r?\n/);
  for (let i = 0; i < lines.length; i += 1) {
    const line = lines[i] ?? "";
    const n = i + 1;
    for (const match of collect(MAC_HOME_PATH, line)) {
      const user = match[1] ?? "";
      if (homeAllowed(file, user)) continue;
      findings.push({ file, line: n, kind: "home-path" });
    }
    for (const match of collect(EMAIL_ADDR, line)) {
      if (emailFixtureAllowed(file, match[0])) continue;
      findings.push({ file, line: n, kind: "email" });
    }
    for (const match of collect(BEARER_TOKEN, line)) {
      if (bearerAllowed(file, match[1] ?? "")) continue;
      findings.push({ file, line: n, kind: "bearer-token" });
    }
    for (const match of collect(OPENAI_STYLE, line)) {
      if (openaiAllowed(file, match[0])) continue;
      findings.push({ file, line: n, kind: "token-looking" });
    }
    for (const match of collect(GITHUB_PAT, line)) {
      findings.push({ file, line: n, kind: "token-looking" });
    }
    for (const match of collect(JWT_PREFIX, line)) {
      findings.push({ file, line: n, kind: "token-looking" });
    }
  }
  return findings;
}

export function formatFinding(finding: Finding): string {
  return `${finding.file}:${finding.line} ${finding.kind}: [redacted]`;
}

function trackedFiles(cwd: string): string[] {
  const result = spawnSync("git", ["ls-files"], { encoding: "utf8", cwd });
  if (result.status !== 0) {
    throw new Error(`git ls-files failed: ${(result.stderr || "").trim() || result.status}`);
  }
  return (result.stdout || "").split(/\r?\n/).filter(Boolean);
}

export function scanFile(logicalPath: string, physicalPath = logicalPath): Finding[] {
  return scanText(logicalPath, readFileSync(physicalPath, "utf-8"));
}

export function scanTrackedFiles(cwd = process.cwd()): Finding[] {
  return trackedFiles(cwd)
    .filter((logicalPath) => existsSync(path.join(cwd, logicalPath)))
    .filter(shouldScan)
    .flatMap((logicalPath) => scanFile(logicalPath, path.join(cwd, logicalPath)));
}

export function reportFindings(findings: Finding[], write = console.error): number {
  if (findings.length === 0) return 0;
  write("Privacy scan failed:");
  for (const finding of findings) write(formatFinding(finding));
  return 1;
}

export function main(): number {
  const findings = scanTrackedFiles();
  const status = reportFindings(findings);
  if (status === 0) console.log("Privacy scan passed");
  return status;
}

if (isMainModule(import.meta.url)) {
  process.exit(main());
}
