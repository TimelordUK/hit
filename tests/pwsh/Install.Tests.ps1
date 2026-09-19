#Requires -Modules @{ ModuleName = 'Pester'; ModuleVersion = '5.0.0' }

# scripts/install.ps1 against temp GOBIN and HIT_DATA_DIR: never the real binary or history.

BeforeAll {
    $script:Install = Join-Path $PSScriptRoot '..' '..' 'scripts' 'install.ps1'
    $script:SavedGobin = $env:GOBIN
}

AfterAll {
    $env:GOBIN = $script:SavedGobin
    $env:HIT_DATA_DIR = $null
}

Describe 'install.ps1' {
    BeforeEach {
        $env:GOBIN = Join-Path $TestDrive "bin-$([guid]::NewGuid())"
        $env:HIT_DATA_DIR = Join-Path $TestDrive "data-$([guid]::NewGuid())"
        $script:Hist = Join-Path $env:HIT_DATA_DIR 'history.jsonl'
        $null = New-Item -ItemType Directory -Path $env:HIT_DATA_DIR
        Set-Content -LiteralPath $script:Hist -Value @(
            '{"k":"cmd","id":"01K5HQ8ZJ2A7Q3M8V4W6X9Y0ZC","cmd":"one"}'
            '{"k":"end","id":"01K5HQ8ZJ2A7Q3M8V4W6X9Y0ZC","exit":0}'
            '{"k":"cmd","id":"01K5HQ8ZJ2A7Q3M8V4W6X9Y0ZD","cmd":"two"}'
        )
    }

    It 'installs into GOBIN and keeps history without -Clear' {
        & $script:Install 6>$null
        Join-Path $env:GOBIN ($IsWindows ? 'hit.exe' : 'hit') | Should -Exist
        $script:Hist | Should -Exist
    }

    It '-Clear moves the history into backup\ rather than deleting it' {
        $out = & $script:Install -Clear 6>&1 | Out-String
        $script:Hist | Should -Not -Exist
        $backups = @(Get-ChildItem (Join-Path $env:HIT_DATA_DIR 'backup') -Filter 'history-*.jsonl')
        $backups.Count | Should -Be 1
        @(Get-Content $backups[0].FullName).Count | Should -Be 3
        $out | Should -Match '2 commands moved'
    }

    It '-Clear with no history is fine' {
        Remove-Item -LiteralPath $script:Hist
        { & $script:Install -Clear 6>$null } | Should -Not -Throw
    }
}
