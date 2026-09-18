import { mkdir, mkdtemp, realpath } from "node:fs/promises";
import { homedir, tmpdir } from "node:os";
import path from "node:path";

function sameOrInside(parent: string, child: string): boolean {
  const rel = path.relative(path.resolve(parent), path.resolve(child));
  return rel === "" || (rel !== ".." && !rel.startsWith(`..${path.sep}`) && !path.isAbsolute(rel));
}

function protectedProfileRoots(): string[] {
  const values = new Set<string>();
  for (const candidate of [process.env.HOME, process.env.USERPROFILE, homedir()]) {
    if (!candidate) continue;
    const profile = path.resolve(candidate);
    values.add(profile);
    const container = path.dirname(profile);
    if (container !== path.parse(profile).root) values.add(container);
  }
  return [...values];
}

function isProtected(base: string): boolean {
  return protectedProfileRoots().some((profile) => sameOrInside(profile, base));
}

export async function createTestTempRoot(prefix: string): Promise<string> {
  const candidates = [
    process.env.BENES_TEST_TMPDIR,
    tmpdir(),
    process.platform === "win32"
      ? path.join(process.env.SystemRoot ?? path.parse(process.cwd()).root, "Temp")
      : undefined,
  ].filter((candidate): candidate is string => Boolean(candidate));

  const errors: string[] = [];
  for (const candidate of [...new Set(candidates.map((value) => path.resolve(value)))]) {
    if (isProtected(candidate)) {
      errors.push(`${candidate}: inside protected profile/container`);
      continue;
    }
    try {
      await mkdir(candidate, { recursive: true });
      return await realpath(await mkdtemp(path.join(candidate, prefix)));
    } catch (error) {
      errors.push(`${candidate}: ${error instanceof Error ? error.message : String(error)}`);
    }
  }

  throw new Error(`no writable test temp root outside protected user paths: ${errors.join("; ")}`);
}
