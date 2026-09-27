// Package shell embeds the shell integration scripts printed by `hit init <shell>`.
package shell

import (
	_ "embed"
	"fmt"
	"io"
	"strings"
)

//go:embed pwsh/hit.ps1
var pwshScript string

// psQuote returns s as a single-quoted PowerShell string literal.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// WritePwsh writes the PowerShell integration: hit.ps1 as a dynamic module named "hit",
// enabled with the history path the binary resolved and the variables config.toml asks to
// capture (C-037), so the prompt hook never reads the config. `Remove-Module hit` unloads
// it; `Disable-Hit` stops recording.
func WritePwsh(w io.Writer, historyPath, exe, version string, captureEnv []string) error {
	enable := "Enable-Hit -HistoryPath " + psQuote(historyPath)
	if len(captureEnv) > 0 {
		quoted := make([]string, len(captureEnv))
		for i, n := range captureEnv {
			quoted[i] = psQuote(n)
		}
		enable += " -CaptureEnv @(" + strings.Join(quoted, ", ") + ")"
	}
	_, err := fmt.Fprintf(w, `# hit %s — PowerShell integration. Load with:
#   Invoke-Expression (& hit init pwsh | Out-String)
$null = New-Module -Name hit -ScriptBlock {
$script:HitExe = %s
$script:HitVersion = %s
%s
%s
Export-ModuleMember -Function Enable-Hit, Disable-Hit, Invoke-HitFinder, Enable-HitServer, Disable-HitServer, Get-HitStatus
} | Import-Module -Global
`, version, psQuote(exe), psQuote(version), pwshScript, enable)
	return err
}
