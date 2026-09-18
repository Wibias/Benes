"use strict";

const fs = require("node:fs");
const path = require("node:path");

function normalizePackEntry(value, packageName) {
  if (Array.isArray(value)) {
    return value.length === 1 && value[0] && typeof value[0] === "object" ? value[0] : null;
  }
  if (!value || typeof value !== "object") return null;
  if (packageName && value[packageName] && typeof value[packageName] === "object") {
    return value[packageName];
  }
  const entries = Object.values(value).filter(
    (entry) => entry && typeof entry === "object" && !Array.isArray(entry),
  );
  return entries.length === 1 ? entries[0] : null;
}

function readPackEntry(file = "pack.json") {
  const parsed = JSON.parse(fs.readFileSync(file, "utf8"));
  const packageJson = JSON.parse(
    fs.readFileSync(path.join(process.cwd(), "package.json"), "utf8"),
  );
  const entry = normalizePackEntry(parsed, packageJson.name);
  if (!entry || typeof entry.filename !== "string" || !Array.isArray(entry.files)) {
    throw new Error("unsupported npm pack --json shape");
  }
  return entry;
}

function verifySurface(entry) {
  if (!entry.files.some((file) => file.path === "gui/dist/index.html")) {
    throw new Error("missing gui/dist/index.html in npm pack");
  }
  if (
    entry.files.some(
      (file) => file.path === "src/index.ts" || String(file.path).startsWith("src/"),
    )
  ) {
    throw new Error("tarball still contains src/");
  }
}

if (require.main === module) {
  try {
    const command = process.argv[2];
    const file = process.argv[3] || "pack.json";
    const entry = readPackEntry(file);
    if (command === "verify") {
      verifySurface(entry);
    } else if (command === "filename") {
      process.stdout.write(entry.filename);
    } else {
      throw new Error("usage: npm-pack-json.cjs <verify|filename> [pack.json]");
    }
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exit(1);
  }
}

module.exports = { normalizePackEntry, readPackEntry, verifySurface };
