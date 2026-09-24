package search

import (
	"testing"
	"time"

	"github.com/TimelordUK/hit/internal/record"
	"github.com/TimelordUK/hit/internal/store"
)

// Ranking answers "what do I most likely want next"; recency answers "what did I just
// run". Frecency actively fights the second question, because a command run two hundred
// times outranks the one run once, five minutes ago — which is the one you are reaching
// for when you walk back through the last few commands (C-032).
func sortFixture(t *testing.T, at time.Time) *store.History {
	t.Helper()
	ts := func(mins int) string { return record.FormatTime(at.Add(-time.Duration(mins) * time.Minute)) }
	recs := []record.Record{
		// Run many times and long ago: ranking's favourite, recency's last.
		{K: record.KindCmd, ID: "o1", TS: ts(600), Cmd: "git status", Cwd: `C:\dev\hit`, Sh: "pwsh", Sid: "s1"},
		{K: record.KindCmd, ID: "o2", TS: ts(500), Cmd: "git status", Cwd: `C:\dev\hit`, Sh: "pwsh", Sid: "s1"},
		{K: record.KindCmd, ID: "o3", TS: ts(400), Cmd: "git status", Cwd: `C:\dev\hit`, Sh: "pwsh", Sid: "s1"},
		{K: record.KindCmd, ID: "o4", TS: ts(300), Cmd: "git status", Cwd: `C:\dev\hit`, Sh: "pwsh", Sid: "s1"},
		// Run once, a minute ago.
		{K: record.KindCmd, ID: "n1", TS: ts(1), Cmd: "git bisect start", Cwd: `C:\dev\hit`, Sh: "pwsh", Sid: "s1"},
	}
	return store.Build(recs)
}

// The cycle narrows a step at a time. Running it the other way was what made ^r feel
// broken: one press from `all` landed on the narrowest scope, emptied the list in any
// directory with no history, and took three more presses to undo (T-008). The order is
// data, so this is the place it is pinned down.
func TestScopesNarrowOneStepAtATime(t *testing.T) {
	if got := Scopes[0]; got != ScopeAll {
		t.Errorf("the cycle starts at %q, want the widest scope %q", got, ScopeAll)
	}
	if got := Scopes[len(Scopes)-1]; got != ScopeDir {
		t.Errorf("the cycle ends at %q, want the narrowest scope %q", got, ScopeDir)
	}
	// Every scope appears exactly once, or a press goes somewhere unreachable.
	seen := map[Scope]bool{}
	for _, s := range Scopes {
		if seen[s] {
			t.Errorf("scope %q appears twice in the cycle", s)
		}
		seen[s] = true
	}
	for _, s := range []Scope{ScopeAll, ScopeHost, ScopeSession, ScopeDir} {
		if !seen[s] {
			t.Errorf("scope %q is not reachable by the cycle", s)
		}
	}
}

func cmdsOf(rs []Result) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Entry.Cmd
	}
	return out
}

func TestSortRecentPutsTheNewestFirst(t *testing.T) {
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	h := sortFixture(t, at)

	ranked := cmdsOf(Search(h, Query{Text: "git", Scope: ScopeAll, Now: at}))
	if ranked[0] != "git status" {
		t.Errorf("ranked first = %q, want the much-used command", ranked[0])
	}

	recent := cmdsOf(Search(h, Query{Text: "git", Scope: ScopeAll, Sort: SortRecent, Now: at}))
	if recent[0] != "git bisect start" {
		t.Errorf("recent first = %q, want the newest command", recent[0])
	}
}

// An empty Sort is the default order, so a caller that never heard of C-032 is unaffected.
func TestEmptySortIsRank(t *testing.T) {
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	h := sortFixture(t, at)
	a := cmdsOf(Search(h, Query{Text: "git", Scope: ScopeAll, Now: at}))
	b := cmdsOf(Search(h, Query{Text: "git", Scope: ScopeAll, Sort: SortRank, Now: at}))
	if len(a) != len(b) {
		t.Fatalf("%d results against %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("row %d: %q against %q", i, a[i], b[i])
		}
	}
}

// The order is an order, not a filter: the same commands match either way. Scope, the
// query and the failed-exit filter all still apply.
func TestSortDoesNotChangeWhatMatches(t *testing.T) {
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	h := sortFixture(t, at)
	for _, q := range []Query{
		{Text: "git", Scope: ScopeAll, Now: at},
		{Text: "bisect", Scope: ScopeAll, Now: at},
		{Text: "", Scope: ScopeDir, Cwd: `C:\dev\hit`, Now: at},
	} {
		want := len(Search(h, q))
		q.Sort = SortRecent
		if got := len(Search(h, q)); got != want {
			t.Errorf("query %+v: %d matches by recency against %d by rank", q, got, want)
		}
	}
}

// Duplicates still collapse: "git status" is one row carrying its run count, whichever
// order is in force, and the row is dated by its most recent run.
func TestSortRecentStillCollapsesDuplicates(t *testing.T) {
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	res := Search(sortFixture(t, at), Query{Text: "status", Scope: ScopeAll, Sort: SortRecent, Now: at})
	if len(res) != 1 {
		t.Fatalf("%d rows, want one collapsed row: %v", len(res), cmdsOf(res))
	}
	if res[0].Count != 4 {
		t.Errorf("run count = %d, want 4", res[0].Count)
	}
	if res[0].Entry.ID != "o4" {
		t.Errorf("row dated by entry %q, want the most recent run", res[0].Entry.ID)
	}
}

// A record with no usable timestamp sorts last rather than first, which is what a zero
// time would do if it were treated as a real one.
func TestSortRecentPutsUndatedEntriesLast(t *testing.T) {
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	recs := []record.Record{
		{K: record.KindCmd, ID: "u", Cmd: "git undated", Cwd: `C:\dev\hit`, Sh: "pwsh", Sid: "s1"},
		{K: record.KindCmd, ID: "d", TS: record.FormatTime(at.Add(-time.Hour)), Cmd: "git dated", Cwd: `C:\dev\hit`, Sh: "pwsh", Sid: "s1"},
	}
	got := cmdsOf(Search(store.Build(recs), Query{Text: "git", Scope: ScopeAll, Sort: SortRecent, Now: at}))
	if len(got) != 2 || got[0] != "git dated" {
		t.Errorf("order = %v, want the dated command first", got)
	}
}
