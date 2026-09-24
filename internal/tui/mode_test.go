package tui

import (
	"strings"
	"testing"

	"github.com/TimelordUK/hit/internal/record"
	"github.com/TimelordUK/hit/internal/search"
	"github.com/TimelordUK/hit/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// alt chords arrive as runes with Alt set; the finder drops the ones it doesn't claim
// (F-027), so they have to be sent the way bubbletea delivers them.
func altKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: true}
}

func sendAlt(m Model, r rune) Model {
	tm, _ := m.Update(altKey(r))
	return tm.(Model)
}

// view renders the finder as text. The escapes are stripped because these tests are about
// what the header says, not how it is coloured — and because the colour profile is global
// and another test in this package pins it (highlight_test.go).
func view(m Model) string {
	out := m.View()
	var b strings.Builder
	runes := []rune(out)
	for i := 0; i < len(runes); {
		if runes[i] == '\x1b' && i+1 < len(runes) && runes[i+1] == '[' {
			j := i + 2
			for j < len(runes) && runes[j] != 'm' {
				j++
			}
			i = j + 1
			continue
		}
		b.WriteRune(runes[i])
		i++
	}
	return b.String()
}

// elsewhere is the finder as it is opened from a directory nothing was ever run in — the
// case where a directory filter has nothing to show, which is most directories.
func elsewhere(t *testing.T) Model {
	t.Helper()
	recs := []record.Record{
		{K: record.KindCmd, ID: "A", TS: ts(50), Cmd: "git status", Cwd: `C:\dev\hit`, Sh: "pwsh", Sid: "s1", Host: "BOX"},
		{K: record.KindCmd, ID: "B", TS: ts(40), Cmd: "go test ./...", Cwd: `C:\dev\hit`, Sh: "pwsh", Sid: "s1", Host: "BOX"},
	}
	return New(store.Build(recs), search.Query{
		Scope: search.ScopeAll, Cwd: `C:\somewhere\else`, Session: "s9", Host: "BOX", Shell: "pwsh", Now: now,
	})
}

// The reported bug: ^r from `all` lands on the narrowest scope, and in a directory with no
// history that empties the list. Getting back out took three more presses through two more
// scopes that were empty too, so the finder looked broken and had to be abandoned.
//
// The cycle itself was correct — the old test asserted the scope field changed and passed
// throughout. What it never asserted was that the rows come back, which is the only thing
// the person pressing the key cares about.
func TestScopeCycleComesBackToAFullList(t *testing.T) {
	m := elsewhere(t)
	want := len(m.Results())
	if want == 0 {
		t.Fatal("the fixture starts empty, so the test proves nothing")
	}
	for i := 0; i < len(search.Scopes); i++ {
		m = send(m, "ctrl+r")
	}
	if m.Query().Scope != search.ScopeAll {
		t.Errorf("a full cycle ended on scope %q, want %q", m.Query().Scope, search.ScopeAll)
	}
	if got := len(m.Results()); got != want {
		t.Errorf("a full cycle came back with %d rows, want %d", got, want)
	}
}

// alt+d is one key in and the same key out (T-008), so a directory filter that turns out
// to be empty is never a dead end.
func TestDirToggleRestoresTheScopeItFound(t *testing.T) {
	m := elsewhere(t)
	want := len(m.Results())

	m = sendAlt(m, 'd')
	if m.Query().Scope != search.ScopeDir {
		t.Fatalf("alt+d gave scope %q, want %q", m.Query().Scope, search.ScopeDir)
	}
	if len(m.Results()) != 0 {
		t.Fatalf("this fixture's directory should have nothing in it, got %d rows", len(m.Results()))
	}

	m = sendAlt(m, 'd')
	if m.Query().Scope != search.ScopeAll {
		t.Errorf("alt+d again gave scope %q, want %q", m.Query().Scope, search.ScopeAll)
	}
	if got := len(m.Results()); got != want {
		t.Errorf("alt+d again came back with %d rows, want %d", got, want)
	}
}

// Toggling out returns to whatever scope was in force, not to a hard-coded `all`.
func TestDirToggleRemembersANarrowerScope(t *testing.T) {
	m := elsewhere(t)
	m.query.Scope = search.ScopeHost
	m.refresh()
	m = sendAlt(m, 'd')
	m = sendAlt(m, 'd')
	if m.Query().Scope != search.ScopeHost {
		t.Errorf("alt+d round trip gave scope %q, want %q", m.Query().Scope, search.ScopeHost)
	}
}

// Reaching `dir` by the cycle and leaving by alt+d must still work: there is no previous
// scope recorded, so it falls back to showing everything rather than to nothing.
func TestDirToggleLeavesADirScopeItDidNotSet(t *testing.T) {
	m := elsewhere(t)
	m.query.Scope = search.ScopeDir // reached by the cycle, so nothing was remembered
	m.refresh()
	m = sendAlt(m, 'd')
	if m.Query().Scope != search.ScopeAll {
		t.Errorf("alt+d gave scope %q, want %q", m.Query().Scope, search.ScopeAll)
	}
	if len(m.Results()) == 0 {
		t.Error("alt+d left the list empty")
	}
}

// alt+s flips the order and back (T-009). Ranking answers "what do I most likely want";
// recent answers "what did I just run", and they are different questions.
func TestSortToggle(t *testing.T) {
	m := testModel(t)
	if got := m.Query().Sort; got != "" && got != search.SortRank {
		t.Fatalf("the finder opens on sort %q, want rank", got)
	}
	m = sendAlt(m, 's')
	if m.Query().Sort != search.SortRecent {
		t.Fatalf("alt+s gave sort %q, want %q", m.Query().Sort, search.SortRecent)
	}
	// The most recent command in the fixture is the one that failed, which ranking pushes
	// down and recency must not.
	if got := cmds(m)[0]; got != "git stash pop" {
		t.Errorf("sorted by recency, the first row is %q, want the newest command", got)
	}
	m = sendAlt(m, 's')
	if m.Query().Sort != search.SortRank {
		t.Errorf("alt+s again gave sort %q, want %q", m.Query().Sort, search.SortRank)
	}
}

// Switching the order must not change which commands match, only their order.
func TestSortDoesNotChangeWhatMatches(t *testing.T) {
	m := send(testModel(t), "git")
	before := len(m.Results())
	m = sendAlt(m, 's')
	if got := len(m.Results()); got != before {
		t.Errorf("alt+s changed the matches: %d → %d", before, got)
	}
}

// The header says which mode the finder is in. An empty list is otherwise indistinguishable
// from a broken finder, which is exactly how the directory scope was reported.
func TestHeaderNamesTheModes(t *testing.T) {
	m := elsewhere(t)
	m.width = 100
	if got := view(m); !strings.Contains(got, "all") || !strings.Contains(got, "rank") {
		t.Errorf("the header does not name the default scope and sort:\n%s", got)
	}
	m = sendAlt(m, 'd')
	if got := view(m); !strings.Contains(got, "dir else") {
		t.Errorf("the header does not name the directory being filtered on:\n%s", got)
	}
	m = sendAlt(m, 's')
	if got := view(m); !strings.Contains(got, "recent") {
		t.Errorf("the header does not name the sort order:\n%s", got)
	}
}

// An empty list says why it is empty and which key widens it. Without that it reads as a
// finder that has stopped working.
func TestEmptyListSaysWhichKeyWidensIt(t *testing.T) {
	m := elsewhere(t)
	m.width = 100
	m = sendAlt(m, 'd')
	got := view(m)
	if !strings.Contains(got, "nothing recorded in else") {
		t.Errorf("the empty list does not say why it is empty:\n%s", got)
	}
	if !strings.Contains(got, "alt+d") {
		t.Errorf("the empty list does not say which key gets you out:\n%s", got)
	}
}

func TestPathLeaf(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`C:\Users\sjame\dev\hit`, "hit"},
		{`C:\Users\sjame\dev\hit\`, "hit"},
		{`C:/Users/sjame/dev/hit`, "hit"},
		{`\\elastic-prod-1\logs`, "logs"},
		{`C:\`, `C:`},
		{"", ""},
	} {
		if got := pathLeaf(tc.in); got != tc.want {
			t.Errorf("pathLeaf(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
