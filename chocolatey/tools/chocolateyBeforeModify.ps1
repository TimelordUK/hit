$ErrorActionPreference = 'Stop'

# Runs before an upgrade or uninstall. A shell with server mode on keeps a `hit serve`
# process running this package's hit.exe, which locks the file so it cannot be replaced
# or removed. Stop those servers first. That is safe by design: every shell falls back to
# starting the finder directly, and the next Ctrl+R starts a fresh server on the new
# version (DESIGN §16). Only servers running from this package are touched.
$toolsDir = Split-Path -Parent $MyInvocation.MyCommand.Definition

Get-CimInstance Win32_Process -Filter "Name='hit.exe'" -ErrorAction SilentlyContinue |
  Where-Object {
    $_.ExecutablePath -and $_.ExecutablePath.StartsWith($toolsDir, [StringComparison]::OrdinalIgnoreCase) -and
    $_.CommandLine -match '\sserve(\s|$)'
  } |
  ForEach-Object {
    Write-Host "Stopping hit serve (pid $($_.ProcessId)) so hit.exe can be replaced"
    Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue
  }
