/**
 * Plan a Node spawn for Windows CLI shims used by the release driver.
 *
 * POSIX and native PE images keep their argv. `.cmd` / `.bat` files go through
 * ComSpec with `/d /s /c` and verbatim arguments so spaces and metacharacters
 * stay one argument. Local `node_modules/.bin` shims nest cmd.exe, so those
 * lines get a second caret pass. A bare name is resolved through PATH and
 * PATHEXT; an unresolved name is left for spawn to fail.
 *
 * `commandInvocation` is the surface used by the release driver.
 */
import { existsSync } from "node:fs";
import { win32 } from "node:path";

export type WindowsLookup = {
  env?: Record<string, string | undefined>;
  exists?: (path: string) => boolean;
};

export type SpawnPlan = {
  file: string;
  args: string[];
  options: { windowsVerbatimArguments?: boolean };
};

const CARET_CHARS = new Set([
  "(", ")", "[", "]", "%", "!", "^", '"', "`", "<", ">", "&", "|", ";", ",", " ", "*", "?",
]);

function envLookup(env: Record<string, string | undefined>, name: string): string | undefined {
  const want = name.toLowerCase();
  for (const [key, value] of Object.entries(env)) {
    if (key.toLowerCase() === want) return value;
  }
}

function caretEscape(text: string): string {
  let out = "";
  for (const ch of text) {
    if (CARET_CHARS.has(ch)) out += "^";
    out += ch;
  }
  return out;
}

function quoteForCrt(arg: string): string {
  let escaped = "";
  let slashes = 0;
  for (const ch of String(arg)) {
    if (ch === "\\") {
      slashes += 1;
      continue;
    }
    if (ch === '"') {
      escaped += "\\".repeat(slashes * 2 + 1) + '"';
      slashes = 0;
      continue;
    }
    escaped += "\\".repeat(slashes) + ch;
    slashes = 0;
  }
  escaped += "\\".repeat(slashes * 2);
  return `"${escaped}"`;
}

function npmBinCmd(file: string): boolean {
  const parts = file.replaceAll("/", "\\").toLowerCase().split("\\");
  const bin = parts.lastIndexOf(".bin");
  if (bin < 1 || parts[bin - 1] !== "node_modules") return false;
  const leaf = parts[bin + 1] ?? "";
  return leaf.endsWith(".cmd") && bin === parts.length - 2;
}

function alreadyLocated(command: string): boolean {
  return Boolean(win32.extname(command) || command.includes("\\") || command.includes("/") || win32.isAbsolute(command));
}

function searchPath(command: string, lookup: WindowsLookup): string {
  const env = lookup.env ?? process.env;
  const exists = lookup.exists ?? existsSync;
  const pathValue = envLookup(env, "PATH") ?? "";
  const pathext = envLookup(env, "PATHEXT");
  const extensions = (pathext === undefined ? ".COM;.EXE;.BAT;.CMD" : pathext).split(";").filter(Boolean);
  if (!pathValue || extensions.length === 0) return command;
  for (const dir of pathValue.split(win32.delimiter).filter(Boolean)) {
    for (const ext of extensions) {
      const lower = win32.join(dir, command + ext.toLowerCase());
      if (exists(lower)) return lower;
      if (ext !== ext.toLowerCase()) {
        const given = win32.join(dir, command + ext);
        if (exists(given)) return given;
      }
    }
  }
  return command;
}

function throughCmd(batch: string, args: readonly string[], env: Record<string, string | undefined>): SpawnPlan {
  const twice = npmBinCmd(batch);
  const pieces = [caretEscape(batch)];
  for (const arg of args) {
    let token = caretEscape(quoteForCrt(arg));
    if (twice) token = caretEscape(token);
    pieces.push(token);
  }
  return {
    file: envLookup(env, "ComSpec") ?? "cmd.exe",
    args: ["/d", "/s", "/c", `"${pieces.join(" ")}"`],
    options: { windowsVerbatimArguments: true },
  };
}

export function commandInvocation(
  command: string,
  args: readonly string[],
  platform: NodeJS.Platform = process.platform,
  lookup: WindowsLookup = {},
): SpawnPlan {
  if (platform !== "win32") {
    return { file: command, args: [...args], options: {} };
  }
  const target = alreadyLocated(command) ? command : searchPath(command, lookup);
  if (!/\.(cmd|bat)$/i.test(target)) {
    return { file: target, args: [...args], options: {} };
  }
  return throughCmd(target, args, lookup.env ?? process.env);
}
