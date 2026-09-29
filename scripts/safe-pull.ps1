# Updates this clone to origin/main without a merge, keeping your own copies of the files
# you edit locally (scripts/profile by default) exactly as they are.
#   ./scripts/safe-pull.ps1                                  # keep scripts/profile
#   ./scripts/safe-pull.ps1 -Keep scripts/profile, sample/config.toml
#
# For a clone that is only ever pulled, never committed to. A normal pull (or stash + pull
# + pop) tries to merge your edits into whatever changed upstream and leaves conflicts;
# this never merges:
#   1. copies the kept paths to a backup folder outside the repo,
#   2. saves any OTHER local edits there as discarded.patch, so nothing is ever lost,
#   3. git fetch + git reset --hard origin/main,
#   4. copies the kept paths back, verbatim — your version wins, upstream's is ignored.
# The backup folder is printed and left in place. Untracked files are never touched.
param(
    [string[]]$Keep = @('scripts/profile'),
    [string]$Remote = 'origin',
    [string]$Branch = 'main'
)
$ErrorActionPreference = 'Stop'

function Invoke-Git {
    $out = & git @args 2>&1
    if ($LASTEXITCODE) { throw "git $($args -join ' ') failed: $out" }
    $out
}

$root = Split-Path $PSScriptRoot -Parent
Push-Location $root
try {
    $current = Invoke-Git rev-parse --abbrev-ref HEAD
    if ($current -ne $Branch) { throw "on '$current', not $Branch; switch first (git switch $Branch)" }

    $backup = Join-Path ([IO.Path]::GetTempPath()) ('hit-safe-pull\{0:yyyyMMdd-HHmmss}' -f (Get-Date))
    $null = New-Item -ItemType Directory -Force -Path $backup

    # 1. The kept paths, as they are on disk (tracked or not).
    $kept = @()
    foreach ($k in $Keep) {
        $src = Join-Path $root $k
        if (-not (Test-Path -LiteralPath $src)) { Write-Warning "nothing at $k; skipped"; continue }
        $dest = Join-Path $backup "keep\$k"
        $null = New-Item -ItemType Directory -Force -Path (Split-Path $dest -Parent)
        Copy-Item -LiteralPath $src -Destination $dest -Recurse -Force
        $kept += $k
    }

    # 2. Everything else you changed, which the reset is about to throw away.
    # @() and a quoted -- : one kept path must still splat as a list, and PowerShell eats a
    # bare -- passed to a function.
    $exclude = @($kept | ForEach-Object { ":(exclude)$_" })
    $discard = @(Invoke-Git diff HEAD --name-only '--' . @exclude)
    if ($discard) {
        Invoke-Git diff HEAD --binary '--' . @exclude | Set-Content -LiteralPath (Join-Path $backup 'discarded.patch')
        Write-Host "discarding local edits (saved to discarded.patch):"
        $discard | ForEach-Object { Write-Host "  $_" }
    }

    # 3. Become exactly what the remote has.
    $before = Invoke-Git rev-parse --short HEAD
    Invoke-Git fetch --quiet --tags $Remote $Branch | Out-Null
    Invoke-Git reset --quiet --hard "$Remote/$Branch" | Out-Null
    $after = Invoke-Git rev-parse --short HEAD

    # 4. Your kept files back, over whatever upstream has.
    foreach ($k in $kept) {
        $dest = Join-Path $root $k
        $parent = Split-Path $dest -Parent
        $null = New-Item -ItemType Directory -Force -Path $parent
        Copy-Item -LiteralPath (Join-Path $backup "keep\$k") -Destination $parent -Recurse -Force
    }

    if ($before -eq $after) { Write-Host "already at $Remote/$Branch ($after)" }
    else { Write-Host "updated $before -> $after"; Invoke-Git log --oneline "$before..$after" | ForEach-Object { Write-Host "  $_" } }
    if ($kept) { Write-Host "kept your copies of: $($kept -join ', ')" }
    Write-Host "backup: $backup"
} finally {
    Pop-Location
}
