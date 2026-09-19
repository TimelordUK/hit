#Requires -Modules @{ ModuleName = 'Pester'; ModuleVersion = '5.0.0' }

# End-to-end through the real `hit init pwsh` output, inside this process, with PSReadLine
# loaded and stand-ins for the tools already in the owner's profile: a starship-like prompt
# that shows the last status, and an mcfly-like AddToHistoryHandler.
# Keys are not pressed here (that is the pty smoke test, S-017); the hooks are invoked
# exactly as PSReadLine and the host invoke them.

BeforeAll {
    Import-Module PSReadLine
    $script:Root = (Resolve-Path (Join-Path $PSScriptRoot '..' '..')).Path
    $script:Exe = Join-Path $TestDrive ($IsWindows ? 'hit.exe' : 'hit')
    Push-Location $script:Root
    try { go build -o $script:Exe ./cmd/hit; if ($LASTEXITCODE) { throw 'go build failed' } }
    finally { Pop-Location }

    $script:SavedPrompt = $function:global:prompt
    $script:SavedHandler = (Get-PSReadLineOption).AddToHistoryHandler

    function Read-HitHistory {
        if (-not (Test-Path $script:Hist)) { return @() }
        @(Get-Content -LiteralPath $script:Hist | ForEach-Object { ConvertFrom-Json $_ -DateKind String })
    }
    # What PSReadLine does on Enter.
    function Send-Line([string]$Line) { (Get-PSReadLineOption).AddToHistoryHandler.Invoke($Line) }
    # What the host does before drawing the prompt. $Fail simulates the command's outcome.
    function Show-Prompt([switch]$Fail) {
        if ($Fail) { Get-Item -LiteralPath 'no such path' -ErrorAction SilentlyContinue }
        else { $null = 1 }
        prompt
    }
}

AfterAll {
    Get-Module hit | Remove-Module
    $function:global:prompt = $script:SavedPrompt
    Set-PSReadLineOption -AddToHistoryHandler $script:SavedHandler
    $env:HIT_DATA_DIR = $null
}

Describe 'hit init pwsh' {
    BeforeEach {
        $env:HIT_DATA_DIR = Join-Path $TestDrive "data-$([guid]::NewGuid())"
        $script:Hist = Join-Path $env:HIT_DATA_DIR 'history.jsonl'
        Get-Module hit | Remove-Module

        $global:StarshipSaw = @()
        $function:global:prompt = { $global:StarshipSaw += $global:?; 'starship> ' }
        $global:McflySaw = @()
        Set-PSReadLineOption -AddToHistoryHandler { param([string]$l) $global:McflySaw += $l; $true }

        Invoke-Expression (& $script:Exe init pwsh | Out-String)
    }

    It 'records a multi-line command verbatim with its cwd, then its end' {
        $cmd = "Invoke-RestMethod ``n    -Uri https://elastic-prod-1:9200/_search ``n    -Method Post"
        Send-Line $cmd | Should -BeTrue
        Show-Prompt | Should -Be 'starship> '
        $r = Read-HitHistory
        $r.Count | Should -Be 2
        $r[0].k | Should -Be 'cmd'
        $r[0].cmd | Should -BeExactly $cmd
        $r[0].cwd | Should -Be (Get-Location).ProviderPath
        $r[0].sh | Should -Be 'pwsh'
        $r[0].sid | Should -Match '^[0-9A-HJKMNP-TV-Z]{26}$'
        $r[1].k | Should -Be 'end'
        $r[1].id | Should -Be $r[0].id
        $r[1].exit | Should -Be 0
        $r[1].ms | Should -BeGreaterOrEqual 0
    }

    It 'chains the previous handler (mcfly keeps working)' {
        Send-Line 'git status' | Out-Null
        $global:McflySaw | Should -Be @('git status')
    }

    It 'keeps $? intact for the wrapped prompt and records the failure' {
        Send-Line 'Get-Item nope' | Out-Null
        Show-Prompt -Fail | Out-Null
        Send-Line 'ls' | Out-Null
        Show-Prompt | Out-Null
        $global:StarshipSaw | Should -Be @($false, $true)
        (Read-HitHistory | Where-Object k -EQ end).exit | Should -Be @(1, 0)
    }

    It 'records a directory change once, as a clean UNC-style provider path' {
        Show-Prompt | Out-Null  # no change since init: nothing written
        Push-Location $TestDrive
        try {
            Show-Prompt | Out-Null
            Show-Prompt | Out-Null
        } finally { Pop-Location }
        $cd = @(Read-HitHistory | Where-Object k -EQ cd)
        $cd.Count | Should -Be 1
        $cd[0].dir | Should -Be (Get-Item $TestDrive).FullName
        $cd[0].dir | Should -Not -Match '::'
    }

    It 'skips blank and leading-space commands' {
        Send-Line '   ' | Out-Null
        Send-Line ' secret-thing --token x' | Out-Null
        Read-HitHistory | Should -BeNullOrEmpty
    }

    It 'skips what the chained handler marks sensitive (PSReadLine default)' {
        Get-Module hit | Remove-Module
        Set-PSReadLineOption -AddToHistoryHandler $script:SavedHandler
        Invoke-Expression (& $script:Exe init pwsh | Out-String)
        Send-Line "`$token = 'abc123'" | Should -Be 'MemoryOnly'
        Send-Line 'ls' | Should -Be 'MemoryAndFile'
        @(Read-HitHistory).cmd | Should -Be @('ls')
    }

    It 're-hooks when a later init replaces the prompt (e.g. zoxide after hit)' {
        $function:global:prompt = { 'replaced> ' }  # hit's wrapper is gone
        Send-Line 'one' | Out-Null
        Show-Prompt | Out-Null                       # hit not called: no end for 'one'
        Send-Line 'two' | Out-Null                    # hit notices and wraps again
        Show-Prompt | Should -Be 'replaced> '
        $r = Read-HitHistory
        @($r | Where-Object k -EQ cmd).cmd | Should -Be @('one', 'two')
        $end = @($r | Where-Object k -EQ end)
        $end.Count | Should -Be 1
        $end[0].id | Should -Be ($r | Where-Object cmd -EQ 'two').id
    }

    It 'does not loop when another prompt wraps hit and hit re-wraps it' {
        $inner = $function:global:prompt
        $function:global:prompt = { & $inner; $null = 'zoxide hook' }.GetNewClosure()
        Send-Line 'one' | Out-Null
        Show-Prompt | Out-Null
        Send-Line 'two' | Out-Null
        Show-Prompt | Out-Null
        @(Read-HitHistory | Where-Object k -EQ end).Count | Should -Be 2
    }

    It 'takes the handler back, chaining, when a later init replaces it' {
        Set-PSReadLineOption -AddToHistoryHandler { param([string]$l) $global:McflySaw += "late:$l"; $true }
        Show-Prompt | Out-Null  # hit notices at the prompt
        Send-Line 'after' | Out-Null
        $global:McflySaw | Should -Contain 'late:after'
        @(Read-HitHistory | Where-Object k -EQ cmd).cmd | Should -Be @('after')
    }

    It 'does not recurse when the other handler chains to hit' {
        $hitHandler = (Get-PSReadLineOption).AddToHistoryHandler
        Set-PSReadLineOption -AddToHistoryHandler { param([string]$l) $hitHandler.Invoke($l) }.GetNewClosure()
        Show-Prompt | Out-Null  # hit re-registers, chaining a handler that calls hit
        Send-Line 'x' | Out-Null
        @(Read-HitHistory | Where-Object k -EQ cmd).cmd | Should -Be @('x')
    }

    It 'Disable-Hit restores the prompt and handler and stops recording' {
        Disable-Hit
        prompt | Should -Be 'starship> '
        Send-Line 'ignored' | Out-Null
        $global:McflySaw | Should -Be @('ignored')
        Read-HitHistory | Should -BeNullOrEmpty
    }
}
