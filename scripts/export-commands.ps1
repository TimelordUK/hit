# Pulls matching commands out of hit's history into a script: the stopgap for S-037
# (Export-HitScript) until that exists. One line per distinct command, successful runs only.
#   ./scripts/export-commands.ps1                                  # install-module, printed
#   ./scripts/export-commands.ps1 -Path scripts/profile/Modules.ps1  # written to a file
#   ./scripts/export-commands.ps1 -Query "'git clone" -Recent       # another search, newest first
#   ./scripts/export-commands.ps1 -Query '' -Category devops        # a whole category
#
# A leading ' matches literally ('install-module won't also find Get-InstalledModule).
# -Path overwrites the file; review the printed output first.
param(
    [string]$Query = "'install-module",
    [string]$Category,
    [string]$Path,
    [switch]$Recent,       # newest first instead of ranked
    [switch]$IncludeFailed
)
$ErrorActionPreference = 'Stop'

$hitArgs = @('search', '--print', '--query', $Query)
if (-not $IncludeFailed) { $hitArgs += '--ok-only' }
if ($Recent) { $hitArgs += @('--sort', 'recent') }
if ($Category) { $hitArgs += @('--category', $Category) }

$cmds = @(& hit @hitArgs | ConvertFrom-Json | ForEach-Object cmd)
if ($LASTEXITCODE) { throw "hit search failed ($LASTEXITCODE)" }

if ($Path) {
    Set-Content -LiteralPath $Path -Value $cmds
    Write-Host "wrote $($cmds.Count) commands to $Path"
} else {
    $cmds
}
