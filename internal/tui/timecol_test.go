package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/TimelordUK/hit/internal/record"
	"github.com/TimelordUK/hit/internal/search"
	"github.com/TimelordUK/hit/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

func TestTimeColumnFormats(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		at         time.Time
	}{
		{"today shows the clock", " 11:10", now.Add(-50 * time.Minute)},
		{"midnight today", " 00:30", time.Date(2026, 9, 19, 0, 30, 0, 0, time.UTC)},
		{"another day shows the date", "16 Sep", now.AddDate(0, 0, -3)},
		{"last year shows the date", "19 Sep", now.AddDate(-1, 0, 0)},
		{"no timestamp is blank", "      ", time.Time{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := timeColumn(tc.at, now)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			if len([]rune(got)) != timeColWidth {
				t.Errorf("got width %d, want %d", len([]rune(got)), timeColWidth)
			}
		})
	}
}

func rowWithTime(t *testing.T, at string, width int) string {
	t.Helper()
	recs := []record.Record{{K: record.KindCmd, ID: "X", TS: at, Cmd: "git status", Cwd: `C:\dev\hit`, Sh: "pwsh", Sid: "s1"}}
	m := New(store.Build(recs), search.Query{Scope: search.ScopeAll, Now: now})
	m.width = width
	return m.renderRow(newStyles(), 0)
}

func TestRowCarriesTheTime(t *testing.T) {
	row := rowWithTime(t, ts(50), 80)
	if !strings.Contains(row, "11:10") {
		t.Errorf("the run's time is not on the row:\n%s", row)
	}
	if !strings.Contains(row, "git status") {
		t.Errorf("the command went missing:\n%s", row)
	}
}

// T-001: the column goes before the command is squeezed, not after.
func TestNarrowPaneDropsTheTimeNotTheCommand(t *testing.T) {
	row := rowWithTime(t, ts(50), 24)
	if strings.Contains(row, "11:10") {
		t.Errorf("the time should be dropped in a narrow pane:\n%s", row)
	}
	if !strings.Contains(row, "git status") {
		t.Errorf("the command should survive:\n%s", row)
	}
}

// An entry from a damaged record has no time; its row must still line up with the rest,
// which is what the blank column is for. Rows 1 and 2 are compared because row 0 carries
// the cursor and is padded to the full pane width.
func TestRowsAlignWhenATimeIsMissing(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	recs := []record.Record{
		{K: record.KindCmd, ID: "A", TS: ts(10), Cmd: "git log", Sh: "pwsh", Sid: "s1"},
		{K: record.KindCmd, ID: "B", TS: ts(50), Cmd: "git status", Sh: "pwsh", Sid: "s1"},
		{K: record.KindCmd, ID: "C", Cmd: "git diff", Sh: "pwsh", Sid: "s1"},
	}
	m := New(store.Build(recs), search.Query{Scope: search.ScopeAll, Now: now})
	m.width = 80
	withTime := strings.Index(m.renderRow(newStyles(), 1), "git status")
	without := strings.Index(m.renderRow(newStyles(), 2), "git diff")
	if withTime != without {
		t.Errorf("commands start in different columns: %d with a time, %d without", withTime, without)
	}
}

func TestViewWithTimesFitsSmallPanes(t *testing.T) {
	for _, size := range []struct{ w, h int }{{120, 30}, {80, 24}, {40, 12}, {30, 8}, {20, 5}} {
		m := testModel(t)
		var tm tea.Model = m
		tm, _ = tm.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		v := tm.(Model).View()
		for i, l := range strings.Split(strings.TrimRight(v, "\n"), "\n") {
			if w := lipglossWidth(l); w > size.w {
				t.Errorf("%dx%d: line %d is %d wide", size.w, size.h, i, w)
			}
		}
	}
}

// The quote is easy to miss at the head of a query, so the mode is named (T-005).
func TestLiteralModeIsNamedInTheHeader(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 90, 20
	if strings.Contains(m.View(), "literal") {
		t.Error("literal should not be announced for an ordinary query")
	}
	m.query.Text = "'git"
	m.refresh()
	if !strings.Contains(m.View(), "literal") {
		t.Errorf("literal mode is not named:\n%s", m.View())
	}
}
