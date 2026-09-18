import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { describe, test } from "node:test";
import { fileURLToPath } from "node:url";

const scriptsDir = path.dirname(fileURLToPath(import.meta.url));
const localPrPath = path.join(scriptsDir, "local-pr.ps1");
const forwardPath = path.join(scriptsDir, "ci-local-forward.ps1");

describe("local-pr ci-local forwarding", () => {
  test("does not string-splat skip switches into WslDistribution", () => {
    const source = readFileSync(localPrPath, "utf8");
    assert.equal(
      source.includes('$ciArgs += "-SkipDarwinCompile"'),
      false,
      "array splat of -SkipDarwinCompile binds to WslDistribution",
    );
    assert.equal(
      source.includes('$ciArgs += "-SkipLinux"'),
      false,
      "array splat of -SkipLinux binds to WslDistribution",
    );
  });

  test("named splat keeps SkipDarwinCompile off WslDistribution", (t) => {
    const shell = process.platform === "win32" ? "powershell" : "pwsh";
    const probe = spawnSync(shell, ["-NoProfile", "-Command", "$PSVersionTable.PSVersion"], {
      encoding: "utf8",
    });
    if (probe.error || probe.status !== 0) {
      t.skip(`${shell} unavailable`);
      return;
    }

    const dir = mkdtempSync(path.join(tmpdir(), "benes-ci-local-forward-"));
    try {
      const stubPath = path.join(dir, "ci-local.ps1");
      const runnerPath = path.join(dir, "run.ps1");
      writeFileSync(
        stubPath,
        [
          "param(",
          "    [string]$WslDistribution = '',",
          "    [switch]$SkipLinux,",
          "    [switch]$SkipDarwinCompile,",
          "    [string]$MacHost,",
          "    [string]$MacRepoPath",
          ")",
          "@{",
          "    WslDistribution = $WslDistribution",
          "    SkipLinux = [bool]$SkipLinux",
          "    SkipDarwinCompile = [bool]$SkipDarwinCompile",
          "} | ConvertTo-Json -Compress",
        ].join("\n"),
        "utf8",
      );
      writeFileSync(
        runnerPath,
        [
          `$ErrorActionPreference = 'Stop'`,
          `. '${forwardPath.replaceAll("'", "''")}'`,
          `$ciArgs = New-CiLocalSplat -SkipDarwinCompile`,
          `& '${stubPath.replaceAll("'", "''")}' @ciArgs`,
        ].join("\n"),
        "utf8",
      );
      const result = spawnSync(shell, ["-NoProfile", "-ExecutionPolicy", "Bypass", "-File", runnerPath], {
        encoding: "utf8",
      });
      assert.equal(result.status, 0, result.stderr || result.stdout);
      const bound = JSON.parse((result.stdout || "").trim().split(/\r?\n/).at(-1) || "{}");
      assert.equal(bound.WslDistribution, "");
      assert.equal(bound.SkipDarwinCompile, true);
      assert.equal(bound.SkipLinux, false);
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });
});
