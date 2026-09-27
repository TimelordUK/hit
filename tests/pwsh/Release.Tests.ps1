#Requires -Modules @{ ModuleName = 'Pester'; ModuleVersion = '5.0.0' }

# scripts/release.ps1 against a throwaway repo and a bare "origin" in TestDrive: never
# this repo's tags or its GitHub remote.

BeforeAll {
    $script:Release = (Resolve-Path (Join-Path $PSScriptRoot '..' '..' 'scripts' 'release.ps1')).Path

    function New-TestRepo([string[]]$Tags) {
        $origin = Join-Path $TestDrive "origin-$([guid]::NewGuid()).git"
        $work = Join-Path $TestDrive "work-$([guid]::NewGuid())"
        git init --quiet --bare --initial-branch=main $origin
        git init --quiet --initial-branch=main $work
        Push-Location $work
        git config user.email 'test@example.com'
        git config user.name 'test'
        git config commit.gpgsign false
        git config tag.gpgsign false
        git remote add origin $origin
        git commit --quiet --allow-empty -m 'first'
        foreach ($t in $Tags) { git tag -a $t -m $t }
        git commit --quiet --allow-empty -m 'the change being released'
        git push --quiet origin main --tags 2>$null
        Pop-Location
        [pscustomobject]@{ Work = $work; Origin = $origin }
    }

    function Get-OriginTags($Repo) { @(git -C $Repo.Origin tag --list) }
}

Describe 'release.ps1' {
    BeforeEach { $script:Here = Get-Location }
    AfterEach { Set-Location $script:Here }

    It 'bumps the patch by default, tags with the HEAD subject and pushes the tag' {
        $r = New-TestRepo -Tags 'v0.1.8', 'v0.1.9'
        Push-Location $r.Work
        & $script:Release 6>$null
        Get-OriginTags $r | Should -Contain 'v0.1.10'
        git tag -l --format='%(contents:subject)' v0.1.10 | Should -Be 'v0.1.10: the change being released'
    }

    It 'orders versions numerically, not as text' {
        $r = New-TestRepo -Tags 'v0.1.9', 'v0.1.10'
        Push-Location $r.Work
        & $script:Release 6>$null
        Get-OriginTags $r | Should -Contain 'v0.1.11'
    }

    It 'bumps minor and major, resetting what follows' {
        $r = New-TestRepo -Tags 'v1.4.7'
        Push-Location $r.Work
        (& $script:Release -Bump minor -DryRun 6>&1 | Out-String) | Should -Match 'v1\.4\.7 -> v1\.5\.0'
        (& $script:Release -Bump major -DryRun 6>&1 | Out-String) | Should -Match 'v1\.4\.7 -> v2\.0\.0'
    }

    It 'changes nothing on a dry run' {
        $r = New-TestRepo -Tags 'v0.1.9'
        Push-Location $r.Work
        & $script:Release -DryRun 6>$null
        @(git tag --list) | Should -Not -Contain 'v0.1.10'
        Get-OriginTags $r | Should -Not -Contain 'v0.1.10'
    }

    It 'refuses a dirty tree' {
        $r = New-TestRepo -Tags 'v0.1.9'
        Push-Location $r.Work
        Set-Content -LiteralPath 'stray.txt' -Value 'x'
        { & $script:Release 6>$null } | Should -Throw '*not clean*'
    }

    It 'refuses a commit that is not on origin/main yet' {
        $r = New-TestRepo -Tags 'v0.1.9'
        Push-Location $r.Work
        git commit --quiet --allow-empty -m 'unpushed'
        { & $script:Release 6>$null } | Should -Throw '*not origin/main*'
        Get-OriginTags $r | Should -Not -Contain 'v0.1.10'
    }

    It 'refuses to tag a commit twice' {
        $r = New-TestRepo -Tags 'v0.1.9'
        Push-Location $r.Work
        & $script:Release 6>$null
        { & $script:Release 6>$null } | Should -Throw '*already tagged v0.1.10*'
    }
}
