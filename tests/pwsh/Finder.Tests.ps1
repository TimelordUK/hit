#Requires -Modules @{ ModuleName = 'Pester'; ModuleVersion = '5.0.0' }

# The Ctrl+R handler against a fake line editor and a fake runner: no keyboard, no binary.
# The real binary is covered by the Go tests and the pty smoke test (S-017).

BeforeAll {
    . (Join-Path $PSScriptRoot '..' '..' 'shell' 'pwsh' 'hit.ps1')

    $script:HitSessionId = 'SESSION1'
    $script:HitHostName = 'BOX1'

    # Fake editor: records what the handler does to the prompt buffer.
    function Use-FakeEditor([string]$Buffer = '') {
        $script:Editor = [pscustomobject]@{ Buffer = $Buffer; Redraws = 0 }
        $fake = $script:Editor
        $script:HitEditor = @{
            GetBuffer = { $fake.Buffer }.GetNewClosure()
            SetBuffer = { param([string]$Text) $fake.Buffer = $Text }.GetNewClosure()
            Redraw    = { $fake.Redraws++ }.GetNewClosure()
        }
    }

    # Fake runner: captures the arguments and writes a canned choice to --out.
    function Use-FakeRunner([hashtable]$Choice, [int]$ExitCode = 0, [switch]$WriteNothing) {
        $script:Runner = [pscustomobject]@{ Arguments = $null }
        $captured = $script:Runner
        $script:HitRunner = {
            param([string[]]$Arguments)
            $captured.Arguments = $Arguments
            if (-not $WriteNothing) {
                $out = $Arguments[[array]::IndexOf($Arguments, '--out') + 1]
                ($Choice | ConvertTo-Json -Compress) | Set-Content -LiteralPath $out -NoNewline
            }
            $ExitCode
        }.GetNewClosure()
    }

    function Get-Arg([string]$Name) {
        $i = [array]::IndexOf($script:Runner.Arguments, $Name)
        if ($i -lt 0) { return $null }
        $script:Runner.Arguments[$i + 1]
    }
}

Describe 'Invoke-HitFinder' {
    It 'seeds the search with what is already typed and passes the shell context' {
        Use-FakeEditor -Buffer 'git st'
        Use-FakeRunner @{ action = 'cancel' }
        Invoke-HitFinder
        $script:Runner.Arguments[0] | Should -Be 'search'
        Get-Arg '--query' | Should -Be 'git st'
        Get-Arg '--session' | Should -Be 'SESSION1'
        Get-Arg '--host' | Should -Be 'BOX1'
        Get-Arg '--shell' | Should -Be 'pwsh'
        Get-Arg '--scope' | Should -Be 'all'
        Get-Arg '--cwd' | Should -Be (Get-Location).ProviderPath
    }

    It 'replaces the buffer with the chosen multi-line command, exactly' {
        $cmd = "Invoke-RestMethod ``n    -Uri https://elastic-prod-1:9200/_search ``n    -Method Post"
        Use-FakeEditor -Buffer 'irm'
        Use-FakeRunner @{ action = 'insert'; cmd = $cmd }
        Invoke-HitFinder
        $script:Editor.Buffer | Should -BeExactly $cmd
    }

    It 'leaves the prompt untouched when the finder is cancelled' {
        Use-FakeEditor -Buffer 'half typed'
        Use-FakeRunner @{ action = 'cancel' }
        Invoke-HitFinder
        $script:Editor.Buffer | Should -Be 'half typed'
    }

    It 'redraws the prompt afterwards' {
        Use-FakeEditor
        Use-FakeRunner @{ action = 'cancel' }
        Invoke-HitFinder
        $script:Editor.Redraws | Should -Be 1
    }

    It 'leaves the prompt alone when the binary fails or writes nothing' {
        Use-FakeEditor -Buffer 'typed'
        Use-FakeRunner @{ action = 'insert'; cmd = 'rm -rf /' } -ExitCode 1
        Invoke-HitFinder
        $script:Editor.Buffer | Should -Be 'typed'

        Use-FakeEditor -Buffer 'typed'
        Use-FakeRunner @{ action = 'insert'; cmd = 'x' } -WriteNothing
        Invoke-HitFinder
        $script:Editor.Buffer | Should -Be 'typed'
    }

    It 'never throws into the session when the runner blows up' {
        Use-FakeEditor -Buffer 'typed'
        $script:HitRunner = { param([string[]]$Arguments) throw 'boom' }
        { Invoke-HitFinder } | Should -Not -Throw
        $script:Editor.Buffer | Should -Be 'typed'
        $script:Editor.Redraws | Should -Be 1
    }

    It 'cleans up its temp file' {
        Use-FakeEditor
        $script:Runner = [pscustomobject]@{ Arguments = $null }
        $captured = $script:Runner
        $script:HitRunner = {
            param([string[]]$Arguments)
            $captured.Arguments = $Arguments
            0
        }.GetNewClosure()
        Invoke-HitFinder
        Get-Arg '--out' | Should -Not -Exist
    }
}

Describe 'Key bindings' {
    BeforeAll { Import-Module PSReadLine }

    AfterEach {
        Set-PSReadLineOption -EditMode Windows
        Set-PSReadLineKeyHandler -Chord 'Ctrl+r' -Function ReverseSearchHistory
    }

    It 'binds Ctrl+R in Windows edit mode and gives it back on removal' {
        Set-PSReadLineOption -EditMode Windows
        Register-HitKeyHandlers
        (Get-PSReadLineKeyHandler -Chord 'Ctrl+r').Function | Should -Be 'HitFinder'
        Remove-HitKeyHandlers
        (Get-PSReadLineKeyHandler -Chord 'Ctrl+r').Function | Should -Be 'ReverseSearchHistory'
    }

    # The owner runs `Set-PSReadLineOption -EditMode vi`, where a chord is bound per vi mode:
    # in vi mode Get-PSReadLineKeyHandler reports one entry per mode, so both must be ours.
    It 'binds Ctrl+R in both vi modes' {
        Set-PSReadLineOption -EditMode Vi
        Register-HitKeyHandlers
        $bound = @(Get-PSReadLineKeyHandler -Chord 'Ctrl+r')
        $bound.Count | Should -Be 2
        $bound.Function | Should -Be @('HitFinder', 'HitFinder')
        Remove-HitKeyHandlers
        @(Get-PSReadLineKeyHandler -Chord 'Ctrl+r').Function |
            Should -Be @('ReverseSearchHistory', 'ReverseSearchHistory')
    }
}
