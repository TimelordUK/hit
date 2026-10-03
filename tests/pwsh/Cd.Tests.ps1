#Requires -Modules @{ ModuleName = 'Pester'; ModuleVersion = '5.0.0' }

# cd's shell half (F-030). The ranking is Go's and is table-tested there; this is what the
# shell decides on its own: a real path never reaches hit, and anything hit cannot place
# ends in plain Set-Location, so a broken hit never leaves you unable to cd.

BeforeAll {
    . (Join-Path $PSScriptRoot '..' '..' 'shell' 'pwsh' 'hit.ps1')

    $script:HitSessionId = 'SESSION1'
    $script:HitServerMode = $false

    # Answers `hit cd --json` with a canned response, and counts the asks.
    function Use-FakeCd($Answer) {
        $script:Cd = [pscustomobject]@{ Calls = 0; Arguments = $null }
        $captured = $script:Cd
        $script:HitCdRunner = {
            param([string[]]$Arguments)
            $captured.Calls++
            $captured.Arguments = $Arguments
            if ($null -ne $Answer) { $Answer | ConvertTo-Json -Compress }
        }.GetNewClosure()
    }
}

Describe 'Invoke-HitCd' {
    BeforeEach {
        $root = Join-Path ([System.IO.Path]::GetTempPath()) ("hit-cd-" + [guid]::NewGuid())
        $null = New-Item -ItemType Directory -Path (Join-Path $root 'trd-platform' 'logs')
        $null = New-Item -ItemType Directory -Path (Join-Path $root 'trd-platform' 'data' 'logs')
        Push-Location (Join-Path $root 'trd-platform')
    }
    AfterEach {
        Pop-Location
        Remove-Item -LiteralPath $root -Recurse -Force
    }

    It 'goes into a real folder without asking hit, visited or not' {
        Use-FakeCd @{ action = 'jump'; cwd = (Join-Path $root 'trd-platform' 'data' 'logs'); step = 'nearest' }
        Invoke-HitCd logs
        (Get-Location).ProviderPath | Should -Be (Join-Path $root 'trd-platform' 'logs')
        $script:Cd.Calls | Should -Be 0
    }

    It 'takes .., - and rooted paths on sight' {
        Use-FakeCd $null
        Invoke-HitCd ..
        (Get-Location).ProviderPath | Should -Be $root
        Invoke-HitCd (Join-Path $root 'trd-platform' 'data')
        (Get-Location).ProviderPath | Should -Be (Join-Path $root 'trd-platform' 'data')
        $script:Cd.Calls | Should -Be 0
    }

    It 'jumps where hit says, passing the terms and where it stands' {
        $target = Join-Path $root 'trd-platform' 'data' 'logs'
        Use-FakeCd @{ action = 'jump'; cwd = $target; step = 'below'; exact = $true }
        Invoke-HitCd data lo 6>$null
        (Get-Location).ProviderPath | Should -Be $target
        $script:Cd.Arguments | Should -Be @('cd', '--json', '--cwd', (Join-Path $root 'trd-platform'), '--', 'data', 'lo')
    }

    It 'says why it went where it did' {
        $target = Join-Path $root 'trd-platform' 'data' 'logs'
        Use-FakeCd @{ action = 'jump'; cwd = $target; step = 'below'; exact = $true }
        $said = Invoke-HitCd data lo 6>&1
        "$said" | Should -Be "→ $target (below, exact)"
    }

    It 'falls back to Set-Location, and its error, when there is no jump' {
        Use-FakeCd @{ action = 'none' }
        $err = Invoke-HitCd nowhere-at-all 2>&1
        $script:Cd.Calls | Should -Be 1
        $err | Should -BeOfType System.Management.Automation.ErrorRecord
        $err.InvocationInfo.MyCommand.Name | Should -Be 'Set-Location'
        (Get-Location).ProviderPath | Should -Be (Join-Path $root 'trd-platform')
    }

    It 'falls back to Set-Location when hit says nothing at all' {
        Use-FakeCd $null
        Invoke-HitCd nowhere-at-all 2>$null
        $script:Cd.Calls | Should -Be 1
        (Get-Location).ProviderPath | Should -Be (Join-Path $root 'trd-platform')
    }

    It 'passes parameters straight to Set-Location' {
        Use-FakeCd $null
        Invoke-HitCd -LiteralPath (Join-Path $root 'trd-platform' 'data')
        (Get-Location).ProviderPath | Should -Be (Join-Path $root 'trd-platform' 'data')
        $script:Cd.Calls | Should -Be 0
    }

    It 'does not move when the jump target has gone' {
        Use-FakeCd @{ action = 'jump'; cwd = (Join-Path $root 'gone'); step = 'anywhere' }
        $said = Invoke-HitCd gone 2>$null 6>&1
        $script:Cd.Calls | Should -Be 1
        $said | Should -BeNullOrEmpty
        (Get-Location).ProviderPath | Should -Be (Join-Path $root 'trd-platform')
    }
}

Describe 'Register-HitCd' {
    AfterEach { Unregister-HitCd }

    It 'binds cd, and gives it back to Set-Location' {
        Register-HitCd
        (Get-Alias cd -Scope Global).Definition | Should -Be 'Invoke-HitCd'
        Unregister-HitCd
        (Get-Alias cd -Scope Global).Definition | Should -Be 'Set-Location'
    }

    It 'leaves cd alone if something else took it since' {
        Register-HitCd
        Set-Alias -Name cd -Value Get-Location -Option AllScope -Scope Global -Force
        Unregister-HitCd
        (Get-Alias cd -Scope Global).Definition | Should -Be 'Get-Location'
        Set-Alias -Name cd -Value Set-Location -Option AllScope -Scope Global -Force
    }
}
