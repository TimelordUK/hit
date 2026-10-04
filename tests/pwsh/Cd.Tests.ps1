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
    # An interactive shell's default. scripts/test.ps1 runs under Stop, where cd's error
    # is thrown rather than written; that case has its own test below.
    BeforeAll { $ErrorActionPreference = 'Continue' }
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
        $err.CategoryInfo.Activity | Should -Be 'Set-Location'  # what the error view labels it with
        (Get-Location).ProviderPath | Should -Be (Join-Path $root 'trd-platform')
    }

    # F-033: the error is reported where you typed cd, not at a line inside hit's module
    # (`Line 1063 | Set-Location @args`, owner 2026-10-04).
    It 'reports the fallback error at the cd you typed' {
        Use-FakeCd @{ action = 'none' }
        $err = Invoke-HitCd nowhere-at-all 2>&1
        $err.Exception.Message | Should -BeLike "Cannot find path*nowhere-at-all*"
        $err.CategoryInfo.Category | Should -Be 'ObjectNotFound'
        $err.InvocationInfo.Line | Should -BeLike '*Invoke-HitCd nowhere-at-all*'
    }

    It 'reports a real path that fails at the cd you typed too' {
        Use-FakeCd $null
        $err = Invoke-HitCd .\not-here 2>&1
        $err.Exception.Message | Should -BeLike "Cannot find path*not-here*"
        $err.InvocationInfo.Line | Should -BeLike '*Invoke-HitCd .\not-here*'
    }

    It 'throws under Stop, as Set-Location would' {
        Use-FakeCd @{ action = 'none' }
        $ErrorActionPreference = 'Stop'
        { Invoke-HitCd nowhere-at-all } | Should -Throw -ExceptionType ([System.Management.Automation.ItemNotFoundException])
        (Get-Location).ProviderPath | Should -Be (Join-Path $root 'trd-platform')
    }

    It 'names a match on the server name' {
        Use-FakeCd @{ action = 'jump'; cwd = (Join-Path $root 'trd-platform' 'data'); step = 'anywhere'; byServer = $true }
        $said = Invoke-HitCd adp1 6>&1
        "$said" | Should -Be "$([char]0x2192) $(Join-Path $root 'trd-platform' 'data') (anywhere, server)"
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

Describe 'Test-HitRealPath' {
    # On sight, never by looking: none of these exist, and a share may not even answer.
    # A stripped backslash once made `[\\/]` mean `/` only, and `.\x` and `\\srv\share`
    # passed only when Test-Path happened to find them (2026-10-04).
    It 'takes <Path> on sight' -ForEach @(
        @{ Path = '.\not-here' }, @{ Path = '..\not-here' }, @{ Path = '~\not-here' }
        @{ Path = './not-here' }, @{ Path = '\\no-such-server\share' }, @{ Path = '\not-here' }
        @{ Path = 'Z:\not-here' }, @{ Path = 'HKLM:\SOFTWARE' }
    ) {
        Test-HitRealPath $Path | Should -BeTrue
    }

    It 'leaves a bare word that is not here to hit' {
        Test-HitRealPath 'not-here-either' | Should -BeFalse
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
