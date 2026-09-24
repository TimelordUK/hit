# Builds and installs hit (go install, version stamped from git).
#   ./scripts/install.ps1           # install, keep history
#   ./scripts/install.ps1 -Clear    # install and start with an empty history
#
# -Clear moves the current history file into <data dir>\backup\ instead of deleting it:
# the finder starts clean, and nothing typed is ever destroyed.
# Honours HIT_DATA_DIR (and GOBIN), so tests can run it against temp dirs.
#   ./scripts/install.ps1 -KeepServers   # leave running resident finders alone
#
# A resident finder (C-031) keeps running the binary it was started from, so after an
# install the *old* code would go on answering Ctrl+R in every shell that already has one
# — which on a machine where releases are frequent means testing a change against the
# version it replaced. They are stopped by default. That is safe by construction: every
# failure on the server path falls back to spawning, and the next recall starts a fresh
# server (DESIGN §16).
param(
    [switch]$Clear,
    [switch]$KeepServers
)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent

Push-Location $root
try {
    $version = git describe --tags --always --dirty 2>$null
    if ($LASTEXITCODE -or -not $version) { $version = 'dev' }
    go install -ldflags "-X main.version=$version" ./cmd/hit
    if ($LASTEXITCODE) { throw 'go install failed' }
} finally {
    Pop-Location
}

$bin = go env GOBIN
if (-not $bin) { $bin = Join-Path (go env GOPATH) 'bin' }
$exe = Join-Path $bin ($IsWindows ? 'hit.exe' : 'hit')
Write-Host "installed $(& $exe version) -> $exe"

if ($Clear) {
    $hist = & $exe path history
    if ($LASTEXITCODE) { throw 'hit path history failed' }
    if (Test-Path -LiteralPath $hist) {
        $count = @(Select-String -LiteralPath $hist -SimpleMatch '"k":"cmd"').Count
        $backup = Join-Path (Split-Path $hist -Parent) 'backup'
        $null = New-Item -ItemType Directory -Force -Path $backup
        $dest = Join-Path $backup ('history-{0:yyyyMMdd-HHmmss}.jsonl' -f (Get-Date))
        # A shell may be mid-append (the file is held for a few ms): retry briefly.
        for ($i = 0; ; $i++) {
            try { Move-Item -LiteralPath $hist -Destination $dest; break }
            catch [System.IO.IOException] { if ($i -ge 20) { throw }; Start-Sleep -Milliseconds 50 }
        }
        Write-Host "cleared history: $count commands moved to $dest"
    } else {
        Write-Host "no history to clear at $hist"
    }
}

if (-not $KeepServers) {
    # Matched on the command line rather than on the image path, because the point is to
    # catch servers started from an *older* binary, wherever that binary lived.
    $servers = @(Get-CimInstance Win32_Process -Filter "Name='hit.exe'" -ErrorAction SilentlyContinue |
            Where-Object { $_.CommandLine -and $_.CommandLine -match '"?\s+serve(\s|$)' })
    foreach ($s in $servers) {
        Stop-Process -Id $s.ProcessId -Force -ErrorAction SilentlyContinue
    }
    if ($servers.Count) {
        Write-Host "stopped $($servers.Count) resident finder(s); the next Ctrl+R starts one on this build"
    }
}

if (Get-Module hit) {
    Write-Host 'open a new terminal to load the new version (this one still runs the old one)'
}
