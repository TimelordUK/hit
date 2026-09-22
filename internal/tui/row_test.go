package tui

import (
	"strings"
	"testing"

	"github.com/TimelordUK/hit/internal/record"
	"github.com/TimelordUK/hit/internal/search"
	"github.com/TimelordUK/hit/internal/store"
)

// A real one from the owner's work machine. Its first line is a bare `& {`, which every
// command of this shape shares: glancing down the list, every one of them reads the same.
const blockCmd = `& {
    . .\scripts\elevated-remote-helpers.ps1
    Write-Host "---- inside dot-sourced scope ---"
    (Get-Command Get-PdProcessTree).Parameters.Keys -join ', '
    $treeArgs = @{ ComputerName = 'doesnotexist.local'; Credential = $cred; RootProcessId = 1 }
    Get-PdProcessTree @treeArgs
}`

func rowFor(t *testing.T, cmd string, width int) string {
	t.Helper()
	recs := []record.Record{{K: record.KindCmd, ID: "X", TS: ts(5), Cmd: cmd, Cwd: `C:\dev\hit`, Sh: "pwsh", Sid: "s1"}}
	m := New(store.Build(recs), search.Query{Scope: search.ScopeAll, Now: now})
	m.width = width
	return m.renderRow(newStyles(), 0)
}

func TestRowIdentifiesABlockCommand(t *testing.T) {
	row := rowFor(t, blockCmd, 120)
	if !strings.Contains(row, "elevated-remote-helpers") {
		t.Errorf("row does not say what the command does, so the list is unreadable:\n%s", row)
	}
	if !strings.Contains(row, "⏎ +6") {
		t.Errorf("row lost the multi-line marker:\n%s", row)
	}
}

// Two commands that differ only after their first line must not render the same.
func TestBlockCommandsAreToldApart(t *testing.T) {
	a := rowFor(t, "& {\n    Get-Process\n}", 120)
	b := rowFor(t, "& {\n    Get-Service\n}", 120)
	if a == b {
		t.Errorf("two different commands render identically:\n%s", a)
	}
}

// A single-line command is shown byte for byte: collapsing runs of spaces inside one
// would change what the user sees for no gain (principle 3 — the view may reformat, but
// it should not do so gratuitously).
func TestSingleLineRowsAreUntouched(t *testing.T) {
	cmd := `git commit -m "two  spaces  kept"`
	row := rowFor(t, cmd, 120)
	if !strings.Contains(row, cmd) {
		t.Errorf("single-line command was reformatted:\n%s", row)
	}
}

func TestRowLabelFlattensOnlyMultiLine(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"single line kept", "git  status", "git  status"},
		{"newline becomes a space", "a\nb", "a b"},
		{"indentation collapses", "& {\n    Get-Process\n}", "& { Get-Process }"},
		{"crlf too", "a\r\nb", "a b"},
		{"leading and trailing trimmed", "\n  a\n  ", "a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := rowLabel(tc.in)
			if string(got) != tc.want {
				t.Errorf("got %q, want %q", string(got), tc.want)
			}
		})
	}
}

// Highlighting is driven by indexes into the whole command, so flattening must carry a
// map back to them or the highlight lands on the wrong characters.
func TestRowLabelMapsBackToTheCommand(t *testing.T) {
	in := "& {\n    Get-Process\n}"
	label, src := rowLabel(in)
	if len(label) != len(src) {
		t.Fatalf("label and source map disagree: %d vs %d", len(label), len(src))
	}
	runes := []rune(in)
	for i, r := range label {
		if r == ' ' {
			continue // a space may stand in for a run of whitespace
		}
		if runes[src[i]] != r {
			t.Errorf("label[%d]=%q maps to %q", i, r, runes[src[i]])
		}
	}
}

// The match highlight must follow a term that only appears after the first line.
func TestMatchOnALaterLineIsHighlighted(t *testing.T) {
	recs := []record.Record{{K: record.KindCmd, ID: "X", TS: ts(5), Cmd: blockCmd, Cwd: `C:\dev\hit`, Sh: "pwsh", Sid: "s1"}}
	m := New(store.Build(recs), search.Query{Scope: search.ScopeAll, Now: now, Text: "PdProcessTree"})
	m.width = 200
	if len(m.results) == 0 {
		t.Fatal("the command should match")
	}
	if len(m.results[0].Matched) == 0 {
		t.Fatal("no matched positions")
	}
	row := m.renderRow(newStyles(), 0)
	if !strings.Contains(row, "PdProcessTree") {
		t.Errorf("matched text from a later line is not in the row:\n%s", row)
	}
}
