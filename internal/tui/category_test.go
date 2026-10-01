package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/TimelordUK/hit/internal/category"
	"github.com/TimelordUK/hit/internal/config"
	"github.com/TimelordUK/hit/internal/record"
	"github.com/TimelordUK/hit/internal/search"
	"github.com/TimelordUK/hit/internal/store"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// The categories under test (C-036): two with colours, one on the default.
const testCategories = `
[[category]]
name     = "git"
color    = "blue"
commands = ["git", "gh"]

[[category]]
name     = "content"
color    = "green"
commands = ["curl", "Expand-Archive"]

[[category]]
name     = "devops"
cwd      = ['C:\ops\**']
`

func testSet(t *testing.T) *category.Set {
	t.Helper()
	s := category.Compile(config.Parse("t.toml", testCategories), category.Paths{Home: `C:\Users\o`, Windows: true})
	if len(s.Problems) > 0 {
		t.Fatal(s.Problems)
	}
	return s
}

func categorised(t *testing.T, filter string) Model {
	t.Helper()
	recs := []record.Record{
		{K: record.KindCmd, ID: "A", TS: ts(50), Cmd: "git status", Cwd: `C:\dev\hit`, Sh: "pwsh", Sid: "s1"},
		{K: record.KindCmd, ID: "B", TS: ts(40), Cmd: "curl https://x", Cwd: `C:\dev\hit`, Sh: "pwsh", Sid: "s1"},
		{K: record.KindCmd, ID: "C", TS: ts(20), Cmd: "ls", Cwd: `C:\dev\hit`, Sh: "pwsh", Sid: "s1"},
		{K: record.KindCmd, ID: "D", TS: ts(30), Cmd: "ls", Cwd: `C:\ops\scripts`, Sh: "pwsh", Sid: "s1"},
		{K: record.KindCmd, ID: "E", TS: ts(10), Cmd: "gh pr list", Cwd: `C:\dev\hit`, Sh: "pwsh", Sid: "s1"},
	}
	// ts(n) is n minutes ago, so gh is the newest and the newest ls ran in C:\dev\hit.
	m := New(store.Build(recs), search.Query{Scope: search.ScopeAll, Sort: search.SortRecent, Now: now})
	m.SetCategories(testSet(t), filter)
	return m
}

func TestAltGCyclesThroughTheCategoriesAndBack(t *testing.T) {
	m := categorised(t, "")
	if got := len(m.Results()); got != 4 { // ls collapses to one row
		t.Fatalf("unfiltered: %v", cmds(m))
	}
	// The first press opens the group picker and changes nothing yet; every press after
	// that steps to the next group, so tapping Alt+G still cycles (T-019).
	m = sendAlt(m, 'g')
	if got := len(m.Results()); got != 4 || !m.picking {
		t.Fatalf("first alt+g should only open the picker: picking=%v %v", m.picking, cmds(m))
	}
	m = sendAlt(m, 'g')
	if got := strings.Join(cmds(m), ","); got != "gh pr list,git status" {
		t.Errorf("git: %s", got)
	}
	m = sendAlt(m, 'g')
	if got := strings.Join(cmds(m), ","); got != "curl https://x" {
		t.Errorf("content: %s", got)
	}
	m = sendAlt(m, 'g')
	if got := strings.Join(cmds(m), ","); got != "ls" {
		t.Errorf("devops: %s", got)
	}
	m = sendAlt(m, 'g')
	if got := len(m.Results()); got != 4 {
		t.Errorf("one more press should be back to everything: %v", cmds(m))
	}
}

// A cwd rule labels a run by where it ran, so the filter works per run: `ls` in C:\ops
// is devops and `ls` in C:\dev\hit is not, and the count says only the devops run.
func TestCategoryFilterCountsOnlyTheRunsItKeeps(t *testing.T) {
	m := categorised(t, "devops")
	res := m.Results()
	if len(res) != 1 || res[0].Count != 1 || res[0].Entry.Cwd != `C:\ops\scripts` {
		t.Errorf("got %+v", res)
	}
}

func TestHeaderNamesTheCategoryFilter(t *testing.T) {
	m := categorised(t, "")
	if v := view(m); !strings.Contains(v, "any group") {
		t.Errorf("unfiltered header should say so:\n%s", v)
	}
	m = sendAlt(sendAlt(m, 'g'), 'g')
	m = send(m, "esc") // leave the picker, keep the group
	if v := view(m); !strings.Contains(v, " git ") || strings.Contains(v, "any group") {
		t.Errorf("filtered header should name git:\n%s", v)
	}
	if v := view(m); !strings.Contains(v, "alt+g group") {
		t.Errorf("status line should teach the key:\n%s", v)
	}
}

// Without a config nothing changes: no header word, no hint, no column.
func TestNoCategoriesLeavesTheFinderAsItWas(t *testing.T) {
	m := elsewhere(t)
	m.SetCategories(category.Compile(config.Config{}, category.Paths{}), "")
	v := view(m)
	if strings.Contains(v, "group") {
		t.Errorf("no categories should mean no group chrome:\n%s", v)
	}
	if got := sendAlt(m, 'g'); len(got.Results()) != len(m.Results()) {
		t.Error("alt+g with no categories should do nothing")
	}
}

func TestEmptyCategoryListSaysWhichKeyMovesOn(t *testing.T) {
	m := categorised(t, "content")
	m.query.Text = "zzz"
	m.refresh()
	if v := view(m); !strings.Contains(v, "alt+g") {
		t.Errorf("empty list in a category should name alt+g:\n%s", v)
	}
}

func TestHitSearchCanStartInACategory(t *testing.T) {
	if got := strings.Join(cmds(categorised(t, "CONTENT")), ","); got != "curl https://x" {
		t.Errorf("a named filter (any case) should apply from the start: %s", got)
	}
	if got := len(categorised(t, "nope").Results()); got != 4 {
		t.Errorf("an unknown name should start unfiltered, got %d", got)
	}
}

// The mark: the category's letter in a column of its own, blank on an uncategorised row.
func TestRowsCarryTheirCategoryMark(t *testing.T) {
	m := categorised(t, "")
	m.width = 60
	s := newStyles()
	for i, r := range m.Results() {
		row := strip(m.renderRow(s, i))
		want := map[string]string{"git status": " G ", "curl https://x": " C ", "gh pr list": " G "}[r.Entry.Cmd]
		if want == "" {
			// ls: its latest run was in C:\dev\hit, where no rule applies, so no mark
			want = "   "
		}
		if !strings.Contains(row, want+r.Entry.Cmd) {
			t.Errorf("row %q: want mark %q before the command", row, want)
		}
	}
}

// The selected row stays one unbroken bar with the mark in it, drawn as a chip so a blue
// letter does not vanish into the blue bar.
func TestSelectedRowWithAMarkIsStillHighlightedAllTheWayAcross(t *testing.T) {
	was := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(was) })
	lipgloss.SetColorProfile(termenv.ANSI256)
	m := categorised(t, "")
	m.width = 60
	m.cursor = 0
	cells := paintedCells(m.renderRow(newStyles(), 0))
	if len(cells) != 60 {
		t.Fatalf("row is %d cells wide, want 60", len(cells))
	}
	for i, p := range cells {
		if !p {
			t.Fatalf("cell %d of the selected row is not highlighted", i)
		}
	}
}

func TestUncategorisedRowsLineUpWithCategorisedOnes(t *testing.T) {
	m := categorised(t, "")
	m.width = 60
	s := newStyles()
	var at []int
	for i, r := range m.Results() {
		row := strip(m.renderRow(s, i))
		at = append(at, utf8.RuneCountInString(row[:strings.Index(row, r.Entry.Cmd)])) // ▸ is 3 bytes
	}
	for _, a := range at[1:] {
		if a != at[0] {
			t.Errorf("commands start at different columns: %v", at)
		}
	}
}

func strip(s string) string {
	var b strings.Builder
	r := []rune(s)
	for i := 0; i < len(r); {
		if r[i] == '\x1b' && i+1 < len(r) && r[i+1] == '[' {
			j := i + 2
			for j < len(r) && r[j] != 'm' {
				j++
			}
			i = j + 1
			continue
		}
		b.WriteRune(r[i])
		i++
	}
	return b.String()
}

// Alt+G's filter was drawn as a badge, black on magenta, and in schemes where black is a
// dark purple it could hardly be read (owner, 2026-09-29). It is now the category's own
// colour as bold text on no background: a yellow devops reads as a yellow "devops".
func TestActiveCategoryIsItsOwnColourWithNoBackground(t *testing.T) {
	s := newStyles()
	if s.noColor {
		t.Skip("NO_COLOR is set")
	}
	st := s.categoryMode("yellow")
	if st.GetForeground() != ansiColor("yellow") {
		t.Errorf("foreground = %v, want the category's yellow", st.GetForeground())
	}
	if _, none := st.GetBackground().(lipgloss.NoColor); !none {
		t.Errorf("background = %v, want none", st.GetBackground())
	}
	if !st.GetBold() {
		t.Error("should be bold, to stand out from the grey mode words")
	}
	if _, none := s.categoryMode("").GetForeground().(lipgloss.NoColor); !none {
		t.Error("a category with no colour should use the terminal's own text colour")
	}
}

// The same complaint, a second time, about the modes the category fix did not cover:
// sort, scope, literal and ok-only were still a badge, black on magenta, and on the
// owner's work, laptop and home machines alike they read as two shades of purple
// (owner, 2026-10-01). Backgrounds in the header are the recurring mistake — a scheme
// only has to move its purples for black-on-colour to vanish — so this asserts the rule
// rather than the one style: an active mode is a hue change on no background (T-021).
func TestActiveModesHaveNoBackground(t *testing.T) {
	s := newStyles()
	if s.noColor {
		t.Skip("NO_COLOR is set")
	}
	if _, none := s.modeOn.GetBackground().(lipgloss.NoColor); !none {
		t.Errorf("background = %v, want none: a filled badge is what made this unreadable",
			s.modeOn.GetBackground())
	}
	if !s.modeOn.GetBold() {
		t.Error("an active mode should be bold, to carry the emphasis the background used to")
	}
	// A hue change, not a shade of the colour it sits next to: two purples was the bug.
	if s.modeOn.GetForeground() == s.scope.GetForeground() {
		t.Error("an active mode is the same colour as an inactive one, so the mode cannot be seen")
	}
}

// Every mode the header can draw must come out readable, so this walks the real header
// with each one switched on and checks none of them paints a background.
func TestNoHeaderModeDrawsABackground(t *testing.T) {
	if newStyles().noColor {
		t.Skip("NO_COLOR is set")
	}
	// The profile is pinned, and put back afterwards. Left alone, lipgloss looks at the
	// test binary's output, finds no terminal, and renders plain text — so this test would
	// pass by seeing no escapes at all, which is worse than not having it.
	was := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(was) })
	lipgloss.SetColorProfile(termenv.ANSI256)
	for _, tc := range []struct {
		name string
		set  func(m *Model)
	}{
		{"sort recent", func(m *Model) { m.query.Sort = search.SortRecent }},
		{"dir scope", func(m *Model) { m.query.Scope = search.ScopeDir }},
		{"literal", func(m *Model) { m.query.Text = "'git" }},
		{"ok-only", func(m *Model) { m.query.HideFailed = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := testModel(t)
			m.width = 120
			tc.set(&m)
			m.refresh()
			header := strings.SplitN(m.View(), "\n", 2)[0]
			if bg := firstBackground(header); bg != "" {
				t.Errorf("the header paints a background (%s), which is what made the modes "+
					"unreadable:\n%s", bg, strings.ReplaceAll(header, "\x1b", "ESC"))
			}
		})
	}
}

// firstBackground returns the first background-setting SGR parameter it finds in s, or ""
// if there is none. Reverse video counts: it paints a background by swapping.
func firstBackground(s string) string {
	runes := []rune(s)
	for i := 0; i < len(runes); {
		if runes[i] != '\x1b' || i+1 >= len(runes) || runes[i+1] != '[' {
			i++
			continue
		}
		j := i + 2
		for j < len(runes) && runes[j] != 'm' {
			j++
		}
		if j >= len(runes) {
			break
		}
		for _, f := range strings.Split(string(runes[i+2:j]), ";") {
			switch {
			case f == "7", f == "48":
				return f
			case len(f) == 2 && f[0] == '4' && f[1] >= '0' && f[1] <= '7':
				return f
			case len(f) == 3 && f[0] == '1' && f[1] == '0' && f[2] >= '0' && f[2] <= '7':
				return f
			}
		}
		i = j + 1
	}
	return ""
}
