# Builds the Chocolatey package for an existing GitHub release (DESIGN: publishing to
# Chocolatey is a manual step, never part of tagging, because moderation can take months).
#   ./scripts/choco-pack.ps1 -Tag v0.1.17            # -> dist/choco/hit.0.1.17.nupkg
#   choco install hit -s dist/choco -y --force       # try it locally (admin shell)
#
# The checksum comes from the release's SHA256SUMS.txt and is checked against the zip it
# names, so a package can never point at bytes other than the ones published. Used as-is
# by .github/workflows/chocolatey.yml; pushing is the workflow's job, not this script's.
param(
    [Parameter(Mandatory)][ValidatePattern('^v\d+\.\d+\.\d+$')][string]$Tag,
    [string]$OutDir = (Join-Path (Split-Path $PSScriptRoot -Parent) 'dist\choco'),
    [string]$Repo = 'TimelordUK/hit'
)
$ErrorActionPreference = 'Stop'

$root = Split-Path $PSScriptRoot -Parent
$version = $Tag.Substring(1)
$zip = "hit_${Tag}_windows_amd64.zip"
$base = "https://github.com/$Repo/releases/download/$Tag"

$stage = Join-Path ([IO.Path]::GetTempPath()) "hit-choco-$version"
Remove-Item -LiteralPath $stage -Recurse -Force -ErrorAction SilentlyContinue
Copy-Item -LiteralPath (Join-Path $root 'chocolatey') -Destination $stage -Recurse

# The checksum as published, then the zip it names, hashed here to prove they agree.
$sums = (Invoke-WebRequest -Uri "$base/SHA256SUMS.txt" -UseBasicParsing).Content
if ($sums -is [byte[]]) { $sums = [Text.Encoding]::UTF8.GetString($sums) } # served as octet-stream
$line = ($sums -split "`n") | Where-Object { $_ -match "\s\*?$([regex]::Escape($zip))\s*$" } | Select-Object -First 1
if (-not $line) { throw "$zip is not listed in $base/SHA256SUMS.txt" }
$checksum = ($line -split '\s+')[0].ToUpperInvariant()
$zipPath = Join-Path $stage $zip
Invoke-WebRequest -Uri "$base/$zip" -OutFile $zipPath -UseBasicParsing
$actual = (Get-FileHash -LiteralPath $zipPath -Algorithm SHA256).Hash
if ($actual -ne $checksum) { throw "checksum mismatch for ${zip}: SHA256SUMS says $checksum, the file is $actual" }
Remove-Item -LiteralPath $zipPath
Write-Host "$zip  sha256 $checksum"

# Fill in the templates; the .template files themselves must not ship.
$nuspec = Join-Path $stage 'hit.nuspec'
(Get-Content -LiteralPath "$nuspec.template" -Raw) -replace '__VERSION__', $version |
    Set-Content -LiteralPath $nuspec -NoNewline
$install = Join-Path $stage 'tools\chocolateyInstall.ps1'
(Get-Content -LiteralPath "$install.template" -Raw) -replace '__VERSION__', $version -replace '__CHECKSUM64__', $checksum |
    Set-Content -LiteralPath $install -NoNewline
Get-ChildItem -LiteralPath $stage -Recurse -Filter '*.template' | Remove-Item
Remove-Item -LiteralPath (Join-Path $stage 'README.md') -ErrorAction SilentlyContinue

$null = New-Item -ItemType Directory -Force -Path $OutDir
& choco pack $nuspec --outputdirectory $OutDir
if ($LASTEXITCODE) { throw "choco pack failed ($LASTEXITCODE)" }
$nupkg = Join-Path $OutDir "hit.$version.nupkg"
if (-not (Test-Path -LiteralPath $nupkg)) { throw "expected $nupkg" }
Remove-Item -LiteralPath $stage -Recurse -Force
Write-Host "packed $nupkg"
$nupkg
