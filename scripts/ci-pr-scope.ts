import { isMainModule } from "./node-runtime.ts";

export type PrCiBuckets = {
  docs: boolean;
  gui: boolean;
  go: boolean;
  automation: boolean;
  privacy: boolean;
  keyring: boolean;
  packaging: boolean;
  linux: boolean;
  darwin: boolean;
  windows: boolean;
  full: boolean;
};

const IGNORE_EXACT = new Set([
  "AGENTS.md",
  "MAINTAINERS.md",
  "CONTRIBUTING.md",
  "SECURITY.md",
  "CODEOWNERS",
  ".gitignore",
]);

const FULL_SELF = [
  "scripts/local-pr.ps1",
  "scripts/ci-local.ps1",
  "scripts/ci-local-linux.sh",
  "scripts/ci-local-macos.sh",
  "scripts/ci-pr-scope.ts",
  "scripts/ci-scope.sh",
  ".github/workflows/ci.yml",
  ".github/workflows/go-core.yml",
];

const SHARED_NATIVE_GO_ROOTS = [
  "cmd/benes",
  "internal/bootstrap",
  "internal/config",
  "internal/credentials",
  "internal/managedfs",
  "internal/nativemain",
  "internal/platform",
  "internal/servicectl",
  "internal/storage",
  "internal/store",
];

function under(file: string, dir: string): boolean {
  return file === dir || file.startsWith(`${dir}/`);
}

function isIgnored(file: string): boolean {
  if (IGNORE_EXACT.has(file)) return true;
  if (under(file, "notes")) return true;
  if (under(file, ".agents")) return true;
  if (under(file, ".cursor")) return true;
  return false;
}

function isKnown(file: string): boolean {
  if (isIgnored(file)) return true;
  if (under(file, "docs")) return true;
  if (under(file, "gui")) return true;
  if (under(file, "cmd")) return true;
  if (under(file, "internal")) return true;
  if (under(file, "bin")) return true;
  if (under(file, "scripts")) return true;
  if (under(file, ".github")) return true;
  if (under(file, "assets")) return true;
  return [
    "go.mod",
    "go.sum",
    "package.json",
    "package-lock.json",
    ".npmignore",
    ".gitattributes",
    "README.md",
    "LICENSE",
  ].includes(file);
}

function emptyBuckets(): PrCiBuckets {
  return {
    docs: false,
    gui: false,
    go: false,
    automation: false,
    privacy: false,
    keyring: false,
    packaging: false,
    linux: false,
    darwin: false,
    windows: false,
    full: false,
  };
}

function allBuckets(): PrCiBuckets {
  return {
    docs: true,
    gui: true,
    go: true,
    automation: true,
    privacy: true,
    keyring: true,
    packaging: true,
    linux: true,
    darwin: true,
    windows: true,
    full: true,
  };
}

function sharedNativeGoPath(file: string): boolean {
  if (file === "go.mod" || file === "go.sum") return true;
  return SHARED_NATIVE_GO_ROOTS.some((root) => under(file, root));
}

function needsWindowsGo(file: string): boolean {
  if (sharedNativeGoPath(file)) return true;
  if (under(file, "internal/winsw") || under(file, "internal/wintray")) return true;
  return /_windows\.go$/i.test(file);
}

function needsDarwinGo(file: string): boolean {
  if (sharedNativeGoPath(file)) return true;
  return /_(?:darwin|unix)\.go$/i.test(file);
}

export function classifyPrCiPaths(files: string[], forceFull = false): PrCiBuckets {
  if (forceFull) return allBuckets();

  const paths = [...new Set(files.map((file) => file.replaceAll("\\", "/").trim()).filter(Boolean))];
  if (paths.length === 0) return emptyBuckets();

  if (paths.some((file) => !isKnown(file))) return allBuckets();

  const relevant = paths.filter((file) => !isIgnored(file));
  if (relevant.length === 0) return emptyBuckets();

  const buckets = emptyBuckets();

  for (const file of relevant) {
    if (under(file, "docs")) buckets.docs = true;
    if (under(file, "gui")) buckets.gui = true;
    if (under(file, "cmd") || under(file, "internal") || file === "go.mod" || file === "go.sum") {
      buckets.go = true;
      if (needsWindowsGo(file)) buckets.windows = true;
      if (needsDarwinGo(file)) buckets.darwin = true;
    }
    if (under(file, ".github")) buckets.automation = true;
    if (file.endsWith(".test.ts") && under(file, "scripts")) buckets.automation = true;
    if (under(file, ".github/scripts") && file.endsWith(".test.cjs")) buckets.automation = true;
    if (file === "scripts/keyring-smoke.ts") buckets.keyring = true;
    if (
      file === "package.json" ||
      file === "package-lock.json" ||
      file === ".npmignore" ||
      file === ".gitattributes" ||
      file === "README.md" ||
      file === "LICENSE" ||
      under(file, "bin") ||
      under(file, "assets") ||
      file === "scripts/prepare-package.ts" ||
      file === ".github/scripts/npm-pack-json.cjs"
    ) {
      buckets.packaging = true;
    }
    if (under(file, "scripts") && file !== "scripts/keyring-smoke.ts") {
      buckets.automation = true;
      buckets.privacy = true;
    }
  }

  if (buckets.gui || buckets.go || buckets.packaging || buckets.automation) {
    buckets.privacy = true;
  }
  if (buckets.go) {
    buckets.linux = true;
  }
  if (buckets.packaging) {
    buckets.linux = true;
  }
  if (buckets.keyring) {
    buckets.linux = true;
  }

  if (paths.some((file) => FULL_SELF.includes(file))) {
    buckets.go = true;
    buckets.linux = true;
    buckets.darwin = true;
    buckets.windows = true;
    buckets.automation = true;
    buckets.privacy = true;
  }

  return buckets;
}

export function scopeEnvValue(buckets: PrCiBuckets): string {
  if (buckets.full) return "all";
  const names = (
    ["docs", "gui", "go", "automation", "privacy", "keyring", "packaging"] as const
  ).filter((name) => buckets[name]);
  return names.join(",");
}

export function needsCiLocal(buckets: PrCiBuckets): boolean {
  if (buckets.full) return true;
  return buckets.go || buckets.packaging || buckets.keyring || buckets.linux || buckets.darwin || buckets.windows;
}

if (isMainModule(import.meta.url)) {
  const forceFull = process.argv.includes("--full") || process.env.BENES_CI_PR_FULL === "1";
  const fromEnv = process.env.CI_PR_FILES;
  const files = fromEnv
    ? fromEnv.split(/\r?\n/)
    : [];
  const buckets = classifyPrCiPaths(files, forceFull);
  process.stdout.write(
    `${JSON.stringify({
      ...buckets,
      scope: scopeEnvValue(buckets),
      ciLocal: needsCiLocal(buckets),
    }, null, 2)}\n`,
  );
}
