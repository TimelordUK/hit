<#
.SYNOPSIS
    Find out what is making process creation slow (C-029, S-026).

.DESCRIPTION
    `HIT_TIMING=1` reports a `spawn` phase: the gap between the shell calling
    Process.Start and the binary running. On a managed machine that can be seconds. This
    tells you *which* of two very different causes it is, because they have different
    fixes:

      - the cost is attached to THIS BINARY (unsigned, unknown reputation, scanned or
        looked up on each launch). Fixable cheaply: ask for the install path to be
        allowlisted, or get the binary signed. A warm process would also hide it.

      - the cost is attached to ALL process creation (an endpoint agent hooking every
        CreateProcess). Nothing about hit can fix that, and allowlisting one path will
        not help. Only spawning less often will.

    It launches a trivial signed Microsoft binary and hit itself, several times each, and
    compares. It starts no services and writes nothing outside your temp folder.

.EXAMPLE
    .\measure-spawn.ps1
    .\measure-spawn.ps1 -Count 15
#>
[CmdletBinding()]
param(
    [int]$Count = 10,
    [string]$HitExe = (Get-Command hit -ErrorAction SilentlyContinue).Source
)

function Measure-Launch {
    param([string]$Label, [scriptblock]$Launch, [int]$Times)
    $ms = for ($i = 0; $i -lt $Times; $i++) {
        (Measure-Command { & $Launch }).TotalMilliseconds
    }
    $sorted = $ms | Sort-Object
    [pscustomobject]@{
        What   = $Label
        First  = [math]::Round($ms[0])
        Min    = [math]::Round($sorted[0])
        Median = [math]::Round($sorted[[int]($sorted.Count / 2)])
        Max    = [math]::Round($sorted[-1])
    }
}

if (-not $HitExe) {
    Write-Error 'hit is not on PATH; pass -HitExe <path>'
    return
}

Write-Host "hit: $HitExe" -ForegroundColor Cyan
Write-Host "signed: $((Get-AuthenticodeSignature $HitExe).Status)" -ForegroundColor Cyan
Write-Host ''

$results = @()

# A signed Microsoft binary that does nothing. If this is slow too, every process on this
# machine is being inspected and the problem is not hit's.
$where = "$env:SystemRoot\System32\where.exe"
$results += Measure-Launch 'where.exe (signed, MS)' { & $where /Q where.exe } $Count

# hit itself, doing the least work it can.
$results += Measure-Launch 'hit version' { & $HitExe version | Out-Null } $Count

# A copy under a new name has no reputation history of its own. If this is much slower
# than the original, launches are being paid for per unknown file.
$copy = Join-Path $env:TEMP ("hit-spawn-probe-{0}.exe" -f (Get-Random))
Copy-Item $HitExe $copy
try {
    $results += Measure-Launch 'hit copied, new name' { & $copy version | Out-Null } $Count
} finally {
    Remove-Item $copy -ErrorAction SilentlyContinue
}

$results | Format-Table -AutoSize

Write-Host 'Reading it:' -ForegroundColor Cyan
Write-Host '  where.exe also slow          -> every CreateProcess is hooked. Only spawning'
Write-Host '                                  less often helps (S-026). Allowlisting will not.'
Write-Host '  only hit slow                -> the cost is on this binary. Ask for the install'
Write-Host '                                  path to be allowlisted, or get it signed.'
Write-Host '  copy much slower than hit    -> per-file reputation, cached after first launch.'
Write-Host '                                  Explains why it is worst after a reinstall.'
Write-Host '  first >> median everywhere   -> a cache that is being evicted. Expect it to'
Write-Host '                                  come back as the day goes on.'
