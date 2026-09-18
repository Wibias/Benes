"use strict";

const { describe, it, after } = require("node:test");
const assert = require("node:assert/strict");
const { execFileSync } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

function git(cwd, args) {
  return execFileSync(
    "git",
    [
      "-c",
      "user.name=Benes CI",
      "-c",
      "user.email=ci@example.test",
      "-c",
      "core.autocrlf=false",
      ...args,
    ],
    {
      cwd,
      encoding: "utf8",
      env: {
        ...process.env,
        GIT_AUTHOR_NAME: "Benes CI",
        GIT_AUTHOR_EMAIL: "ci@example.test",
        GIT_COMMITTER_NAME: "Benes CI",
        GIT_COMMITTER_EMAIL: "ci@example.test",
      },
    },
  ).trim();
}

describe("WSL linux-native CI bootstrap", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "benes-ci-linux-bootstrap-"));
  after(() => {
    fs.rmSync(root, { recursive: true, force: true });
  });

  it("materializes a commit from a git directory without FETCH_HEAD", () => {
    const source = path.join(root, "source");
    fs.mkdirSync(source);
    git(source, ["init", "-q"]);
    fs.writeFileSync(path.join(source, "README"), "ci\n");
    git(source, ["add", "README"]);
    git(source, ["commit", "-qm", "seed"]);
    const sha = git(source, ["rev-parse", "HEAD"]);
    fs.writeFileSync(path.join(source, ".git", "shallow"), `${sha}\n`);

    const dest = path.join(root, "dest");
    git(root, ["init", "-q", dest]);
    git(dest, [
      "fetch",
      "--no-tags",
      "--update-shallow",
      path.join(source, ".git"),
      `+${sha}:refs/heads/benes-ci`,
    ]);
    git(dest, ["checkout", "--detach", sha]);
    assert.equal(git(dest, ["rev-parse", "HEAD"]), sha);
    assert.equal(fs.readFileSync(path.join(dest, "README"), "utf8").replace(/\r\n/g, "\n"), "ci\n");
  });
});
