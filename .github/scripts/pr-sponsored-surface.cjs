"use strict";

const SPONSOR_LABEL = "maintainer-sponsored";

const SECURITY_SURFACES = Object.freeze([
  {
    id: "credentials",
    prefixes: [
      "internal/oauth/",
      "internal/codexauth/",
      "internal/credentials/",
      "internal/credentialpool/",
      "internal/server/oauth_",
      "internal/server/codex_auth_",
      "internal/server/auth_",
    ],
    files: ["internal/server/credentials_api.go"],
  },
  {
    id: "github-automation",
    prefixes: [".github/workflows/", ".github/scripts/"],
    files: [],
  },
  {
    id: "release",
    prefixes: ["scripts/lib/release/"],
    files: [
      "scripts/release.ts",
      "scripts/node-runtime.ts",
      "scripts/win-exec.ts",
      "scripts/prepare-package.ts",
    ],
  },
  {
    id: "dependencies",
    prefixes: [],
    files: ["package.json", "package-lock.json"],
  },
]);

function matchesSurface(filePath, surface) {
  if (surface.files.includes(filePath)) return true;
  return surface.prefixes.some((prefix) => filePath.startsWith(prefix));
}

function pathRequiresMaintainerReview(filePath) {
  return SECURITY_SURFACES.some((surface) => matchesSurface(filePath, surface));
}

function classifySecuritySurfaces(changedFiles) {
  const hits = [];
  for (const surface of SECURITY_SURFACES) {
    const paths = (changedFiles || []).filter((filePath) => matchesSurface(filePath, surface));
    if (paths.length > 0) hits.push({ id: surface.id, paths });
  }
  return hits;
}

function labelName(label) {
  return typeof label === "string" ? label : label?.name;
}

function hasExplicitSponsorship(labels) {
  return (labels || []).some((label) => labelName(label) === SPONSOR_LABEL);
}

function missingMaintainerSponsorship({
  authorHasPushPermission = false,
  changedFiles = [],
  labels = [],
}) {
  const surfaces = classifySecuritySurfaces(changedFiles);
  if (surfaces.length === 0) return [];
  if (authorHasPushPermission) return [];
  if (hasExplicitSponsorship(labels)) return [];
  return [
    {
      code: "unsponsored_surface",
      paths: surfaces.flatMap((surface) => surface.paths),
      surfaces: surfaces.map((surface) => surface.id),
    },
  ];
}

module.exports = {
  SECURITY_SURFACES,
  SPONSOR_LABEL,
  classifySecuritySurfaces,
  pathRequiresMaintainerReview,
  missingMaintainerSponsorship,
};
