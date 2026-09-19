# Builds and installs hit (go install, version stamped from git).
#   ./scripts/install.ps1           # install, keep history
#   ./scripts/install.ps1 -Clear    # install and start with an empty history
#
# -Clear moves the current history file into <data dir>\backup\ instead of deleting it:
# the finder starts clean, and nothing typed is ever destroyed.
# Honours HIT_DATA_DIR (and GOBIN), so tests can run it against temp dirs.
param(
    [switch]$Clear
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

if (Get-Module hit) {
    Write-Host 'open a new terminal to load the new version (this one still runs the old one)'
}
