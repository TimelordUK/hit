package tui

import (
	"strings"
	"testing"

	"github.com/TimelordUK/hit/internal/record"
	"github.com/TimelordUK/hit/internal/search"
	"github.com/TimelordUK/hit/internal/store"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// The selected row is drawn from several styles — the time column, the matched runes, the
// ⏎ marker — and each of them ends with a full SGR reset. Wrapping the finished line in a
// "selected" style put the highlight on only as far as the first nested style, so in
// practice the bar covered the "▸ " prefix and nothing else: the one row you need to find
// with your eye was the one row with no highlight on it (T-010).
//
// The assertion is on the rendered escapes rather than on the styles, because the bug was
// entirely in how the escapes composed — every style involved was correct on its own.
func TestSelectedRowIsHighlightedAllTheWayAcross(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
	}{
		{"with a match to highlight", "git"},
		{"with no match", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := selectedRow(t, "git commit -m hello", tc.query)
			cells := paintedCells(row)
			if len(cells) == 0 {
				t.Fatal("no visible cells: the test cannot see anything")
			}
			for i, painted := range cells {
				if !painted {
					t.Fatalf("the highlight stops at cell %d of %d:\n%s",
						i, len(cells), strings.ReplaceAll(row, "\x1b", "ESC"))
				}
			}
		})
	}
}

// The bar runs to the edge of the pane, so the selection reads as a band across the list
// rather than as a highlight that stops wherever the command happens to end.
func TestSelectedRowFillsTheWidth(t *testing.T) {
	const width = 60
	row := selectedRow(t, "git status", "git")
	if got := len(paintedCells(row)); got != width {
		t.Errorf("highlighted %d cells, want the full pane width of %d", got, width)
	}
}

// An unselected row carries no highlight at all, or every row looks selected.
func TestUnselectedRowIsNotHighlighted(t *testing.T) {
	m, s := highlightModel(t, "git commit -m hello", "git")
	m.cursor = 1 // move the selection off row 0
	for i, painted := range paintedCells(m.renderRow(s, 0)) {
		if painted {
			t.Fatalf("cell %d of an unselected row is highlighted", i)
		}
	}
}

func highlightModel(t *testing.T, cmd, query string) (Model, styles) {
	t.Helper()
	// The rendered escapes are the subject, so the profile is pinned: left to itself
	// lipgloss looks at the test binary's output, which is not a terminal, and renders
	// everything as plain text — the test would pass while seeing nothing. It is a
	// package-level setting, so it is put back afterwards; the tests that assert on the
	// finder's *text* are written against plain output and start failing without this.
	was := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(was) })
	lipgloss.SetColorProfile(termenv.ANSI256)
	recs := []record.Record{
		{K: record.KindCmd, ID: "A", TS: ts(5), Cmd: cmd, Cwd: `C:\dev\hit`, Sh: "pwsh", Sid: "s1"},
		{K: record.KindCmd, ID: "B", TS: ts(4), Cmd: "git switch main", Cwd: `C:\dev\hit`, Sh: "pwsh", Sid: "s1"},
	}
	m := New(store.Build(recs), search.Query{Scope: search.ScopeAll, Text: query, Now: now})
	m.width = 60
	return m, newStyles()
}

func selectedRow(t *testing.T, cmd, query string) string {
	t.Helper()
	m, s := highlightModel(t, cmd, query)
	m.cursor = 0
	return m.renderRow(s, 0)
}

// paintedCells walks the SGR escapes in a rendered row and reports, for each visible cell,
// whether a background colour (or reverse video, which is how NO_COLOR draws a selection)
// was in force when it was written. It tracks whether *any* background is set rather than
// which one, so the test says nothing about the palette and survives lipgloss degrading
// 256 colours to 16 on a terminal that only has 16.
func paintedCells(row string) []bool {
	var cells []bool
	painted := false
	runes := []rune(row)
	for i := 0; i < len(runes); {
		if runes[i] == '\x1b' && i+1 < len(runes) && runes[i+1] == '[' {
			j := i + 2
			for j < len(runes) && runes[j] != 'm' && runes[j] != '\a' {
				j++
			}
			if j < len(runes) && runes[j] == 'm' {
				painted = applySGR(painted, string(runes[i+2:j]))
			}
			i = j + 1
			continue
		}
		cells = append(cells, painted)
		i++
	}
	return cells
}

// applySGR folds one escape's parameters into "is a background in force".
func applySGR(painted bool, params string) bool {
	fields := strings.Split(params, ";")
	for k := 0; k < len(fields); k++ {
		switch fields[k] {
		case "", "0":
			painted = false // a full reset: this is what used to end the bar early
		case "7":
			painted = true // reverse video
		case "27", "49":
			painted = false
		case "48":
			painted = true // extended background; its arguments follow and are skipped
			if k+1 < len(fields) && fields[k+1] == "5" {
				k += 2
			} else if k+1 < len(fields) && fields[k+1] == "2" {
				k += 4
			}
		default:
			if n := fields[k]; len(n) == 2 && n[0] == '4' && n[1] >= '0' && n[1] <= '7' {
				painted = true // 40-47
			} else if len(n) == 3 && n[0] == '1' && n[1] == '0' && n[2] >= '0' && n[2] <= '7' {
				painted = true // 100-107
			}
		}
	}
	return painted
}
