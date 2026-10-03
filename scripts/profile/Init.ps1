# Prompt and hook integrations, shared by every machine. Dot-sourced LAST by
# profile.ps1, so hit chains starship/zoxide/PSFzf rather than being overwritten by them.

# oh-my-posh init pwsh --config ys | Invoke-Expression
# oh-my-posh init pwsh --config 'amro' | Invoke-Expression
Invoke-Expression (&starship init powershell)
# Invoke-Expression -Command $(mcfly init powershell | out-string)
# Invoke-Expression (& { (zoxide init --cmd cd powershell | Out-String) })

# hit: history finder (Ctrl+R) and Alt+M reflow.
Invoke-Expression (& hit init pwsh | Out-String)
