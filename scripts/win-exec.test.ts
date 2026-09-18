import assert from "node:assert/strict";
import { describe, test } from "node:test";
import { commandInvocation } from "./win-exec.ts";

describe("POSIX spawn plan", () => {
  test("does not wrap a native executable in a shell", () => {
    assert.deepEqual(commandInvocation("go", ["test", "./..."], "linux"), {
      file: "go",
      args: ["test", "./..."],
      options: {},
    });
  });
});

describe("Windows native images", () => {
  test("keeps argv boundaries for a path that already names an .exe", () => {
    const plan = commandInvocation(
      "C:\\Go\\bin\\go.exe",
      ["test", "./internal/server", "-count=1"],
      "win32",
    );
    assert.equal(plan.file, "C:\\Go\\bin\\go.exe");
    assert.deepEqual(plan.args, ["test", "./internal/server", "-count=1"]);
    assert.equal(plan.options.windowsVerbatimArguments, undefined);
  });

  test("resolves a bare name through case-insensitive PATH and PATHEXT", () => {
    const plan = commandInvocation("go", ["version"], "win32", {
      env: { Path: "C:\\Go\\bin;C:\\missing", pathext: ".COM;.EXE;.CMD" },
      exists: (candidate) => candidate.toLowerCase() === "c:\\go\\bin\\go.exe",
    });
    assert.equal(plan.file, "C:\\Go\\bin\\go.exe");
    assert.deepEqual(plan.args, ["version"]);
  });

  test("leaves an unresolved bare name for spawn to fail honestly", () => {
    const plan = commandInvocation("not-installed", ["--help"], "win32", {
      env: { PATH: "C:\\none", PATHEXT: ".EXE" },
      exists: () => false,
    });
    assert.equal(plan.file, "not-installed");
    assert.deepEqual(plan.args, ["--help"]);
  });

  test("does not search PATH when the command already has an extension or separator", () => {
    const exists = () => {
      throw new Error("must not search PATH");
    };
    assert.equal(commandInvocation("git.exe", [], "win32", { exists }).file, "git.exe");
    assert.equal(commandInvocation("C:\\tools\\rg.exe", [], "win32", { exists }).file, "C:\\tools\\rg.exe");
    assert.equal(commandInvocation("bin/foo", [], "win32", { exists }).file, "bin/foo");
  });

  test("does not invent a hit when PATH or PATHEXT is empty", () => {
    assert.equal(
      commandInvocation("benes", [], "win32", { env: { PATH: "", PATHEXT: ".CMD" }, exists: () => true }).file,
      "benes",
    );
    assert.equal(
      commandInvocation("benes", [], "win32", { env: { PATH: "C:\\npm", PATHEXT: "" }, exists: () => true }).file,
      "benes",
    );
  });
});

describe("Windows batch wrapping", () => {
  test("routes .cmd through cmd.exe /d /s /c with verbatim arguments", () => {
    const plan = commandInvocation("benes", ["help"], "win32", {
      env: {
        ComSpec: "C:\\Windows\\System32\\cmd.exe",
        PATH: "C:\\npm",
        PATHEXT: ".CMD",
      },
      exists: (candidate) => candidate === "C:\\npm\\benes.cmd",
    });
    assert.equal(plan.file, "C:\\Windows\\System32\\cmd.exe");
    assert.deepEqual(plan.args.slice(0, 3), ["/d", "/s", "/c"]);
    assert.match(plan.args[3] ?? "", /^".+"$/);
    assert.equal(plan.options.windowsVerbatimArguments, true);
  });

  test("preserves spaces, quotes, and cmd metacharacters in the batch line", () => {
    const plan = commandInvocation(
      "C:\\tools\\run.cmd",
      ["--out", "C:\\Program Files\\out.txt", 'say "hi"', "a&b"],
      "win32",
      { env: { ComSpec: "cmd.exe" } },
    );
    const line = plan.args[3] ?? "";
    assert.match(line, /Program\^ Files/);
    assert.match(line, /\^&/);
    assert.match(line, /\\\^"/);
  });

  test("applies a second caret pass only for npm node_modules/.bin shims", () => {
    const env = { ComSpec: "cmd.exe" };
    const globalCmd = commandInvocation("C:\\tools\\oxlint.cmd", ["--help"], "win32", { env });
    const shimCmd = commandInvocation(
      "C:\\repo\\node_modules\\.bin\\oxlint.cmd",
      ["--help"],
      "win32",
      { env },
    );
    assert.notEqual(shimCmd.args[3], globalCmd.args[3]);
    assert.ok((shimCmd.args[3] ?? "").length > (globalCmd.args[3] ?? "").length);
  });
});
