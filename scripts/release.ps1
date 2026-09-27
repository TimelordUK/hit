# Tags the next version and pushes the tag, which is what triggers release.yml. The
# equivalent of `npm version patch`, except there is no version file to edit: the binary
# is stamped from `git describe` (install.ps1, release.yml), so the tag is the version.
#   ./scripts/release.ps1                  # v0.1.9 -> v0.1.10
#   ./scripts/release.ps1 -Bump minor      # v0.1.9 -> v0.2.0
#   ./scripts/release.ps1 -Message 'v0.1.10: server mode on Linux (S-035)'
#   ./scripts/release.ps1 -DryRun          # say what it would do, change nothing
#
# The message defaults to "<tag>: <subject of HEAD>". Refuses a dirty tree, a branch other
# than main, and a HEAD that is not exactly origin/main: a tag should name a commit that
# CI has already run on, and pushing main is left to you.
param(
    [ValidateSet('patch', 'minor', 'major')][string]$Bump = 'patch',
    [string]$Message,
    [switch]$DryRun
)
$ErrorActionPreference = 'Stop'

function Invoke-Git {
    $out = & git @args 2>&1
    if ($LASTEXITCODE) { throw "git $($args -join ' ') failed: $out" }
    $out
}

if (Invoke-Git status --porcelain) { throw 'working tree is not clean; commit or stash first' }
$branch = Invoke-Git rev-parse --abbrev-ref HEAD
if ($branch -ne 'main') { throw "on '$branch', not main" }

Invoke-Git fetch --quiet --tags origin main | Out-Null
$head = Invoke-Git rev-parse HEAD
$remote = Invoke-Git rev-parse origin/main
if ($head -ne $remote) { throw 'HEAD is not origin/main; push (or pull) first so the tag names a commit CI has seen' }

$latest = Invoke-Git tag --list 'v*' --sort=-v:refname |
    Where-Object { $_ -match '^v\d+\.\d+\.\d+$' } | Select-Object -First 1
if (-not $latest) { $latest = 'v0.0.0' }
$at = @(Invoke-Git tag --points-at HEAD) -match '^v\d+\.\d+\.\d+$'
if ($at) { throw "HEAD is already tagged $($at -join ', ')" }

$v = [version]$latest.Substring(1)
$next = switch ($Bump) {
    'major' { 'v{0}.0.0' -f ($v.Major + 1) }
    'minor' { 'v{0}.{1}.0' -f $v.Major, ($v.Minor + 1) }
    'patch' { 'v{0}.{1}.{2}' -f $v.Major, $v.Minor, ($v.Build + 1) }
}
if (-not $Message) { $Message = "${next}: $(Invoke-Git log -1 --format=%s)" }

Write-Host "$latest -> $next on $($head.Substring(0, 7))"
Write-Host "message: $Message"
if ($DryRun) { Write-Host 'dry run: nothing tagged or pushed'; return }

Invoke-Git tag -a $next -m $Message | Out-Null
try {
    Invoke-Git push --quiet origin $next | Out-Null
} catch {
    Invoke-Git tag -d $next | Out-Null   # not published, so don't leave it to confuse the next run
    throw
}
Write-Host "pushed $next; release.yml is building it"
$url = (Invoke-Git remote get-url origin) -replace '^git@github\.com:', 'https://github.com/' -replace '\.git$', ''
if ($url -match '^https://github\.com/') { Write-Host "$url/actions/workflows/release.yml" }
