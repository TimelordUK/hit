#Requires -Modules @{ ModuleName = 'Pester'; ModuleVersion = '5.0.0' }

# Up/Down at the prompt (S-009), against a fake line editor and a fake source of matches:
# no keyboard, no PSReadLine history, no server.

BeforeAll {
    . (Join-Path $PSScriptRoot '..' '..' 'shell' 'pwsh' 'hit.ps1')

    $script:HitSessionId = 'SESSION1'

    # Fake editor. The cursor goes to the end on every SetBuffer, as Insert leaves it.
    function Use-FakeEditor([string]$Buffer = '', [int]$Cursor = -1) {
        $script:Editor = [pscustomobject]@{
            Buffer = $Buffer; Cursor = $(if ($Cursor -lt 0) { $Buffer.Length } else { $Cursor })
            Dings = 0; LinesUp = 0; LinesDown = 0
        }
        $fake = $script:Editor
        $script:HitEditor = @{
            GetBuffer    = { $fake.Buffer }.GetNewClosure()
            GetCursor    = { $fake.Cursor }.GetNewClosure()
            SetBuffer    = { param([string]$Text) $fake.Buffer = $Text; $fake.Cursor = $Text.Length }.GetNewClosure()
            Redraw       = { }
            Ding         = { $fake.Dings++ }.GetNewClosure()
            PreviousLine = { $fake.LinesUp++ }.GetNewClosure()
            NextLine     = { $fake.LinesDown++ }.GetNewClosure()
        }
    }

    # Fake source: Select-HitUpItems over a fixed history, oldest first, counting asks. Not
    # a closure: GetNewClosure's module scope cannot see the dot-sourced functions.
    function Use-FakeHistory([string[]]$Lines) {
        $script:FakeLines = $Lines
        $script:Asks = [System.Collections.Generic.List[string]]::new()
        $script:HitUpSource = {
            param([string]$Text)
            $script:Asks.Add($Text)
            Select-HitUpItems $script:FakeLines $Text
        }
    }

    function Up { Invoke-HitUp 1 }
    function Down { Invoke-HitUp -1 }
}

Describe 'Up/Down at the prompt (S-009)' {
    BeforeEach {
        $script:HitUpWalk = $null
        Use-FakeHistory @('git status', 'ls', 'jq ''.[] | def''', 'git log', 'git status')
    }

    Describe 'Select-HitUpItems' {
        It 'is a case-insensitive substring match, newest first, each command once' {
            Select-HitUpItems @('git status', 'ls', 'Git log', 'git status') 'GIT' |
                Should -Be @('git status', 'Git log')
        }

        It 'is everything on a blank line, without blanks' {
            Select-HitUpItems @('a', '', 'b', 'a') '' | Should -Be @('a', 'b')
        }

        It 'leaves out the command already typed' {
            @(Select-HitUpItems @('git status', 'git log') 'git status ').Count | Should -Be 0
        }

        It 'finds text in the middle, which a prefix search would not' {
            Select-HitUpItems @('jq ''.[] | def''', 'def x') '| def' | Should -Be @('jq ''.[] | def''')
        }
    }

    Describe 'Up on an empty line' {
        It 'steps back through history as plain Up does' {
            Use-FakeEditor
            Up; $script:Editor.Buffer | Should -Be 'git status'
            Up; $script:Editor.Buffer | Should -Be 'git log'
            Up; $script:Editor.Buffer | Should -Be 'jq ''.[] | def'''
        }

        It 'asks once per walk, not once per press' {
            Use-FakeEditor
            Up; Up; Up
            $script:Asks.Count | Should -Be 1
        }
    }

    Describe 'Up with text on the line' {
        It 'walks only the commands containing it, newest first' {
            Use-FakeEditor 'git'
            Up; $script:Editor.Buffer | Should -Be 'git status'
            Up; $script:Editor.Buffer | Should -Be 'git log'
        }

        It 'stays on the oldest match and dings past it' {
            Use-FakeEditor 'def'
            Up; Up
            $script:Editor.Buffer | Should -Be 'jq ''.[] | def'''
            $script:Editor.Dings | Should -Be 1
        }

        It 'leaves the line alone and dings when nothing matches' {
            Use-FakeEditor 'zzz'
            Up
            $script:Editor.Buffer | Should -Be 'zzz'
            $script:Editor.Dings | Should -Be 1
        }

        It 'searches again on the edited text when a recalled line is changed' {
            Use-FakeEditor 'git'
            Up
            $script:Editor.Buffer = 'ls'; $script:Editor.Cursor = 2
            Up
            $script:Asks | Should -Be @('git', 'ls')
            $script:Editor.Buffer | Should -Be 'ls'  # 'ls' itself is left out: nothing older contains it
            $script:Editor.Dings | Should -Be 1
        }
    }

    Describe 'Down' {
        It 'walks back towards the newest, then gives back what was typed' {
            Use-FakeEditor 'git'
            Up; Up
            Down; $script:Editor.Buffer | Should -Be 'git status'
            Down; $script:Editor.Buffer | Should -Be 'git'
        }

        It 'does nothing when not walking' {
            Use-FakeEditor 'git'
            Down
            $script:Editor.Buffer | Should -Be 'git'
            $script:Asks.Count | Should -Be 0
        }

        It 'does nothing once the recalled line has been edited' {
            Use-FakeEditor 'git'
            Up
            $script:Editor.Buffer = 'git status -s'
            Down
            $script:Editor.Buffer | Should -Be 'git status -s'
        }
    }

    Describe 'A new prompt' {
        It 'ends the walk, so the next Up asks again and sees the command just run' {
            Use-FakeEditor
            Up
            Reset-HitUpWalk
            Use-FakeEditor
            Up
            $script:Asks.Count | Should -Be 2
        }
    }

    Describe 'Multi-line commands' {
        It 'moves up a line while editing one, instead of recalling' {
            Use-FakeEditor "Get-Thing ```n    -Name x" -Cursor 18
            Up
            $script:Editor.LinesUp | Should -Be 1
            $script:Asks.Count | Should -Be 0
        }

        It 'moves down a line while editing one, instead of recalling' {
            Use-FakeEditor "Get-Thing ```n    -Name x" -Cursor 3
            Down
            $script:Editor.LinesDown | Should -Be 1
        }

        It 'recalls from the first line' {
            Use-FakeEditor "git ```n  x" -Cursor 2
            Up
            $script:Asks.Count | Should -Be 1
        }

        It 'keeps walking past a recalled multi-line command, wherever the cursor is' {
            Use-FakeHistory @('git one', "git two ```n  -x", 'other')
            Use-FakeEditor 'git'
            Up; $script:Editor.Buffer | Should -Be "git two ```n  -x"
            Up; $script:Editor.Buffer | Should -Be 'git one'
            $script:Editor.LinesUp | Should -Be 0
        }
    }

    Describe 'Where the matches come from' {
        BeforeEach {
            $script:HitServerMode = $false
            $script:HitVersion = 'v1'
        }

        It 'uses the server when server mode is on and it is this version' {
            $script:HitServerMode = $true
            Mock Invoke-HitServerRequest {
                if ($Request.ping) { return [pscustomobject]@{ pong = $true; version = 'v1' } }
                [pscustomobject]@{ action = 'recall'; cmds = @('from server') }
            }
            Get-HitUpItems 'x' | Should -Be @('from server')
            Should -Invoke Invoke-HitServerRequest -ParameterFilter { $null -ne $Request.up -and $Request.up -eq 'x' -and $Request.session -eq 'SESSION1' -and $Request.shell -eq 'pwsh' }
        }

        It 'treats a served empty answer as no matches, not as one blank match' {
            $script:HitServerMode = $true
            Mock Invoke-HitServerRequest {
                if ($Request.ping) { return [pscustomobject]@{ pong = $true; version = 'v1' } }
                [pscustomobject]@{ action = 'recall' }
            }
            Mock Get-HitPSReadLineHistory { @('local zzz') }
            @(Get-HitUpItems 'zzz').Count | Should -Be 0
        }

        It 'never asks a server of another version, which would draw the finder instead' {
            $script:HitServerMode = $true
            Mock Invoke-HitServerRequest { [pscustomobject]@{ pong = $true; version = 'v0' } }
            Mock Get-HitPSReadLineHistory { @('local x') }
            Get-HitUpItems 'x' | Should -Be @('local x')
            Should -Invoke Invoke-HitServerRequest -Times 0 -ParameterFilter { $null -ne $Request.up }
        }

        It 'falls back to PSReadLine''s history when no server answers, and never starts one' {
            $script:HitServerMode = $true
            Mock Invoke-HitServerRequest { $null }
            Mock Start-HitServer { }
            Mock Get-HitPSReadLineHistory { @('local x', 'other') }
            Get-HitUpItems 'x' | Should -Be @('local x')
            Should -Invoke Start-HitServer -Times 0
        }

        It 'does not touch the pipe at all when server mode is off' {
            Mock Invoke-HitServerRequest { throw 'should not be called' }
            Mock Get-HitPSReadLineHistory { @('local x') }
            Get-HitUpItems 'x' | Should -Be @('local x')
        }
    }

    Describe 'Opting in' {
        It 'is off unless asked for' {
            $script:HitUpBound | Should -BeFalse
        }
    }
}
