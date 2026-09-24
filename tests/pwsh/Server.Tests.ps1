#Requires -Modules @{ ModuleName = 'Pester'; ModuleVersion = '5.0.0' }

# The resident finder's shell half (C-031). What matters most here is not that the fast
# path works — it is that every way it can fail still leaves you with a working Ctrl+R.

BeforeAll {
    . (Join-Path $PSScriptRoot '..' '..' 'shell' 'pwsh' 'hit.ps1')

    $script:HitSessionId = 'SESSION1'
    $script:HitHostName = 'BOX1'

    function Use-FakeEditor([string]$Buffer = '') {
        $script:Editor = [pscustomobject]@{ Buffer = $Buffer; Redraws = 0 }
        $fake = $script:Editor
        $script:HitEditor = @{
            GetBuffer = { $fake.Buffer }.GetNewClosure()
            SetBuffer = { param([string]$T) $fake.Buffer = $T }.GetNewClosure()
            Redraw    = { $fake.Redraws++ }.GetNewClosure()
        }
    }

    # Counts cold spawns so a test can prove the fallback ran.
    function Use-CountingRunner([hashtable]$Choice) {
        $script:Runner = [pscustomobject]@{ Calls = 0 }
        $captured = $script:Runner
        $script:HitRunner = {
            param([string[]]$Arguments)
            $captured.Calls++
            $out = $Arguments[[array]::IndexOf($Arguments, '--out') + 1]
            ($Choice | ConvertTo-Json -Compress) | Set-Content -LiteralPath $out -NoNewline
            0
        }.GetNewClosure()
    }
}

Describe 'Get-HitPipeName' {
    # Pinned to the same vectors as the Go side's ipc.Name, so the two cannot drift into
    # naming different endpoints and silently never meeting.
    It 'agrees with the Go implementation' {
        Get-HitPipeName -Session '01J8Z4KABC' | Should -Be 'hit-01j8z4kabc'
        Get-HitPipeName -Session '' | Should -Be 'hit-default'
        Get-HitPipeName -Session '../../etc/passwd' | Should -Be 'hit-etcpasswd'
        Get-HitPipeName -Session 'a\b/c' | Should -Be 'hit-abc'
    }

    It 'caps the length so the name is always a legal endpoint' {
        (Get-HitPipeName -Session ('A' * 200)).Length | Should -BeLessOrEqual 52
    }
}

Describe 'Server mode is opt-in' {
    AfterEach { Disable-HitServer }

    It 'is off unless asked for, so nothing changes for anyone who has not opted in' {
        $script:HitServerMode | Should -BeFalse
    }

    It 'can be turned on and off again at a prompt, with no restart' {
        Enable-HitServer
        $script:HitServerMode | Should -BeTrue
        Disable-HitServer
        $script:HitServerMode | Should -BeFalse
    }

    It 'takes an idle period' {
        Enable-HitServer -Idle '5m'
        $script:HitServerIdle | Should -Be '5m'
    }
}

Describe 'Falling back to a cold spawn' {
    AfterEach { Disable-HitServer }

    It 'spawns when server mode is off' {
        Use-FakeEditor -Buffer 'git st'
        Use-CountingRunner @{ action = 'insert'; cmd = 'git status' }
        Invoke-HitFinder
        $script:Runner.Calls | Should -Be 1
        $script:Editor.Buffer | Should -Be 'git status'
    }

    # The important one: server mode on, but nothing listening. This must not be a
    # failure the user ever sees.
    It 'spawns when server mode is on but no server is listening' {
        Enable-HitServer
        $script:HitServerStarted = $true   # don't actually start one in a test
        Use-FakeEditor -Buffer 'git st'
        Use-CountingRunner @{ action = 'insert'; cmd = 'git status' }
        Invoke-HitFinder
        $script:Runner.Calls | Should -Be 1
        $script:Editor.Buffer | Should -Be 'git status'
    }

    It 'still redraws the prompt when it falls back' {
        Enable-HitServer
        $script:HitServerStarted = $true
        Use-FakeEditor -Buffer 'x'
        Use-CountingRunner @{ action = 'cancel' }
        Invoke-HitFinder
        $script:Editor.Redraws | Should -Be 1
    }
}

Describe 'Test-HitServer' {
    It 'reports no server rather than throwing when the pipe is not there' {
        $script:HitSessionId = 'NOSUCHSESSION' + (Get-Random)
        { Test-HitServer } | Should -Not -Throw
        Test-HitServer | Should -BeFalse
    }

    It 'gives up quickly instead of holding the prompt' {
        $script:HitSessionId = 'NOSUCHSESSION' + (Get-Random)
        $ms = (Measure-Command { Test-HitServer }).TotalMilliseconds
        $ms | Should -BeLessThan 3000
    }
}

Describe 'Invoke-HitFinderOnServer' {
    It 'returns nothing, rather than throwing, when there is no server' {
        $script:HitSessionId = 'NOSUCHSESSION' + (Get-Random)
        $script:HitScope = 'all'
        { Invoke-HitFinderOnServer 'git' } | Should -Not -Throw
        Invoke-HitFinderOnServer 'git' | Should -BeNullOrEmpty
    }
}

Describe 'Get-HitStatus' {
    AfterEach { Disable-HitServer; $script:HitVersion = $null }

    It 'says nothing is answering, rather than throwing, when there is no server' {
        $script:HitSessionId = 'NOSUCHSESSION' + (Get-Random)
        Enable-HitServer
        $s = Get-HitStatus
        $s.Answering | Should -BeFalse
        $s.ServerMode | Should -BeTrue
        $s.Pid | Should -BeNullOrEmpty
        $s.Pipe | Should -Match ([regex]::Escape((Get-HitPipeName)) + '$')
    }

    It 'names the process that answered, and whether it belongs to this shell' {
        $script:HitSessionId = 'SESSION1'
        $script:HitVersion = 'v2'
        $me = $PID
        Mock Invoke-HitServerRequest {
            [pscustomobject]@{ pong = $true; version = 'v2'; server = [pscustomobject]@{
                pid = 4242; parent = $me; requests = 3; exe = 'C:\bin\hit.exe' } }
        }
        $s = Get-HitStatus
        $s.Answering | Should -BeTrue
        $s.Pid | Should -Be 4242
        $s.ParentIsUs | Should -BeTrue
        $s.Requests | Should -Be 3
        $s.Stale | Should -BeFalse
    }

    # The S-031 case: a server started before the last install is still answering Ctrl+R.
    It 'flags a server running a different version from this module' {
        $script:HitSessionId = 'SESSION1'
        $script:HitVersion = 'v2'
        Mock Invoke-HitServerRequest {
            [pscustomobject]@{ pong = $true; version = 'v1'; server = [pscustomobject]@{ pid = 1; parent = 2 } }
        }
        $s = Get-HitStatus
        $s.Stale | Should -BeTrue
        $s.ParentIsUs | Should -BeFalse
    }
}
