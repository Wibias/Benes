"use strict";

const TEST_PATH = /(?:^|\/)(?:__tests__\/.+|[^/]+\.(?:test|spec)\.[^.]+|[^/]+_test\.go)$/;
const BLOB_SHA = /^[a-f0-9]{40}$/i;

function posixPath(filePath) {
  return String(filePath || "").replace(/\\/g, "/");
}

function isGuiTestPath(filePath) {
  const pathName = posixPath(filePath);
  if (!TEST_PATH.test(pathName)) return false;
  return pathName.startsWith("gui/src/") || pathName.startsWith("gui/scripts/");
}

function isSurvivingGuiTestFile(file) {
  if (!file || file.status === "removed") return false;
  return isGuiTestPath(file.filename);
}

function decodeGitBlob(data) {
  if (!data || typeof data.content !== "string") return null;
  if (data.encoding !== "base64") return null;
  return Buffer.from(data.content.replace(/\s+/g, ""), "base64").toString("utf8");
}

async function loadGuiTestContentsAtHead({ github, owner, repo, files = [], warn = () => {} } = {}) {
  const contents = Object.create(null);
  for (const file of files) {
    if (!isSurvivingGuiTestFile(file)) continue;
    const pathName = posixPath(file.filename);
    const sha = String(file.sha || "");
    if (!BLOB_SHA.test(sha)) continue;
    try {
      const { data } = await github.rest.git.getBlob({
        owner,
        repo,
        file_sha: sha,
      });
      const text = decodeGitBlob(data);
      if (text != null) contents[pathName] = text;
    } catch (error) {
      warn(`Could not read GUI test ${pathName} at the PR head: ${error.message}`);
    }
  }
  return contents;
}

function readFileFromContents(contents = {}) {
  return (filePath) => {
    const pathName = posixPath(filePath);
    if (!Object.prototype.hasOwnProperty.call(contents, pathName)) {
      throw new Error(`gui-test-unavailable:${pathName}`);
    }
    return contents[pathName];
  };
}

module.exports = {
  isGuiTestPath,
  isSurvivingGuiTestFile,
  decodeGitBlob,
  loadGuiTestContentsAtHead,
  readFileFromContents,
};
