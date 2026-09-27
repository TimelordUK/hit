# Profile dispatcher, shared by Windows and Linux. Each machine's $PROFILE is just:
#
#   . ~/dev/hit/scripts/profile/profile.ps1
#
# Load order:
#   Common.ps1           everything that is the same on every machine
#   Windows.ps1/Linux.ps1  platform specifics (may override Common)
#   Local.ps1            optional, next to $PROFILE, not checked in: per-machine
#                        settings such as work proxies or paths
#   Init.ps1             starship, zoxide, hit: must come last
#
# These are dot-sourced at script scope (not from a function), so the aliases,
# functions and variables they define land in the session.

# Windows PowerShell 5.1 has no $IsWindows
$platform = if ($PSVersionTable.PSEdition -eq 'Desktop' -or $IsWindows) { 'Windows' } else { 'Linux' }

. (Join-Path $PSScriptRoot 'Common.ps1')
. (Join-Path $PSScriptRoot "$platform.ps1")

$localProfile = Join-Path (Split-Path $PROFILE) 'Local.ps1'
if (Test-Path $localProfile) { . $localProfile }

. (Join-Path $PSScriptRoot 'Init.ps1')

Remove-Variable platform, localProfile
