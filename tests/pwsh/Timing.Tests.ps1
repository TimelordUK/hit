#Requires -Modules @{ ModuleName = 'Pester'; ModuleVersion = '5.0.0' }

# Phase timings for one Ctrl+R (S-024). The point of these is that timing is *off* by
# default and changes nothing when off: the instrumentation must not become the thing
# that slows the hot path down.

BeforeAll {
    # The owner's shell may well have HIT_TIMING on. Start from off, and give it back after.
    $script:SavedHitTiming = $env:HIT_TIMING
    $env:HIT_TIMING = $null

    . (Join-Path $PSScriptRoot '..' '..' 'shell' 'pwsh' 'hit.ps1')

    $script:HitSessionId = 'SESSION1'
    $script:HitHostName = 'BOX1'

    function Use-FakeEditor([string]$Buffer = '') {
        $script:Editor = [pscustomobject]@{ Buffer = $Buffer; Redraws = 0 }
        $fake = $script:Editor
        $script:HitEditor = @{
            GetBuffer = { $fake.Buffer }.GetNewClosure()
            SetBuffer = { param([string]$Text) $fake.Buffer = $Text }.GetNewClosure()
            Redraw    = { $fake.Redraws++ }.GetNewClosure()
        }
    }

    function Use-FakeRunner([hashtable]$Choice) {
        $script:Runner = [pscustomobject]@{ Arguments = $null }
        $captured = $script:Runner
        $script:HitRunner = {
            param([string[]]$Arguments)
            $captured.Arguments = $Arguments
            $out = $Arguments[[array]::IndexOf($Arguments, '--out') + 1]
            ($Choice | ConvertTo-Json -Compress) | Set-Content -LiteralPath $out -NoNewline
            0
        }.GetNewClosure()
    }

    function Get-Arg([string]$Name) {
        $i = [array]::IndexOf($script:Runner.Arguments, $Name)
        if ($i -lt 0) { return $null }
        $script:Runner.Arguments[$i + 1]
    }
}

Describe 'New-HitSearchArguments' {
    It 'leaves --started-at off entirely when timing is not asked for' {
        $a = New-HitSearchArguments -Query 'x' -OutFile 'o' -Scope 'all'
        $a | Should -Not -Contain '--started-at'
    }

    It 'passes the spawn moment so the binary can measure process creation' {
        $a = New-HitSearchArguments -Query 'x' -OutFile 'o' -Scope 'all' -StartedAt 1758448800123
        $a | Should -Contain '--started-at'
        $a[[array]::IndexOf($a, '--started-at') + 1] | Should -Be '1758448800123'
    }
}

Describe 'Invoke-HitFinder timing' {
    AfterEach { $env:HIT_TIMING = $null }

    It 'does not pass --started-at when HIT_TIMING is unset' {
        Use-FakeEditor -Buffer 'git st'
        Use-FakeRunner @{ action = 'cancel' }
        Invoke-HitFinder
        Get-Arg '--started-at' | Should -BeNullOrEmpty
    }

    It 'passes a sane --started-at when HIT_TIMING is set' {
        $env:HIT_TIMING = '1'
        $before = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
        Use-FakeEditor -Buffer 'git st'
        Use-FakeRunner @{ action = 'cancel' }
        Invoke-HitFinder
        $after = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()

        $started = [long](Get-Arg '--started-at')
        $started | Should -BeGreaterOrEqual $before
        $started | Should -BeLessOrEqual $after
    }

    It 'still returns the chosen command with timing on' {
        $env:HIT_TIMING = '1'
        Use-FakeEditor -Buffer 'git st'
        Use-FakeRunner @{ action = 'insert'; cmd = 'git status' }
        Invoke-HitFinder
        $script:Editor.Buffer | Should -Be 'git status'
    }
}

Describe 'Format-HitTimeline' {
    AfterEach { $env:HIT_TIMING = $null }

    It 'reports nothing when timing is off' {
        New-HitTimeline | Should -BeNullOrEmpty
        Format-HitTimeline $null | Should -BeNullOrEmpty
    }

    It 'marking a null timeline is a no-op, so the off path carries no checks' {
        { Add-HitMark $null 'temp' } | Should -Not -Throw
    }

    It 'names every phase and ends with a total' {
        $env:HIT_TIMING = '1'
        $t = New-HitTimeline
        $t | Should -Not -BeNullOrEmpty
        Add-HitMark $t 'temp'
        Add-HitMark $t 'run'
        $report = Format-HitTimeline $t
        $report | Should -Match 'temp \d+'
        $report | Should -Match 'run \d+'
        $report | Should -Match 'total \d+ ms$'
    }

    It 'reports an empty timeline as nothing rather than a bare total' {
        $env:HIT_TIMING = '1'
        Format-HitTimeline (New-HitTimeline) | Should -BeNullOrEmpty
    }
}

AfterAll { $env:HIT_TIMING = $script:SavedHitTiming }
