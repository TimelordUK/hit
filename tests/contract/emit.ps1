# Contract writer for pwsh: turns each case in testdata/contract/cases.json into a record
# with the real pwsh functions and appends it with the real appender.
# Driven by internal/contract/contract_test.go; not meant to be run by hand.
param(
    [Parameter(Mandatory)][string]$CasesPath,
    [Parameter(Mandatory)][string]$OutPath
)
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot '..' '..' 'shell' 'pwsh' 'hit.ps1')

# System.Text.Json, not ConvertFrom-Json: the latter turns timestamps into DateTimes.
$doc = [System.Text.Json.JsonDocument]::Parse([System.IO.File]::ReadAllText($CasesPath))
foreach ($case in $doc.RootElement.EnumerateArray()) {
    $r = $case.GetProperty('record')
    $get = {
        param($name)
        $v = [System.Text.Json.JsonElement]::new()
        if ($r.TryGetProperty($name, [ref]$v)) { $v } else { $null }
    }
    $p = @{ Kind = (& $get 'k').GetString() }
    foreach ($pair in @(
            @('id', 'Id'), @('ts', 'Timestamp'), @('cmd', 'Command'), @('cwd', 'Cwd'), @('dir', 'Dir'),
            @('sh', 'Shell'), @('host', 'HostName'), @('sid', 'SessionId'))) {
        $v = & $get $pair[0]
        if ($null -ne $v) { $p[$pair[1]] = $v.GetString() }
    }
    if ($null -ne ($v = & $get 'exit')) { $p.ExitCode = $v.GetInt32() }
    if ($null -ne ($v = & $get 'ms')) { $p.DurationMs = $v.GetInt64() }

    $generate = [System.Text.Json.JsonElement]::new()
    if ($case.TryGetProperty('generate', [ref]$generate) -and $generate.GetBoolean()) {
        $now = Get-HitNow
        $p.Id = New-HitId $now
        $p.Timestamp = ConvertTo-HitTimestamp $now
    }

    $line = ConvertTo-HitJsonLine (New-HitRecord @p)
    if (-not (Add-HitLine -Path $OutPath -Line $line)) { throw "Add-HitLine failed for '$($case.GetProperty('name'))'" }
}
