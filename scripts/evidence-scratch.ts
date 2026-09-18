import { randomUUID } from "node:crypto";
import { homedir } from "node:os";
import path from "node:path";
import { lstat, mkdir, open, readFile, readdir, realpath, rm, writeFile } from "node:fs/promises";

const MARKER_NAME = ".benes-evidence-owner.json";
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

export type EvidenceScratch = {
  runId: string;
  root: string;
  path: string;
  markerPath: string;
};

export type EvidenceScratchMarker = {
  version: 1;
  runId: string;
  root: string;
  target: string;
};

type ValidationOptions = {
  additionalProtectedPaths?: readonly string[];
};

type CreateOptions = ValidationOptions & {
  runId?: string;
};

function samePath(a: string, b: string): boolean {
  const left = path.resolve(a);
  const right = path.resolve(b);
  return process.platform === "win32"
    ? left.localeCompare(right, undefined, { sensitivity: "accent" }) === 0
    : left === right;
}

function isStrictDescendant(parent: string, child: string): boolean {
  const rel = path.relative(path.resolve(parent), path.resolve(child));
  return rel !== "" && rel !== ".." && !rel.startsWith(`..${path.sep}`) && !path.isAbsolute(rel);
}

export function protectedUserProfilePaths(profile: string): string[] {
  const resolved = path.resolve(profile);
  const values = new Set<string>([resolved]);
  const container = path.dirname(resolved);
  if (!samePath(container, path.parse(resolved).root)) {
    values.add(container);
  }
  for (const child of ["Desktop", "Documents", "AppData"]) {
    values.add(path.join(resolved, child));
  }
  return [...values];
}

function defaultProtectedPaths(): string[] {
  const values = new Set<string>();
  const home = homedir();
  for (const candidate of [process.env.HOME, process.env.USERPROFILE, home]) {
    if (!candidate) continue;
    for (const protectedPath of protectedUserProfilePaths(candidate)) {
      values.add(protectedPath);
    }
  }
  return [...values];
}

function isInsideOrEqual(parent: string, child: string): boolean {
  return samePath(parent, child) || isStrictDescendant(parent, child);
}

function assertAbsolute(input: string, label: string): string {
  if (!path.isAbsolute(input)) {
    throw new Error(`${label} must be absolute`);
  }
  return path.resolve(input);
}

function assertNotDriveRoot(target: string): void {
  if (samePath(path.parse(target).root, target)) {
    throw new Error("cleanup target must not be a drive/filesystem root");
  }
}

function protectedPaths(options: ValidationOptions): string[] {
  return [...defaultProtectedPaths(), ...(options.additionalProtectedPaths ?? [])];
}

function assertNotProtected(target: string, paths: readonly string[]): void {
  for (const protectedPath of paths) {
    if (!protectedPath) continue;
    const resolved = path.resolve(protectedPath);
    if (isInsideOrEqual(resolved, target)) {
      throw new Error(`cleanup target is inside protected path: ${resolved}`);
    }
  }
}

async function assertNoSymlinkBetween(root: string, target: string): Promise<void> {
  const parts = path.relative(root, target).split(path.sep).filter(Boolean);
  let current = root;
  for (const part of ["", ...parts]) {
    if (part) current = path.join(current, part);
    const info = await lstat(current);
    if (info.isSymbolicLink()) {
      throw new Error(`symlink/reparse point is not allowed in evidence scratch path: ${current}`);
    }
  }
}

async function nearestExistingAncestor(pathValue: string): Promise<string> {
  let current = path.resolve(pathValue);
  for (;;) {
    try {
      await lstat(current);
      return current;
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
    }
    const parent = path.dirname(current);
    if (samePath(parent, current)) throw new Error(`no existing ancestor for ${pathValue}`);
    current = parent;
  }
}

async function assertCreationPathHasNoSymlink(pathValue: string, label: string): Promise<void> {
  const ancestor = await nearestExistingAncestor(pathValue);
  const actual = await realpath(ancestor);
  if (!samePath(ancestor, actual)) {
    throw new Error(`${label} resolves through a symlink/reparse point: ${ancestor}`);
  }
}

async function assertCanonicalExisting(pathValue: string, label: string): Promise<string> {
  const resolved = path.resolve(pathValue);
  const actual = await realpath(resolved);
  if (!samePath(resolved, actual)) {
    throw new Error(`${label} resolves through a symlink/reparse point`);
  }
  return resolved;
}

async function assertNoSymlinkDescendants(directory: string): Promise<void> {
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const child = path.join(directory, entry.name);
    const info = await lstat(child);
    if (info.isSymbolicLink()) {
      throw new Error(`symlink/reparse point is not allowed inside evidence scratch: ${child}`);
    }
    if (info.isDirectory()) {
      await assertNoSymlinkDescendants(child);
    }
  }
}

function parseMarker(raw: string): EvidenceScratchMarker {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    throw new Error("ownership marker is not valid JSON");
  }
  if (!parsed || typeof parsed !== "object") {
    throw new Error("ownership marker has invalid shape");
  }
  const marker = parsed as Partial<EvidenceScratchMarker>;
  if (marker.version !== 1 || typeof marker.runId !== "string" || typeof marker.root !== "string" || typeof marker.target !== "string") {
    throw new Error("ownership marker has invalid shape");
  }
  return marker as EvidenceScratchMarker;
}

export async function createEvidenceScratch(root: string, options: CreateOptions = {}): Promise<EvidenceScratch> {
  const canonicalRoot = assertAbsolute(root, "scratch root");
  assertNotDriveRoot(canonicalRoot);
  assertNotProtected(canonicalRoot, protectedPaths(options));
  await assertCreationPathHasNoSymlink(canonicalRoot, "scratch root");
  await mkdir(canonicalRoot, { recursive: true });
  await assertCanonicalExisting(canonicalRoot, "scratch root");

  const runId = options.runId ?? randomUUID();
  if (!UUID_RE.test(runId)) {
    throw new Error("run id must be a UUID v4");
  }

  const target = path.join(canonicalRoot, runId);
  await mkdir(target, { recursive: false });
  const canonicalTarget = await assertCanonicalExisting(target, "scratch target");
  await assertNoSymlinkBetween(canonicalRoot, canonicalTarget);

  const markerPath = path.join(canonicalTarget, MARKER_NAME);
  const marker: EvidenceScratchMarker = {
    version: 1,
    runId,
    root: canonicalRoot,
    target: canonicalTarget,
  };
  await writeFile(markerPath, `${JSON.stringify(marker)}\n`, { encoding: "utf8", flag: "wx" });

  return { runId, root: canonicalRoot, path: canonicalTarget, markerPath };
}

export async function validateEvidenceScratchForCleanup(
  root: string,
  target: string,
  options: ValidationOptions = {},
): Promise<EvidenceScratchMarker> {
  const canonicalRoot = assertAbsolute(root, "scratch root");
  const canonicalTarget = assertAbsolute(target, "cleanup target");
  assertNotDriveRoot(canonicalRoot);
  assertNotDriveRoot(canonicalTarget);

  if (samePath(canonicalRoot, canonicalTarget)) {
    throw new Error("cleanup target must not be the scratch root itself");
  }
  if (!isStrictDescendant(canonicalRoot, canonicalTarget)) {
    throw new Error("cleanup target is outside scratch root");
  }
  if (!samePath(path.dirname(canonicalTarget), canonicalRoot)) {
    throw new Error("cleanup target must be a direct child of scratch root");
  }

  assertNotProtected(canonicalTarget, protectedPaths(options));

  await assertCanonicalExisting(canonicalRoot, "scratch root");
  await assertCanonicalExisting(canonicalTarget, "cleanup target");
  await assertNoSymlinkBetween(canonicalRoot, canonicalTarget);

  const runId = path.basename(canonicalTarget);
  if (!UUID_RE.test(runId)) {
    throw new Error("cleanup target basename is not a valid run id");
  }

  const markerPath = path.join(canonicalTarget, MARKER_NAME);
  let markerRaw: string;
  try {
    const markerFile = await open(markerPath, "r");
    try {
      const markerInfo = await markerFile.stat();
      if (!markerInfo.isFile()) {
        throw new Error("ownership marker must be a regular file");
      }
      markerRaw = await markerFile.readFile("utf8");
    } finally {
      await markerFile.close();
    }
  } catch (error) {
    if (error instanceof Error && /ownership marker/.test(error.message)) throw error;
    throw new Error("ownership marker is missing or unreadable");
  }

  const marker = parseMarker(markerRaw);
  if (marker.runId !== runId) {
    throw new Error("ownership marker run id does not match target");
  }
  if (!samePath(marker.root, canonicalRoot)) {
    throw new Error("ownership marker root does not match scratch root");
  }
  if (!samePath(marker.target, canonicalTarget)) {
    throw new Error("ownership marker target does not match cleanup target");
  }

  return marker;
}

export async function removeEvidenceScratch(
  root: string,
  target: string,
  options: ValidationOptions = {},
): Promise<void> {
  await validateEvidenceScratchForCleanup(root, target, options);
  await assertNoSymlinkDescendants(path.resolve(target));
  await rm(path.resolve(target), { recursive: true, force: false });
}
