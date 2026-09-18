import { spawn, type SpawnOptions } from "node:child_process";
import { access, readFile, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

export async function readTextFile(path: string): Promise<string> {
  return await readFile(path, "utf8");
}

export async function writeTextFile(path: string, body: string): Promise<void> {
  await writeFile(path, body, "utf8");
}

export async function fileExists(path: string): Promise<boolean> {
  try {
    await access(path);
    return true;
  } catch {
    return false;
  }
}

export async function readStdin(): Promise<string> {
  const chunks: Buffer[] = [];
  for await (const chunk of process.stdin) {
    chunks.push(typeof chunk === "string" ? Buffer.from(chunk) : chunk);
  }
  return Buffer.concat(chunks).toString("utf8");
}

export function isMainModule(metaUrl: string): boolean {
  const entry = process.argv[1];
  if (!entry) return false;
  return metaUrl === pathToFileURL(resolve(entry)).href;
}

export async function sleep(ms: number): Promise<void> {
  await new Promise(resolve => setTimeout(resolve, ms));
}

type NpmInvocationOptions = {
  env?: NodeJS.ProcessEnv;
  execPath?: string;
  platform?: NodeJS.Platform;
};

export function npmInvocation(
  args: readonly string[],
  options: NpmInvocationOptions = {},
): { command: string; args: string[] } {
  const env = options.env ?? process.env;
  if ((options.platform ?? process.platform) === "win32") {
    return {
      command: env.ComSpec?.trim() || env.COMSPEC?.trim() || "cmd.exe",
      args: ["/d", "/s", "/c", "npm", ...args],
    };
  }

  return { command: "npm", args: [...args] };
}

type RunOptions = {
  inherit?: boolean;
  windowsVerbatimArguments?: boolean;
  env?: NodeJS.ProcessEnv;
};

export async function runCommand(args: readonly string[], options: RunOptions = {}): Promise<{
  stdout: string;
  stderr: string;
  exitCode: number;
}> {
  const [cmd, ...rest] = args;
  if (!cmd) {
    return { stdout: "", stderr: "missing command", exitCode: 1 };
  }
  const spawnOpts: SpawnOptions = {
    windowsHide: true,
    windowsVerbatimArguments: options.windowsVerbatimArguments,
    stdio: options.inherit ? "inherit" : ["ignore", "pipe", "pipe"],
    env: options.env,
  };
  return await new Promise((resolvePromise) => {
    const proc = spawn(cmd, rest, spawnOpts);
    let stdout = "";
    let stderr = "";
    if (!options.inherit) {
      proc.stdout?.setEncoding("utf8");
      proc.stderr?.setEncoding("utf8");
      proc.stdout?.on("data", (chunk: string) => { stdout += chunk; });
      proc.stderr?.on("data", (chunk: string) => { stderr += chunk; });
    }
    proc.on("error", (err: NodeJS.ErrnoException) => {
      resolvePromise({ stdout, stderr: err.message, exitCode: 1 });
    });
    proc.on("close", (code) => {
      resolvePromise({ stdout, stderr, exitCode: code ?? 1 });
    });
  });
}
