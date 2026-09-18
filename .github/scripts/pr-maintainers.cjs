"use strict";

const SECTION_TITLE = "## Current maintainers";
const MENTION_LINK =
  /\[@([A-Za-z0-9-]+)\]\(https:\/\/github\.com\/([A-Za-z0-9-]+)\/?\)/g;

function isCurrentMaintainersHeading(line) {
  return line.trimEnd() === SECTION_TITLE;
}

function isH2(line) {
  return line.startsWith("## ") && !line.startsWith("### ");
}

function readMaintainerGitHubLogins(markdown) {
  if (typeof markdown !== "string" || markdown.length === 0) return [];

  const logins = [];
  const seen = new Set();
  let inSection = false;

  for (const raw of markdown.split(/\r?\n/)) {
    if (!inSection) {
      if (isCurrentMaintainersHeading(raw)) inSection = true;
      continue;
    }
    if (isH2(raw)) break;

    MENTION_LINK.lastIndex = 0;
    let match;
    while ((match = MENTION_LINK.exec(raw))) {
      const mention = match[1];
      const urlLogin = match[2];
      if (mention.toLowerCase() !== urlLogin.toLowerCase()) continue;
      const key = mention.toLowerCase();
      if (seen.has(key)) continue;
      seen.add(key);
      logins.push(mention);
    }
  }
  return logins;
}

module.exports = {
  readMaintainerGitHubLogins,
};
