# Linux (WSL) only. Dot-sourced by profile.ps1 after Common.ps1.
# ZLocation and Get-ChildItemColor aren't used here: zoxide and eza replace them.

If (-Not (Test-Path Variable:PSise)) {  # Only run this in the console and not in the ISE
    function ls { eza --icons --group-directories-first @args }
}

$env:VISUAL = "nvim"
$env:EDITOR = "nvim"

# UNC share - Windows only. If it's mounted under WSL, point this at /mnt/... instead.
# function shd() {
# 	z \\desktop-t81gpbn\shared
# }

# Ctrl+g: fzf over zoxide's frecency list (Windows uses ZLocation)
Set-PSReadLineKeyHandler -Chord 'Ctrl+g' `
    -ScriptBlock {
        $dir = zoxide query --list | fzf
        if ($dir) {
            Set-Location $dir
            [Microsoft.PowerShell.PSConsoleReadLine]::InvokePrompt()
        }
    }

# No Ctrl+h history picker: hit's Ctrl+R supersedes it, and on Linux terminals Ctrl+h is Backspace

# rg and fd generate their own completions (Windows loads them from chocolatey)
rg --generate complete-powershell | Out-String | Invoke-Expression
fd --gen-completions powershell 2>$null | Out-String | Invoke-Expression
