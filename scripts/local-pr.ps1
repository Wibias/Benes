[CmdletBinding()]
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [ValidateRange(1, 2147483647)]
    [int]$PR,
    [string]$WslDistribution = "",
    [switch]$SkipLinux,
    [switch]$SkipDarwinCompile,
    [string]$MacHost,
    [string]$MacRepoPath,
    [switch]$Full
)

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

. (Join-Path $PSScriptRoot "ci-local-forward.ps1")

function Fail([string]$Message) {
    throw "local-pr: $Message"
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

function Invoke-GitFetchNoMaintenance {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Root,
        [Parameter(Mandatory = $true)]
        [string[]]$FetchArgs
    )

    & git -c gc.auto=0 -c maintenance.auto=false -C $Root fetch @FetchArgs | Out-Host
    if ($LASTEXITCODE -ne 0) {
        Fail "git fetch failed: $($FetchArgs -join ' ')"
    }
}

function Get-RepoRoot {
    $root = (& git rev-parse --show-toplevel 2>$null)
    if ($LASTEXITCODE -ne 0 -or -not $root) {
        Fail "run this script from a Benes checkout"
    }
    return ($root | Select-Object -Last 1).Trim()
}

function Get-RepoSlug {
    $slug = (& gh repo view --json nameWithOwner --jq .nameWithOwner 2>$null)
    if ($LASTEXITCODE -ne 0 -or -not $slug) {
        Fail "could not resolve the GitHub repository; ensure gh is authenticated for this checkout"
    }
    return ($slug | Select-Object -Last 1).Trim()
}

function Get-PrSnapshot([string]$RepoSlug, [int]$Number) {
    $raw = & gh api ("repos/{0}/pulls/{1}" -f $RepoSlug, $Number)
    if ($LASTEXITCODE -ne 0 -or -not $raw) {
        Fail "could not read PR #$Number from $RepoSlug"
    }

    try {
        $data = (($raw -join [Environment]::NewLine) | ConvertFrom-Json)
    } catch {
        Fail "could not parse GitHub metadata for PR #$Number"
    }

    $headSha = [string]$data.head.sha
    $baseSha = [string]$data.base.sha
    $baseRef = [string]$data.base.ref
    $state = [string]$data.state
    $url = [string]$data.html_url

    if (-not $headSha -or -not $baseSha -or -not $baseRef -or -not $state) {
        Fail "GitHub returned incomplete metadata for PR #$Number"
    }

    return [pscustomobject]@{
        HeadSha = $headSha
        BaseSha = $baseSha
        BaseRef = $baseRef
        State = $state
        Draft = [bool]$data.draft
        Url = $url
    }
}

function Get-LiveBaseSha([string]$Root, [string]$BaseRef) {
    Invoke-GitFetchNoMaintenance -Root $Root -FetchArgs @(
        "--no-tags",
        "origin",
        $BaseRef
    )
    $fetchedBase = (& git -C $Root rev-parse FETCH_HEAD).Trim()
    if ($LASTEXITCODE -ne 0 -or -not $fetchedBase) {
        Fail "could not resolve current base ref $BaseRef"
    }
    return $fetchedBase
}

function Assert-RemoteSnapshot([string]$Root, [int]$Number, $Snapshot) {
    if ($Snapshot.State -ne "open") {
        Fail "PR #$Number is not open (state=$($Snapshot.State))"
    }

    Invoke-GitFetchNoMaintenance -Root $Root -FetchArgs @(
        "--no-tags",
        "origin",
        ("refs/pull/{0}/head" -f $Number)
    )
    $fetchedHead = (& git -C $Root rev-parse FETCH_HEAD).Trim()
    if ($LASTEXITCODE -ne 0 -or $fetchedHead -ne $Snapshot.HeadSha) {
        Fail "PR #$Number head changed while resolving it: API=$($Snapshot.HeadSha), fetched=$fetchedHead"
    }

    $liveBaseSha = Get-LiveBaseSha $Root $Snapshot.BaseRef

    Invoke-GitFetchNoMaintenance -Root $Root -FetchArgs @(
        "--no-tags",
        "origin",
        "+refs/heads/main:refs/remotes/origin/main"
    )

    return $liveBaseSha
}

function New-DetachedWorktree([string]$Root, [string]$Sha) {
    $base = Join-Path ([System.IO.Path]::GetTempPath()) ("benes-pr-ci-{0}-{1}" -f $PR, [guid]::NewGuid().ToString("N"))
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

Assert-Command git
Assert-Command gh
Assert-Command node
Assert-Command npm
Assert-Command go

$repoRoot = Get-RepoRoot
$repoSlug = Get-RepoSlug
$before = Get-PrSnapshot $repoSlug $PR
$liveBaseShaBefore = Assert-RemoteSnapshot $repoRoot $PR $before

Write-Host "`nBenes local PR CI"
Write-Host "PR:   #$PR $($before.Url)"
Write-Host "Base: $($before.BaseRef) @ $liveBaseShaBefore"
if ($before.BaseSha -ne $liveBaseShaBefore) {
    Write-Host "NOTE: PR API base snapshot is $($before.BaseSha); current $($before.BaseRef) is $liveBaseShaBefore."
}
Write-Host "Head: $($before.HeadSha)"
if ($before.Draft) {
    Write-Host "NOTE: PR #$PR is currently a draft."
}

$forceFull = $Full.IsPresent -or $env:BENES_CI_PR_FULL -eq "1"
$changedNames = @(& git -C $repoRoot diff --name-only "$liveBaseShaBefore...$($before.HeadSha)")
if ($LASTEXITCODE -ne 0) {
    Fail "could not list changed paths for PR #$PR"
}
$env:CI_PR_FILES = ($changedNames -join "`n")
$classifyArgs = @(
    "--experimental-strip-types",
    (Join-Path $repoRoot "scripts\ci-pr-scope.ts")
)
if ($forceFull) { $classifyArgs += "--full" }
$classifyRaw = & node @classifyArgs
if ($LASTEXITCODE -ne 0 -or -not $classifyRaw) {
    Fail "could not classify PR #$PR path buckets"
}
try {
    $buckets = (($classifyRaw | Out-String) | ConvertFrom-Json)
} catch {
    Fail "could not parse PR #$PR path buckets"
}
Write-Host "Diff: $($changedNames.Count) path(s) vs $($before.BaseRef)@$liveBaseShaBefore"
Write-Host "Scope: $($buckets.scope)"
Remove-Item Env:CI_PR_FILES -ErrorAction SilentlyContinue

$wt = $null
try {
    $wt = New-DetachedWorktree $repoRoot $before.HeadSha
    Push-Location $wt.Path
    try {
        $actual = (& git rev-parse HEAD).Trim()
        if ($LASTEXITCODE -ne 0 -or $actual -ne $before.HeadSha) {
            Fail "detached worktree is not on the expected PR head"
        }

        if ($buckets.gui) {
            Write-Host "`n== PR #$PR gui gate @ $($before.HeadSha) =="
            Push-Location gui
            try {
                Invoke-Checked npm ci
                Invoke-Checked npm run lint
                Invoke-Checked npm run build
            } finally {
                Pop-Location
            }
            Write-Host "PASS: PR #$PR gui @ $($before.HeadSha)"
        }

        if ($buckets.docs) {
            Write-Host "`n== PR #$PR docs gate @ $($before.HeadSha) =="
            Push-Location docs
            try {
                Invoke-Checked npm ci
                Invoke-Checked npm run build
                Invoke-Checked npm audit --audit-level=high
            } finally {
                Pop-Location
            }
            Write-Host "PASS: PR #$PR docs @ $($before.HeadSha)"
        }

        if ($buckets.automation -and -not $buckets.ciLocal) {
            Write-Host "`n== PR #$PR automation tests @ $($before.HeadSha) =="
            Invoke-Checked node .github/scripts/run-automation-tests.cjs
            Write-Host "PASS: PR #$PR automation @ $($before.HeadSha)"
        }

        if ($buckets.privacy -and -not $buckets.ciLocal) {
            Write-Host "`n== PR #$PR privacy scan @ $($before.HeadSha) =="
            Invoke-Checked node --experimental-strip-types scripts/privacy-scan.ts
            Write-Host "PASS: PR #$PR privacy @ $($before.HeadSha)"
        }

        if ($buckets.ciLocal) {
            Write-Host "`n== PR #$PR canonical local cross-platform CI @ $($before.HeadSha) =="
            $previousScope = $env:BENES_CI_SCOPE
            try {
                if ($buckets.full) {
                    Remove-Item Env:BENES_CI_SCOPE -ErrorAction SilentlyContinue
                } else {
                    $env:BENES_CI_SCOPE = [string]$buckets.scope
                }
                $ciArgs = New-CiLocalSplat `
                    -WslDistribution $WslDistribution `
                    -SkipLinux:($SkipLinux -or (-not $buckets.full -and -not $buckets.linux)) `
                    -SkipDarwinCompile:($SkipDarwinCompile -or (-not $buckets.full -and -not $buckets.darwin)) `
                    -MacHost $MacHost `
                    -MacRepoPath $MacRepoPath

                & (Join-Path $wt.Path "scripts\ci-local.ps1") @ciArgs
                if (-not $?) {
                    Fail "canonical local CI failed"
                }
            } finally {
                if ($null -eq $previousScope) {
                    Remove-Item Env:BENES_CI_SCOPE -ErrorAction SilentlyContinue
                } else {
                    $env:BENES_CI_SCOPE = $previousScope
                }
            }
        }

        if (-not $buckets.gui -and -not $buckets.docs -and -not $buckets.automation -and -not $buckets.privacy -and -not $buckets.ciLocal) {
            Write-Host "`nSKIP: PR #$PR has no local CI buckets for this diff (pass -Full to run everything)"
        }
    } finally {
        Pop-Location
    }

    $after = Get-PrSnapshot $repoSlug $PR
    if ($after.HeadSha -ne $before.HeadSha) {
        Fail "PR #$PR head moved during verification: was $($before.HeadSha), now $($after.HeadSha)"
    }
    if ($after.BaseRef -ne $before.BaseRef) {
        Fail "PR #$PR base ref moved during verification: was $($before.BaseRef), now $($after.BaseRef)"
    }
    if ($after.State -ne "open") {
        Fail "PR #$PR is no longer open after verification (state=$($after.State))"
    }

    $liveBaseShaAfter = Get-LiveBaseSha $repoRoot $after.BaseRef
    if ($liveBaseShaAfter -ne $liveBaseShaBefore) {
        Fail "PR #$PR base moved during verification: was $($after.BaseRef)@$liveBaseShaBefore, now $($after.BaseRef)@$liveBaseShaAfter"
    }

    Write-Host "`nPASS: PR #$PR exact-head local CI completed"
    Write-Host "Base: $($after.BaseRef) @ $liveBaseShaAfter"
    Write-Host "Head: $($after.HeadSha)"
    if (-not $MacHost) {
        Write-Host "NOTE: macOS is compile-only. Full macOS runtime/keychain/package evidence requires -MacHost and -MacRepoPath."
    }
} finally {
    Remove-DetachedWorktree $wt $repoRoot
}
