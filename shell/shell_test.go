package shell

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// `Invoke-Expression (& hit init pwsh | Out-String)` decodes the script with the console's
// output encoding, which on Windows is the OEM code page unless the user changed it. So a
// non-ASCII character in code arrives garbled: `→` printed as `ΓåÆ` after a jump (owner,
// 2026-10-03). In a comment it is harmless; anywhere else, spell it with [char].
func TestPwshScriptIsASCIIOutsideComments(t *testing.T) {
	for i, line := range strings.Split(pwshScript, "\n") {
		code := line
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if j := strings.Index(code, " # "); j >= 0 {
			code = code[:j] // a trailing comment
		}
		for _, r := range code {
			if r >= utf8.RuneSelf {
				t.Errorf("hit.ps1:%d: %q is not ASCII and will be garbled by a non-UTF-8 console; use [char]0x%04X\n  %s",
					i+1, r, r, strings.TrimSpace(line))
				break
			}
		}
	}
}
