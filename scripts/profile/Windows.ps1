# Windows-only. Dot-sourced by profile.ps1 after Common.ps1.

Import-Module ZLocation
Import-Module Get-ChildItemColor

If (-Not (Test-Path Variable:PSise)) {  # Only run this in the console and not in the ISE
    Set-Alias ls Get-ChildItemColorFormatWide -option AllScope
    Set-Alias chrome 'C:\Program Files (x86)\Google\Chrome\Application\chrome.exe'
}

$vim="C:\WINDOWS\vim.bat"
[System.Environment]::SetEnvironmentVariable("VISUAL", $vim)

function shd() {
	z \\desktop-t81gpbn\shared
}

Set-PSReadLineKeyHandler -Chord 'Ctrl+g' `
    -ScriptBlock {
        Set-Location $((Get-ZLocation).GetEnumerator() |
        Sort-Object -Property Value -Descending |
        select-object -Property Name |
        fzf)
    }

Set-PSReadLineKeyHandler -Chord 'Ctrl+h' `
    -ScriptBlock {
        Get-History |
        Select-Object -Property CommandLine |
        fzf |
        Set-Clipboard

        [Microsoft.PowerShell.PSConsoleReadLine]::RevertLine()
        $a = Get-Clipboard
        [Microsoft.PowerShell.PSConsoleReadLine]::ClearScreen()
        [Microsoft.PowerShell.PSConsoleReadLine]::Insert($a)
    }

$chocopath="C:\ProgramData\chocolatey\lib\"

# Dynamically find ripgrep completion script regardless of version
$ripgrepPath = Get-ChildItem -Path "$chocopath\ripgrep\tools" -Directory -Name "ripgrep-*-x86_64-pc-windows-msvc" | Select-Object -First 1
if ($ripgrepPath) {
    $completionScript = Join-Path "$chocopath\ripgrep\tools\$ripgrepPath\complete" "_rg.ps1"
    if (Test-Path $completionScript) {
        . $completionScript
    }
}
# . $chocopath\fd\tools\_fd.ps1
# . C:\Users\Stephen\dev\f7hist.ps1

$env:HIT_SERVER = 1
$env:HIT_TIMING = 1
