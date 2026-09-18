#Requires -Version 5.1
#
# benes installer for Windows PowerShell (5.1 and later).
#
# The run is a fixed pipeline of stages driven by Invoke-Install. Windows needs
# two things a plain presence check misses: the npm and benes launchers are
# command shims (npm.cmd / benes.cmd) that must win over any other candidate on
# PATH, and a native child failing must stop the installer with that child's
# own non-zero status instead of PowerShell's success status.
#
# Stage order: announce -> preflight -> version -> package -> locate -> health
# -> handoff. The toolchain table is the single source of truth for preflight.
$ErrorActionPreference = "Stop"

$PackageName = "benes"
$NodeFloor = 18
$NodeOrigin = "https://nodejs.org/"
$GoOrigin = "https://go.dev/dl/"

$Toolchain = @(
    @{ Binary = "node"; Remedy = "Node.js 18+ is required. Install it from $NodeOrigin and rerun." }
    @{ Binary = "npm"; Remedy = "npm is required to install the published $PackageName package." }
    @{ Binary = "go"; Remedy = "Go 1.27.0 is required. Install it from $GoOrigin and rerun." }
)

function Write-Stage {
    param([string]$Message = "")
    Write-Host $Message
}

# Written straight to the error stream so the process can still choose its own
# exit status. Write-Error would terminate before `exit` could set the code.
function Stop-Install {
    param([Parameter(Mandatory)][string]$Message, [int]$Code = 1)
    [Console]::Error.WriteLine($Message)
    exit $Code
}

# Resolve the first candidate that exists, in the order given. The order is the
# contract: shims are listed before their extension-less names.
function Resolve-Launcher {
    param([Parameter(Mandatory)][string[]]$Candidate)
    foreach ($name in $Candidate) {
        $found = Get-Command -Name $name -ErrorAction SilentlyContinue
        if ($found) { return $found }
    }
    return $null
}

function Invoke-Preflight {
    foreach ($entry in $Toolchain) {
        if (-not (Resolve-Launcher -Candidate @($entry.Binary))) {
            Stop-Install $entry.Remedy
        }
    }
}

function Get-NodeVersion {
    $raw = & node -p "process.versions.node"
    $text = ($raw | Select-Object -First 1)
    if (-not $text) { Stop-Install "Node.js 18+ is required. Install it from $NodeOrigin and rerun." }
    return $text.ToString().Trim()
}

function Assert-NodeFloor {
    param([Parameter(Mandatory)][string]$Version)
    $major = 0
    if (-not [int]::TryParse(($Version -split '\.')[0], [ref]$major) -or $major -lt $NodeFloor) {
        Stop-Install "Node.js 18+ is required. Current version: v$Version"
    }
}

function Invoke-PackageInstall {
    param([Parameter(Mandatory)]$Npm)
    & $Npm.Source install -g $PackageName
    if ($LASTEXITCODE -ne 0) {
        Stop-Install "npm install -g $PackageName failed with exit code $LASTEXITCODE" $LASTEXITCODE
    }
}

function Resolve-BenesLauncher {
    param([Parameter(Mandatory)]$Npm)
    $launcher = Resolve-Launcher -Candidate @("benes.cmd", "benes")
    if (-not $launcher) {
        $prefix = & $Npm.Source prefix -g
        Stop-Install "$PackageName is installed but not on PATH. Add the npm global bin directory, then reopen PowerShell: $prefix"
    }
    return $launcher
}

function Assert-BenesHealth {
    param([Parameter(Mandatory)]$Launcher)
    & $Launcher.Source help *> $null
    if ($LASTEXITCODE -ne 0) {
        Stop-Install "$PackageName is on PATH but help failed with exit code $LASTEXITCODE." $LASTEXITCODE
    }
}

function Invoke-Install {
    Write-Stage "Installing $PackageName..."
    Invoke-Preflight
    $nodeVersion = Get-NodeVersion
    Assert-NodeFloor -Version $nodeVersion
    Write-Stage "Using Node v$nodeVersion"
    Write-Stage "Using $(go version)"
    $npm = Resolve-Launcher -Candidate @("npm.cmd", "npm")
    if (-not $npm) { Stop-Install "npm is required to install the published $PackageName package." }
    Invoke-PackageInstall -Npm $npm
    $launcher = Resolve-BenesLauncher -Npm $npm
    Assert-BenesHealth -Launcher $launcher
    Write-Stage ""
    Write-Stage "$PackageName is installed. Next: $PackageName init"
}

Invoke-Install
