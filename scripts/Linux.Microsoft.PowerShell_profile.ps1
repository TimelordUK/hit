using namespace System.Management.Automation
using namespace System.Management.Automation.Language

# Linux (WSL) port of the Windows profile. Original kept alongside as
# Microsoft.PowerShell_profile.ps1.windows-orig

# Import-Module ZLocation            # Windows-only habit; zoxide (below) replaces it
# Import-Module Get-ChildItemColor   # not needed on Linux; ls -> eza below
# Import-Module oh-my-posh
Import-Module PSFzf
Import-Module Microsoft.PowerShell.ConsoleGuiTools
Enable-PsFzfAliases

If (-Not (Test-Path Variable:PSise)) {  # Only run this in the console and not in the ISE
    Set-Alias l Get-ChildItem -option AllScope
    function ls { eza --icons --group-directories-first @args }
    # Set-Alias chrome 'C:\Program Files (x86)\Google\Chrome\Application\chrome.exe'
}

# Import-Module posh-git
Set-PSReadLineOption -EditMode vi
Set-PSReadLineOption -ViModeIndicator Prompt
Set-PSReadLineOption -HistorySearchCursorMovesToEnd:$true
Set-PSReadLineKeyHandler -Chord 'Ctrl+SpaceBar' -Function MenuComplete

# $vim="C:\WINDOWS\vim.bat"
$env:VISUAL = "nvim"
$env:EDITOR = "nvim"
$env:FZF_DEFAULT_COMMAND = "fd --type file"
#$env:FZF_DEFAULT_COMMAND = "rg --files"
#$env:FZF_DEFAULT_OPTS = "-m -height 50% --border"

# UNC share - Windows only. If it's mounted under WSL, point this at /mnt/... instead.
# function shd() {
# 	z \\desktop-t81gpbn\shared
# }

# Ctrl+g: fzf over zoxide's frecency list (was ZLocation)
Set-PSReadLineKeyHandler -Chord 'Ctrl+g' `
    -ScriptBlock {
        $dir = zoxide query --list | fzf
        if ($dir) {
            Set-Location $dir
            [Microsoft.PowerShell.PSConsoleReadLine]::InvokePrompt()
        }
    }

# Ctrl+h: superseded by hit's Ctrl+R, and on Linux terminals Ctrl+h is Backspace
# Set-PSReadLineKeyHandler -Chord 'Ctrl+h' `
#     -ScriptBlock {
#         Get-History |
#         Select-Object -Property CommandLine |
#         fzf |
#         Set-Clipboard
#
#         [Microsoft.PowerShell.PSConsoleReadLine]::RevertLine()
#         $a = Get-Clipboard
#         [Microsoft.PowerShell.PSConsoleReadLine]::ClearScreen()
#         [Microsoft.PowerShell.PSConsoleReadLine]::Insert($a)
#     }

Set-PSReadLineKeyHandler -Chord 'Ctrl+p' `
   -ScriptBlock {
	fzf --ansi --preview-window 'right:60%' --preview 'bat --color=always --style=header,grid --line-range :300 {}' |
        Set-Clipboard
#        fzf --preview 'bat --color=always --style=numbers --line-range=:500 {}' | 
    }

Set-PSReadLineKeyHandler -Key '"',"'" `
                         -BriefDescription SmartInsertQuote `
                         -LongDescription "Insert paired quotes if not already on a quote" `
                         -ScriptBlock {
    param($key, $arg)

    $quote = $key.KeyChar

    $selectionStart = $null
    $selectionLength = $null
    [Microsoft.PowerShell.PSConsoleReadLine]::GetSelectionState([ref]$selectionStart, [ref]$selectionLength)

    $line = $null
    $cursor = $null
    [Microsoft.PowerShell.PSConsoleReadLine]::GetBufferState([ref]$line, [ref]$cursor)

    # If text is selected, just quote it without any smarts
    if ($selectionStart -ne -1)
    {
        [Microsoft.PowerShell.PSConsoleReadLine]::Replace($selectionStart, $selectionLength, $quote + $line.SubString($selectionStart, $selectionLength) + $quote)
        [Microsoft.PowerShell.PSConsoleReadLine]::SetCursorPosition($selectionStart + $selectionLength + 2)
        return
    }

    $ast = $null
    $tokens = $null
    $parseErrors = $null
    [Microsoft.PowerShell.PSConsoleReadLine]::GetBufferState([ref]$ast, [ref]$tokens, [ref]$parseErrors, [ref]$null)

    function FindToken
    {
        param($tokens, $cursor)

        foreach ($token in $tokens)
        {
            if ($cursor -lt $token.Extent.StartOffset) { continue }
            if ($cursor -lt $token.Extent.EndOffset) {
                $result = $token
                $token = $token -as [StringExpandableToken]
                if ($token) {
                    $nested = FindToken $token.NestedTokens $cursor
                    if ($nested) { $result = $nested }
                }

                return $result
            }
        }
        return $null
    }

    $token = FindToken $tokens $cursor

    # If we're on or inside a **quoted** string token (so not generic), we need to be smarter
    if ($token -is [StringToken] -and $token.Kind -ne [TokenKind]::Generic) {
        # If we're at the start of the string, assume we're inserting a new string
        if ($token.Extent.StartOffset -eq $cursor) {
            [Microsoft.PowerShell.PSConsoleReadLine]::Insert("$quote$quote ")
            [Microsoft.PowerShell.PSConsoleReadLine]::SetCursorPosition($cursor + 1)
            return
        }

        # If we're at the end of the string, move over the closing quote if present.
        if ($token.Extent.EndOffset -eq ($cursor + 1) -and $line[$cursor] -eq $quote) {
            [Microsoft.PowerShell.PSConsoleReadLine]::SetCursorPosition($cursor + 1)
            return
        }
    }

    if ($null -eq $token -or
        $token.Kind -eq [TokenKind]::RParen -or $token.Kind -eq [TokenKind]::RCurly -or $token.Kind -eq [TokenKind]::RBracket) {
        if ($line[0..$cursor].Where{$_ -eq $quote}.Count % 2 -eq 1) {
            # Odd number of quotes before the cursor, insert a single quote
            [Microsoft.PowerShell.PSConsoleReadLine]::Insert($quote)
        }
        else {
            # Insert matching quotes, move cursor to be in between the quotes
            [Microsoft.PowerShell.PSConsoleReadLine]::Insert("$quote$quote")
            [Microsoft.PowerShell.PSConsoleReadLine]::SetCursorPosition($cursor + 1)
        }
        return
    }

    # If cursor is at the start of a token, enclose it in quotes.
    if ($token.Extent.StartOffset -eq $cursor) {
        if ($token.Kind -eq [TokenKind]::Generic -or $token.Kind -eq [TokenKind]::Identifier -or 
            $token.Kind -eq [TokenKind]::Variable -or $token.TokenFlags.hasFlag([TokenFlags]::Keyword)) {
            $end = $token.Extent.EndOffset
            $len = $end - $cursor
            [Microsoft.PowerShell.PSConsoleReadLine]::Replace($cursor, $len, $quote + $line.SubString($cursor, $len) + $quote)
            [Microsoft.PowerShell.PSConsoleReadLine]::SetCursorPosition($end + 2)
            return
        }
    }

    # We failed to be smart, so just insert a single quote
    [Microsoft.PowerShell.PSConsoleReadLine]::Insert($quote)
}

Set-PSReadLineKeyHandler -Key '(','{','[' `
                         -BriefDescription InsertPairedBraces `
                         -LongDescription "Insert matching braces" `
                         -ScriptBlock {
    param($key, $arg)

    $closeChar = switch ($key.KeyChar)
    {
        <#case#> '(' { [char]')'; break }
        <#case#> '{' { [char]'}'; break }
        <#case#> '[' { [char]']'; break }
    }

    $selectionStart = $null
    $selectionLength = $null
    [Microsoft.PowerShell.PSConsoleReadLine]::GetSelectionState([ref]$selectionStart, [ref]$selectionLength)

    $line = $null
    $cursor = $null
    [Microsoft.PowerShell.PSConsoleReadLine]::GetBufferState([ref]$line, [ref]$cursor)
    
    if ($selectionStart -ne -1)
    {
      # Text is selected, wrap it in brackets
      [Microsoft.PowerShell.PSConsoleReadLine]::Replace($selectionStart, $selectionLength, $key.KeyChar + $line.SubString($selectionStart, $selectionLength) + $closeChar)
      [Microsoft.PowerShell.PSConsoleReadLine]::SetCursorPosition($selectionStart + $selectionLength + 2)
    } else {
      # No text is selected, insert a pair
      [Microsoft.PowerShell.PSConsoleReadLine]::Insert("$($key.KeyChar)$closeChar")
      [Microsoft.PowerShell.PSConsoleReadLine]::SetCursorPosition($cursor + 1)
    }
}

Set-PSReadLineKeyHandler -Key ')',']','}' `
                         -BriefDescription SmartCloseBraces `
                         -LongDescription "Insert closing brace or skip" `
                         -ScriptBlock {
    param($key, $arg)

    $line = $null
    $cursor = $null
    [Microsoft.PowerShell.PSConsoleReadLine]::GetBufferState([ref]$line, [ref]$cursor)

    if ($line[$cursor] -eq $key.KeyChar)
    {
        [Microsoft.PowerShell.PSConsoleReadLine]::SetCursorPosition($cursor + 1)
    }
    else
    {
        [Microsoft.PowerShell.PSConsoleReadLine]::Insert("$($key.KeyChar)")
    }
}

Set-PSReadLineKeyHandler -Key Backspace `
                         -BriefDescription SmartBackspace `
                         -LongDescription "Delete previous character or matching quotes/parens/braces" `
                         -ScriptBlock {
    param($key, $arg)

    $line = $null
    $cursor = $null
    [Microsoft.PowerShell.PSConsoleReadLine]::GetBufferState([ref]$line, [ref]$cursor)

    if ($cursor -gt 0)
    {
        $toMatch = $null
        if ($cursor -lt $line.Length)
        {
            switch ($line[$cursor])
            {
                <#case#> '"' { $toMatch = '"'; break }
                <#case#> "'" { $toMatch = "'"; break }
                <#case#> ')' { $toMatch = '('; break }
                <#case#> ']' { $toMatch = '['; break }
                <#case#> '}' { $toMatch = '{'; break }
            }
        }

        if ($toMatch -ne $null -and $line[$cursor-1] -eq $toMatch)
        {
            [Microsoft.PowerShell.PSConsoleReadLine]::Delete($cursor - 1, 2)
        }
        else
        {
            [Microsoft.PowerShell.PSConsoleReadLine]::BackwardDeleteChar($key, $arg)
        }
    }
}

function y {
    $tmp = (New-TemporaryFile).FullName
    yazi $args --cwd-file="$tmp"
    $cwd = Get-Content -Path $tmp -Encoding UTF8
    if (-not [String]::IsNullOrEmpty($cwd) -and $cwd -ne $PWD.Path) {
        Set-Location -LiteralPath (Resolve-Path -LiteralPath $cwd).Path
    }
    Remove-Item -Path $tmp
}

# Chocolatey paths are Windows-only; rg and fd generate their own completions
# $chocopath="C:\ProgramData\chocolatey\lib\"
# $ripgrepPath = Get-ChildItem -Path "$chocopath\ripgrep\tools" -Directory -Name "ripgrep-*-x86_64-pc-windows-msvc" | Select-Object -First 1
# ...
rg --generate complete-powershell | Out-String | Invoke-Expression
fd --gen-completions powershell 2>$null | Out-String | Invoke-Expression
# . C:\Users\Stephen\dev\f7hist.ps1

# oh-my-posh init pwsh --config ys | Invoke-Expression
# oh-my-posh init pwsh --config 'amro' | Invoke-Expression
Invoke-Expression (&starship init powershell)
# Invoke-Expression -Command $(mcfly init powershell | out-string)
Invoke-Expression (& { (zoxide init --cmd cd powershell | Out-String) })

# zellij tab-completion (regenerate after upgrades:
#   zellij setup --generate-completion powershell | Out-File "$PSScriptRoot\zellij-completion.ps1" -Encoding utf8)
$zellijCompletion = Join-Path $PSScriptRoot 'zellij-completion.ps1'
if (Test-Path $zellijCompletion) { . $zellijCompletion }

# --- eza-based listings (Linux-like) -----------------------------------------
# ll : equivalent to `ls -alrt` -- long, all (incl. hidden), sorted by mtime,
#      newest at the bottom, with icons and git status. Extra args pass through.
function ll {
    eza --long --all --icons --git --sort=modified --reverse --group-directories-first @args
}

# lt : shallow tree. First numeric arg sets depth (default 2); other args are
#      treated as paths, so all of these work:
#        lt              # cwd, depth 2
#        lt 3            # cwd, depth 3
#        lt .\src        # path, depth 2
#        lt 4 .\src      # path, depth 4
function lt {
    $depth = 2
    $rest = @()
    foreach ($a in $args) {
        if ($a -match '^\d+$' -and $rest.Count -eq 0) {
            $depth = [int]$a
        } else {
            $rest += $a
        }
    }
    eza --tree --level=$depth --icons --group-directories-first @rest
}

. ~/dev/sql-cli/scripts/lrt.ps1

# hit: history finder (Ctrl+R) and Alt+M reflow. Keep this LAST so it chains
# starship/zoxide/PSFzf rather than being overwritten by them.
Invoke-Expression (& hit init pwsh | Out-String) 