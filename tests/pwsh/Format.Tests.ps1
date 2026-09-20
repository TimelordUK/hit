#Requires -Modules @{ ModuleName = 'Pester'; ModuleVersion = '5.0.0' }

# One line ↔ many lines (S-004, S-016). Two rules hold for every case:
#   1. the rewrite parses to exactly the same tokens as the original;
#   2. anything unsafe or unparseable comes back untouched.

BeforeAll {
    . (Join-Path $PSScriptRoot '..' '..' 'shell' 'pwsh' 'hit.ps1')
    $script:BT = [string][char]96   # backtick
    $script:LF = [string][char]10
}

Describe 'Format-HitCommand' {
    It 'splits at top-level parameters' {
        $out = Format-HitCommand 'irm -Uri https://x -Method Post'
        $out | Should -BeExactly ("irm $BT$LF    -Uri https://x $BT$LF    -Method Post")
    }

    It 'splits after a pipe without adding a backtick, nesting the stage parameters' {
        $out = Format-HitCommand 'gci -Recurse | Select-Object -First 2'
        $out | Should -BeExactly ("gci $BT$LF    -Recurse |$LF    Select-Object $BT$LF        -First 2")
    }

    It 'leaves parameters inside script blocks, parens and hashtables alone' {
        foreach ($cmd in @(
                'gci | Where-Object { $_.Name -like "*x*" -and (Test-Path $_ -PathType Leaf) }'
                'irm -Body @{ method = "post"; opts = @(1, 2) }'
                'Write-Host (Get-Date -Format o)'
            )) {
            $out = Format-HitCommand $cmd
            # only top-level breaks: no line may start with a parameter that was nested
            foreach ($line in ($out -split "`n")) {
                $line | Should -Not -Match '^\s+-(PathType|Format)\b'
            }
            Test-HitSameCommand $cmd $out | Should -BeTrue
        }
    }

    It 'indents with the given indent' {
        Format-HitCommand 'irm -Uri x' -Indent '  ' | Should -BeExactly ("irm $BT$LF  -Uri x")
    }

    It 'returns the command unchanged when there is nothing to split' {
        foreach ($cmd in 'ls', 'git status', '$x = 1', '') {
            Format-HitCommand $cmd | Should -BeExactly $cmd
        }
    }

    It 'returns the command unchanged when it does not parse' {
        $broken = 'irm -Uri "unterminated'
        Format-HitCommand $broken | Should -BeExactly $broken
    }

    It 'leaves an already multi-line command alone' {
        $cmd = "irm $BT$LF    -Uri x"
        Format-HitCommand $cmd | Should -BeExactly $cmd
    }
}

Describe 'Join-HitCommand' {
    It 'brings a continued command back onto one line' {
        Join-HitCommand ("irm $BT$LF    -Uri https://x $BT$LF    -Method Post") |
            Should -BeExactly 'irm -Uri https://x -Method Post'
    }

    It 'joins a pipeline split after the pipe' {
        Join-HitCommand ("gci -Recurse |$LF    Select-Object -First 2") |
            Should -BeExactly 'gci -Recurse | Select-Object -First 2'
    }

    It 'handles CRLF line endings' {
        $crlf = "irm $BT" + [char]13 + $LF + '    -Uri x'
        Join-HitCommand $crlf | Should -BeExactly 'irm -Uri x'
    }

    It 'refuses to join a here-string, whose newlines are part of the command' {
        $cmd = '$body = @"' + $LF + '{ "size": 0 }' + $LF + '"@' + $LF + 'irm -Body $body'
        Join-HitCommand $cmd | Should -BeExactly $cmd
    }

    It 'leaves a single-line command alone' {
        Join-HitCommand 'irm -Uri x' | Should -BeExactly 'irm -Uri x'
    }

    It 'returns the command unchanged when it does not parse' {
        $broken = "irm $BT$LF   -Uri 'unterminated"
        Join-HitCommand $broken | Should -BeExactly $broken
    }
}

Describe 'Round trip over a corpus of real commands' {
    # These are the shapes the owner types daily. Each one must survive both directions
    # with an identical token stream, and end up back where it started.
    BeforeDiscovery {
        $script:Corpus = @(
            'ls'
            'git status'
            'irm -Uri https://elastic-prod-1:9200/logs-*/_search -Method Post -ContentType application/json'
            'Invoke-RestMethod -Uri "https://elastic-prod-1:9200/_cat/indices?v" -Headers @{ Accept = "application/json" }'
            'gci -Recurse -Filter *.go | Select-String TODO | Select-Object -First 20'
            'Get-ChildItem \\elastic-prod-1\logs -Filter *.log | Where-Object { $_.Length -gt 1mb } | Sort-Object Length -Descending'
            'sql-cli -q "select * from read_jsonl(''-'')" -o table'
            'docker run --rm -it -v ${PWD}:/work alpine sh -c "ls /work"'
            '$r = irm -Uri $u -Method Post -Body ($body | ConvertTo-Json -Depth 5)'
            'Set-Location \\elastic-prod-1\logs'
        )
    }

    It 'splits and rejoins <_> without changing the command' -ForEach $Corpus {
        $original = $_
        $split = Format-HitCommand $original
        Test-HitSameCommand $original $split | Should -BeTrue -Because 'splitting must not change the tokens'
        $rejoined = Join-HitCommand $split
        Test-HitSameCommand $original $rejoined | Should -BeTrue -Because 'joining must not change the tokens'
        $rejoined | Should -BeExactly $original -Because 'a round trip should land exactly where it started'
    }
}

Describe 'Invoke-HitToggleMultiline' {
    BeforeEach {
        $script:Editor = [pscustomobject]@{ Buffer = ''; Redraws = 0 }
        $fake = $script:Editor
        $script:HitEditor = @{
            GetBuffer = { $fake.Buffer }.GetNewClosure()
            SetBuffer = { param([string]$Text) $fake.Buffer = $Text }.GetNewClosure()
            Redraw    = { $fake.Redraws++ }.GetNewClosure()
        }
    }

    It 'splits a one-line command and joins it back on the next press' {
        $script:Editor.Buffer = 'irm -Uri https://x -Method Post'
        Invoke-HitToggleMultiline
        $script:Editor.Buffer | Should -Match ([regex]::Escape($BT))
        ($script:Editor.Buffer -split "`n").Count | Should -Be 3
        Invoke-HitToggleMultiline
        $script:Editor.Buffer | Should -BeExactly 'irm -Uri https://x -Method Post'
    }

    It 'leaves an empty or unparseable buffer alone and never throws' {
        foreach ($buffer in '', '   ', 'irm -Uri "oops') {
            $script:Editor.Buffer = $buffer
            { Invoke-HitToggleMultiline } | Should -Not -Throw
            $script:Editor.Buffer | Should -BeExactly $buffer
        }
    }
}
