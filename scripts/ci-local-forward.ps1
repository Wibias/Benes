function New-CiLocalSplat {
    param(
        [string]$WslDistribution = "",
        [switch]$SkipLinux,
        [switch]$SkipDarwinCompile,
        [string]$MacHost,
        [string]$MacRepoPath
    )

    $splat = @{}
    if (-not [string]::IsNullOrWhiteSpace($WslDistribution)) {
        $splat.WslDistribution = $WslDistribution
    }
    if ($SkipLinux) {
        $splat.SkipLinux = $true
    }
    if ($SkipDarwinCompile) {
        $splat.SkipDarwinCompile = $true
    }
    if (-not [string]::IsNullOrWhiteSpace($MacHost)) {
        $splat.MacHost = $MacHost
    }
    if (-not [string]::IsNullOrWhiteSpace($MacRepoPath)) {
        $splat.MacRepoPath = $MacRepoPath
    }
    return $splat
}
