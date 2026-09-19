#Requires -Modules @{ ModuleName = 'Pester'; ModuleVersion = '5.0.0' }

BeforeAll {
    . (Join-Path $PSScriptRoot '..' '..' 'shell' 'pwsh' 'hit.ps1')
    $script:Schema = Join-Path $PSScriptRoot '..' '..' 'schema' 'record.schema.json'
    $script:Crockford = '^[0-9A-HJKMNP-TV-Z]{26}$'

    # Decodes the 48-bit millisecond time from the first 10 chars of a ULID.
    function Get-UlidMs([string]$Id) {
        [uint64]$ms = 0
        foreach ($c in $Id.Substring(0, 10).ToCharArray()) {
            $ms = $ms * 32 + $script:HitUlidAlphabet.IndexOf($c)
        }
        $ms
    }
}

Describe 'Get-HitNow' {
    AfterEach { $env:HIT_NOW = $null }

    It 'is pinned by HIT_NOW' {
        $env:HIT_NOW = '2026-09-19T10:12:03.412Z'
        ConvertTo-HitTimestamp (Get-HitNow) | Should -Be '2026-09-19T10:12:03.412Z'
    }
    It 'is the real UTC time otherwise' {
        $env:HIT_NOW = $null
        ((Get-HitNow) - [DateTimeOffset]::UtcNow).Duration().TotalSeconds | Should -BeLessThan 5
    }
}

Describe 'ConvertTo-HitTimestamp' {
    It 'writes UTC with milliseconds whatever the culture and offset' {
        $t = [DateTimeOffset]::new(2026, 9, 19, 11, 12, 3, 412, [TimeSpan]::FromHours(1))
        $old = [Globalization.CultureInfo]::CurrentCulture
        try {
            [Globalization.CultureInfo]::CurrentCulture = 'ar-SA'
            ConvertTo-HitTimestamp $t | Should -Be '2026-09-19T10:12:03.412Z'
        } finally { [Globalization.CultureInfo]::CurrentCulture = $old }
    }
}

Describe 'New-HitId' {
    It 'is a 26-char Crockford ULID encoding the given time' {
        $t = [DateTimeOffset]::Parse('2026-09-19T10:12:03.412Z')
        $id = New-HitId $t
        $id | Should -Match $script:Crockford
        Get-UlidMs $id | Should -Be $t.ToUnixTimeMilliseconds()
    }
    It 'sorts by time' {
        $t = [DateTimeOffset]::Parse('2026-09-19T10:12:03.412Z')
        (New-HitId $t) | Should -BeLessThan (New-HitId $t.AddMilliseconds(1))
    }
    It 'is unique within the same millisecond' {
        $t = Get-HitNow
        $ids = 1..500 | ForEach-Object { New-HitId $t }
        ($ids | Sort-Object -Unique).Count | Should -Be 500
    }
    It 'uses the high random bits (no constant tail)' {
        $t = Get-HitNow
        $tails = 1..50 | ForEach-Object { (New-HitId $t).Substring(10, 4) }
        ($tails | Sort-Object -Unique).Count | Should -BeGreaterThan 40
    }
}

Describe 'New-HitRecord and ConvertTo-HitJsonLine' {
    It 'writes keys in the Go writer order and omits empty fields' {
        $r = New-HitRecord -Kind cmd -Id 'X' -Timestamp 'T' -Command 'ls' -Cwd 'C:\' -Shell pwsh -SessionId 's'
        @($r.Keys) | Should -Be @('k', 'id', 'ts', 'cmd', 'cwd', 'sh', 'sid')
    }
    It 'keeps exit code 0 and duration 0' {
        ConvertTo-HitJsonLine (New-HitRecord -Kind end -Id 'X' -ExitCode 0 -DurationMs 0) |
            Should -BeExactly '{"k":"end","id":"X","exit":0,"ms":0}'
    }
    It 'produces a single line for a multi-line command and round-trips it exactly' {
        $cmd = "Invoke-RestMethod ``n    -Uri x ``r`n    -Method Post`t'q' `"dq`" \ `u{1F980}"
        $line = ConvertTo-HitJsonLine (New-HitRecord -Kind cmd -Id 'X' -Command $cmd)
        $line | Should -Not -Match "[`r`n]"
        (ConvertFrom-Json $line).cmd | Should -BeExactly $cmd
    }
    It 'produces records that match the schema' {
        $now = Get-HitNow
        $records = @(
            New-HitRecord -Kind cmd -Id (New-HitId $now) -Timestamp (ConvertTo-HitTimestamp $now) `
                -Command "a`nb" -Cwd '\\elastic-prod-1\logs' -Shell pwsh -HostName box1 -SessionId s1
            New-HitRecord -Kind end -Id (New-HitId $now) -ExitCode 1 -DurationMs 12
            New-HitRecord -Kind cd -Timestamp (ConvertTo-HitTimestamp $now) -Dir 'C:\dev' -Shell pwsh
            New-HitRecord -Kind del -Id (New-HitId $now) -Timestamp (ConvertTo-HitTimestamp $now)
        )
        foreach ($r in $records) {
            ConvertTo-HitJsonLine $r | Test-Json -SchemaFile $script:Schema | Should -BeTrue
        }
    }
}

Describe 'Add-HitLine' {
    BeforeEach {
        $script:Path = Join-Path $TestDrive "data-$([guid]::NewGuid())" 'history.jsonl'
    }

    It 'creates the data dir and writes UTF-8 without BOM, LF-terminated' {
        Add-HitLine -Path $script:Path -Line '{"k":"cmd","cmd":"café"}' | Should -BeTrue
        Add-HitLine -Path $script:Path -Line '{"k":"cmd","cmd":"two"}' | Should -BeTrue
        $bytes = [IO.File]::ReadAllBytes($script:Path)
        $bytes[0] | Should -Be ([byte][char]'{')
        [Text.Encoding]::UTF8.GetString($bytes) | Should -BeExactly "{`"k`":`"cmd`",`"cmd`":`"café`"}`n{`"k`":`"cmd`",`"cmd`":`"two`"}`n"
    }
    It 'returns $false instead of throwing when the path is unusable' {
        $bad = Join-Path $TestDrive 'file-not-dir'
        Set-Content -Path $bad -Value x
        Add-HitLine -Path (Join-Path $bad 'history.jsonl') -Line 'x' | Should -BeFalse
    }
    It 'never interleaves lines from concurrent writers' {
        $script:Path = Join-Path $TestDrive 'concurrent.jsonl'
        $src = Join-Path $PSScriptRoot '..' '..' 'shell' 'pwsh' 'hit.ps1'
        $path = $script:Path
        1..4 | ForEach-Object -ThrottleLimit 4 -Parallel {
            . $using:src
            $w = $_
            $pad = 'x' * 2000
            foreach ($i in 1..200) {
                if (-not (Add-HitLine -Path $using:path -Line "{`"w`":$w,`"i`":$i,`"pad`":`"$pad`"}")) { throw 'write failed' }
            }
        }
        $lines = [IO.File]::ReadAllLines($path)
        $lines.Count | Should -Be 800
        foreach ($l in $lines) { { ConvertFrom-Json $l } | Should -Not -Throw }
        foreach ($w in 1..4) {
            @($lines | ConvertFrom-Json | Where-Object w -EQ $w).Count | Should -Be 200
        }
    }
}
