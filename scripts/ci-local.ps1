[CmdletBinding()]
param(
    [string]$WslDistribution = "",
    [switch]$SkipLinux,
    [switch]$SkipDarwinCompile,
    [string]$MacHost,
    [string]$MacRepoPath,
    [switch]$SkipWindows
)

function Test-CiScope([string]$Name) {
    $raw = [string]$env:BENES_CI_SCOPE
    if ([string]::IsNullOrWhiteSpace($raw) -or $raw -eq "all") { return $true }
    $parts = @($raw.Split(",") | ForEach-Object { $_.Trim() } | Where-Object { $_ })
    return $parts -contains $Name
}

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

function Enable-Utf8Console {
    $utf8 = New-Object System.Text.UTF8Encoding $false
    [Console]::InputEncoding = $utf8
    [Console]::OutputEncoding = $utf8
    $script:OutputEncoding = $utf8
    & chcp.com 65001 | Out-Null
}

Enable-Utf8Console

function Fail([string]$Message) {
    throw "ci-local: $Message"
}

function Assert-Command([string]$Name) {
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        Fail "required command not found: $Name"
    }
}

function Invoke-Checked {
    if ($args.Count -lt 1) { Fail "Invoke-Checked requires a command" }
    $filePath = [string]$args[0]
    $argumentList = @()
    if ($args.Count -gt 1) { $argumentList = @($args[1..($args.Count - 1)]) }
    & $filePath @argumentList | Out-Host
    if ($LASTEXITCODE -ne 0) {
        Fail "command failed ($LASTEXITCODE): $filePath $($argumentList -join ' ')"
    }
}

function Get-RepoRoot {
    $root = (& git rev-parse --show-toplevel 2>$null)
    if ($LASTEXITCODE -ne 0 -or -not $root) {
        Fail "run this script from a Benes checkout"
    }
    return $root.Trim()
}

function Assert-Toolchains {
    $nodeMajor = [int](& node -p "Number(process.versions.node.split('.')[0])")
    if ($LASTEXITCODE -ne 0 -or $nodeMajor -ne 24) {
        Fail "Node 24 is required to match .github/workflows/ci.yml; found $(& node --version)"
    }
    $toolchainLine = Get-Content go.mod | Where-Object { $_ -match '^toolchain\s+' } | Select-Object -First 1
    if ($toolchainLine) {
        $expectedGo = ($toolchainLine -split '\s+')[1]
        $actualGo = (& go env GOVERSION).Trim()
        if ($LASTEXITCODE -ne 0 -or $actualGo -ne $expectedGo) {
            Fail "Go toolchain $expectedGo is required; found $actualGo"
        }
    }
}

function New-DetachedWorktree([string]$Root, [string]$Sha, [string]$Label) {
    $base = Join-Path ([System.IO.Path]::GetTempPath()) ("benes-ci-{0}-{1}" -f $Label, [guid]::NewGuid().ToString("N"))
    New-Item -ItemType Directory -Path $base | Out-Null
    $worktree = Join-Path $base "repo"
    Invoke-Checked git -C $Root worktree add --detach $worktree $Sha
    return [pscustomobject]@{ Base = $base; Path = $worktree }
}

function Remove-DetachedWorktree($Worktree, [string]$Root) {
    if ($null -eq $Worktree) { return }

    $previousErrorActionPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = "Continue"
        $removed = $false

        for ($attempt = 1; $attempt -le 5; $attempt++) {
            & git -c gc.auto=0 -c maintenance.auto=false -C $Root worktree remove --force $Worktree.Path 2>$null | Out-Null
            if ($LASTEXITCODE -eq 0) {
                $removed = $true
                break
            }
            Start-Sleep -Milliseconds (200 * $attempt)
        }

        if (-not $removed) {
            & git -c gc.auto=0 -c maintenance.auto=false -C $Root worktree prune --expire now 2>$null | Out-Null
            if (Test-Path -LiteralPath $Worktree.Path) {
                Remove-Item -LiteralPath $Worktree.Path -Recurse -Force -ErrorAction SilentlyContinue
            }
            if (Test-Path -LiteralPath $Worktree.Path) {
                Fail "could not remove temporary worktree after 5 attempts: $($Worktree.Path)"
            }
        }
    } finally {
        $ErrorActionPreference = $previousErrorActionPreference
    }

    Remove-Item -LiteralPath $Worktree.Base -Recurse -Force -ErrorAction SilentlyContinue
}

function Invoke-WindowsEvidence([string]$Root, [string]$Sha) {
    $wt = $null
    try {
        $wt = New-DetachedWorktree $Root $Sha "windows"
        Push-Location $wt.Path
        try {
            Assert-Toolchains
            Write-Host "`n== Benes local CI: Windows @ $Sha =="
            Invoke-Checked node --version
            Invoke-Checked go version

            if (Test-CiScope "automation") {
                Write-Host "`n-- Workflow contract --"
                Invoke-Checked node .github/scripts/run-automation-tests.cjs
            }

            if (Test-CiScope "privacy") {
                Write-Host "`n-- Privacy --"
                Invoke-Checked node --experimental-strip-types scripts/privacy-scan.ts
            }

            if (Test-CiScope "go") {
                Write-Host "`n-- Go core --"
                $binary = Join-Path $wt.Base "benes.exe"
                Invoke-Checked go build -o $binary ./cmd/benes
                Invoke-Checked go test ./...
                Invoke-Checked go vet ./...
            }

            if (Test-CiScope "keyring") {
                Write-Host "`n-- Windows keyring smoke --"
                Invoke-Checked npm install
                Invoke-Checked node --experimental-strip-types scripts/keyring-smoke.ts
            }

            if (Test-CiScope "packaging") {
                Write-Host "`n-- npm global packaging smoke --"
                Invoke-Checked git clean -fdx
                Invoke-Checked npm install --omit=dev
                Invoke-Checked npm run build:gui
                $packJson = & npm pack --json
                if ($LASTEXITCODE -ne 0) { Fail "npm pack failed" }
                [System.IO.File]::WriteAllText((Join-Path (Get-Location) "pack.json"), ($packJson -join [Environment]::NewLine), (New-Object System.Text.UTF8Encoding($false)))
                Invoke-Checked node .github/scripts/npm-pack-json.cjs verify pack.json
                $tarball = (& node .github/scripts/npm-pack-json.cjs filename pack.json).Trim()
                if ($LASTEXITCODE -ne 0) { Fail "could not read npm pack metadata" }
                if (-not $tarball -or -not (Test-Path -LiteralPath $tarball)) { Fail "npm pack did not report a usable tarball" }
                $prefix = Join-Path $wt.Base "npm-prefix"
                New-Item -ItemType Directory -Path $prefix | Out-Null
                Invoke-Checked npm install -g --prefix $prefix ("./" + $tarball)
                $oldPath = $env:PATH
                try {
                    $env:PATH = "$prefix;$oldPath"
                    Invoke-Checked benes help
                } finally {
                    $env:PATH = $oldPath
                }
            }
            Write-Host "`nPASS: Windows local CI @ $Sha"
        } finally {
            Pop-Location
        }
    } finally {
        Remove-DetachedWorktree $wt $Root
    }
}

function Invoke-LinuxEvidence([string]$Root, [string]$Sha, [string]$Distribution) {
    Assert-Command wsl.exe

    $distributionArgs = @()
    $distributionLabel = "default"
    if (-not [string]::IsNullOrWhiteSpace($Distribution)) {
        $distributionArgs = @("--distribution", $Distribution)
        $distributionLabel = $Distribution
    }

    $translateArgs = @($distributionArgs + @("--exec", "wslpath", "-a", $Root))
    $wslRootOutput = & wsl.exe @translateArgs
    $translateExit = $LASTEXITCODE
    if ($translateExit -ne 0 -or -not $wslRootOutput) {
        Fail "could not translate repository path using WSL distribution '$distributionLabel'"
    }
    $wslRoot = ($wslRootOutput | Select-Object -Last 1).Trim()
    if (-not $wslRoot) {
        Fail "WSL distribution '$distributionLabel' returned an empty repository path"
    }

    $gitCommonOutput = & git -C $Root rev-parse --path-format=absolute --git-common-dir
    $gitCommonExit = $LASTEXITCODE
    if ($gitCommonExit -ne 0 -or -not $gitCommonOutput) {
        Fail "could not resolve the Windows git common directory"
    }
    $gitCommonDir = ($gitCommonOutput | Select-Object -Last 1).Trim()
    if (-not $gitCommonDir) {
        Fail "Windows git returned an empty common directory"
    }

    $translateGitArgs = @($distributionArgs + @("--exec", "wslpath", "-a", $gitCommonDir))
    $wslGitCommonOutput = & wsl.exe @translateGitArgs
    $translateGitExit = $LASTEXITCODE
    if ($translateGitExit -ne 0 -or -not $wslGitCommonOutput) {
        Fail "could not translate the git common directory using WSL distribution '$distributionLabel'"
    }
    $wslGitCommonDir = ($wslGitCommonOutput | Select-Object -Last 1).Trim()
    if (-not $wslGitCommonDir) {
        Fail "WSL distribution '$distributionLabel' returned an empty git common directory"
    }

    Write-Host "`n== Starting Linux/WSL2 evidence ($distributionLabel) =="
    $envArgs = @(
        "BENES_CI_EXPECTED_SHA=$Sha",
        "BENES_CI_SOURCE_GIT_DIR=$wslGitCommonDir"
    )
    if (-not [string]::IsNullOrWhiteSpace($env:BENES_CI_SCOPE)) {
        $envArgs += "BENES_CI_SCOPE=$($env:BENES_CI_SCOPE)"
    }
    $runArgs = @($distributionArgs + @(
        "--exec", "env"
    ) + $envArgs + @(
        "bash", "$wslRoot/scripts/ci-local-linux.sh"
    ))
    Invoke-Checked wsl.exe @runArgs
}

function Invoke-DarwinCompileEvidence([string]$Root, [string]$Sha) {
    $wt = $null
    try {
        $wt = New-DetachedWorktree $Root $Sha "darwin-cross"
        Push-Location $wt.Path
        try {
            Assert-Toolchains
            foreach ($arch in @("amd64", "arm64")) {
                Write-Host "`n== macOS compile evidence: darwin/$arch @ $Sha =="
                $oldGOOS = $env:GOOS
                $oldGOARCH = $env:GOARCH
                $oldCGO = $env:CGO_ENABLED
                try {
                    $env:GOOS = "darwin"
                    $env:GOARCH = $arch
                    $env:CGO_ENABLED = "0"
                    $binary = Join-Path $wt.Base ("benes-darwin-{0}" -f $arch)
                    Invoke-Checked go build -o $binary ./cmd/benes
                    Invoke-Checked go build ./...
                    Invoke-Checked go vet ./...
                    $packages = & go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./...
                    if ($LASTEXITCODE -ne 0) { Fail "go list failed for darwin/$arch" }
                    $index = 0
                    foreach ($pkg in $packages) {
                        if (-not $pkg) { continue }
                        $testBinary = Join-Path $wt.Base ("darwin-{0}-test-{1}" -f $arch, $index)
                        Invoke-Checked go test -c -o $testBinary $pkg
                        $index += 1
                    }
                } finally {
                    $env:GOOS = $oldGOOS
                    $env:GOARCH = $oldGOARCH
                    $env:CGO_ENABLED = $oldCGO
                }
                Write-Host "PASS: macOS compile-only evidence darwin/$arch @ $Sha"
            }
        } finally {
            Pop-Location
        }
    } finally {
        Remove-DetachedWorktree $wt $Root
    }
}

function Invoke-RemoteMacEvidence([string]$Sha, [string]$HostName, [string]$RemoteRepo) {
    if (-not $HostName) { return }
    if (-not $RemoteRepo) { Fail "-MacRepoPath is required when -MacHost is set" }
    Assert-Command ssh
    Write-Host "`n== Starting full macOS evidence on $HostName =="
    $quotedRepo = $RemoteRepo.Replace("'", "'\\''")
    $scopePrefix = ""
    if (-not [string]::IsNullOrWhiteSpace($env:BENES_CI_SCOPE)) {
        $quotedScope = $env:BENES_CI_SCOPE.Replace("'", "'\\''")
        $scopePrefix = "BENES_CI_SCOPE='$quotedScope' "
    }
    $remote = "cd '$quotedRepo' && ${scopePrefix}BENES_CI_EXPECTED_SHA='$Sha' bash scripts/ci-local-macos.sh"
    Invoke-Checked ssh $HostName $remote
}

Assert-Command git
Assert-Command node
Assert-Command npm
Assert-Command go

$repoRoot = Get-RepoRoot
$headSha = (& git -C $repoRoot rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or -not $headSha) { Fail "could not resolve HEAD" }

Write-Host "Benes local CI target: $headSha"
if ($env:BENES_CI_SCOPE) {
    Write-Host "Scope: $env:BENES_CI_SCOPE"
}
if (-not $SkipWindows) { Invoke-WindowsEvidence $repoRoot $headSha }
if (-not $SkipLinux) { Invoke-LinuxEvidence $repoRoot $headSha $WslDistribution }
if (-not $SkipDarwinCompile) { Invoke-DarwinCompileEvidence $repoRoot $headSha }
Invoke-RemoteMacEvidence $headSha $MacHost $MacRepoPath

Write-Host "`nPASS: requested Benes local CI evidence completed for $headSha"
if (-not $MacHost) {
    Write-Host "NOTE: macOS is compile-only. Full macOS runtime/keychain/package evidence requires -MacHost and -MacRepoPath."
}
