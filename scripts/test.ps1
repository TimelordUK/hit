# Runs everything CI runs, locally: go vet, go test, a short fuzz pass, Pester.
#   ./scripts/test.ps1            # all
#   ./scripts/test.ps1 -Fuzz 0    # skip fuzzing
param(
    [int]$Fuzz = 10  # seconds per fuzz target; 0 to skip
)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
Push-Location $root
try {
    go vet ./...
    if ($LASTEXITCODE) { throw 'go vet failed' }
    go test ./...
    if ($LASTEXITCODE) { throw 'go test failed' }

    if ($Fuzz -gt 0) {
        $targets = @(
            @('./internal/record', 'FuzzCommandRoundTrip'),
            @('./internal/store', 'FuzzRead')
        )
        foreach ($t in $targets) {
            go test $t[0] -run '^$' -fuzz "^$($t[1])$" -fuzztime "$($Fuzz)s"
            if ($LASTEXITCODE) { throw "fuzz $($t[1]) failed" }
        }
    }

    Import-Module Pester -MinimumVersion 5.0.0
    $cfg = New-PesterConfiguration
    $cfg.Run.Path = Join-Path $root 'tests' 'pwsh'
    $cfg.Run.Exit = $false
    $cfg.Run.PassThru = $true
    $cfg.Output.Verbosity = 'Normal'
    $result = Invoke-Pester -Configuration $cfg
    if ($result.FailedCount -gt 0) { throw "Pester: $($result.FailedCount) failed" }
} finally {
    Pop-Location
}
