package search

import (
	"testing"
	"time"

	"github.com/TimelordUK/hit/internal/record"
	"github.com/TimelordUK/hit/internal/store"
)

// The owner typed 'Restart looking for scripts\restart-lucid.ps1 and saw nothing:
// smart-case made the capital R demand an exact match. Case is now ignored by default,
// with smart-case as the opt-in (`[finder] case = "smart"`).
func TestCaseIsIgnoredByDefault(t *testing.T) {
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	h := store.Build([]record.Record{
		{K: record.KindCmd, ID: "A", TS: record.FormatTime(at.Add(-time.Hour)), Cmd: `scripts\restart-lucid.ps1`},
		{K: record.KindCmd, ID: "B", TS: record.FormatTime(at.Add(-time.Hour)), Cmd: `git status`},
	})
	for _, text := range []string{"'Restart", "Restart", "'RESTART-LUCID"} {
		got := cmdsOf(Search(h, Query{Text: text, Scope: ScopeAll, Now: at}))
		if len(got) != 1 || got[0] != `scripts\restart-lucid.ps1` {
			t.Errorf("%q: got %v", text, got)
		}
		if got := Search(h, Query{Text: text, Scope: ScopeAll, Now: at, Case: CaseSmart}); len(got) != 0 {
			t.Errorf("%q with smart-case should demand the capitals: got %v", text, cmdsOf(got))
		}
	}
}

// Ignoring case in what matches does not ignore it in the ranking: the exact-case bonus
// still puts Restart-Service above restart-lucid for "Restart".
func TestExactCaseStillRanksFirst(t *testing.T) {
	exact, _, _ := MatchCase("Restart-Service", "'Restart", CaseIgnore)
	folded, _, _ := MatchCase("restart-lucid", "'Restart", CaseIgnore)
	if exact <= folded {
		t.Errorf("exact case %v should outscore folded %v", exact, folded)
	}
}

func TestMatchCaseModes(t *testing.T) {
	for _, tc := range []struct {
		text, pattern string
		c             Case
		ok            bool
	}{
		{"invoke-restmethod", "IRM", CaseIgnore, true},
		{"invoke-restmethod", "IRM", "", true}, // empty is the default: ignore
		{"invoke-restmethod", "IRM", CaseSmart, false},
		{"Invoke-RestMethod", "irm", CaseSmart, true},
		{"Get-childitem", "'ChildItem", CaseIgnore, true},
		{"Get-childitem", "'ChildItem", CaseSmart, false},
	} {
		if _, _, ok := MatchCase(tc.text, tc.pattern, tc.c); ok != tc.ok {
			t.Errorf("MatchCase(%q, %q, %q) = %v, want %v", tc.text, tc.pattern, tc.c, ok, tc.ok)
		}
	}
}
