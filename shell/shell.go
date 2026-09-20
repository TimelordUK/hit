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
// enabled with the history path the binary resolved. `Remove-Module hit` unloads it;
// `Disable-Hit` stops recording.
func WritePwsh(w io.Writer, historyPath, exe, version string) error {
	_, err := fmt.Fprintf(w, `# hit %s — PowerShell integration. Load with:
#   Invoke-Expression (& hit init pwsh | Out-String)
$null = New-Module -Name hit -ScriptBlock {
$script:HitExe = %s
%s
Enable-Hit -HistoryPath %s
Export-ModuleMember -Function Enable-Hit, Disable-Hit, Invoke-HitFinder
} | Import-Module -Global
`, version, psQuote(exe), pwshScript, psQuote(historyPath))
	return err
}
