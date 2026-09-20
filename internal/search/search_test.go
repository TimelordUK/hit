package search

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TimelordUK/hit/internal/store"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func TestMatch(t *testing.T) {
	tests := []struct {
		text, pattern string
		ok            bool
	}{
		{"git status", "gs", true},          // subsequence across words
		{"git status", "status", true},      // substring
		{"git status", "tats", true},        // inside a word
		{"git status", "sg", false},         // wrong order
		{"git status", "gitx", false},       // missing rune
		{"Invoke-RestMethod", "irm", true},  // initials
		{"invoke-restmethod", "IRM", false}, // smart-case: upper must match exactly
		{"Invoke-RestMethod", "IM", true},
		{"git status", "", true},
		{"café 🦀", "🦀", true},
	}
	for _, tc := range tests {
		_, _, ok := Match(tc.text, tc.pattern)
		if ok != tc.ok {
			t.Errorf("Match(%q, %q) ok = %v, want %v", tc.text, tc.pattern, ok, tc.ok)
		}
	}
}

func TestMatchPositionsAreRuneIndexes(t *testing.T) {
	_, m, _ := Match("café au 🦀 lait", "🦀l")
	if len(m) != 2 || m[0] != 8 || m[1] != 10 {
		t.Fatalf("positions = %v, want [8 10]", m)
	}
}

func TestMatchRanksTheObviousCandidateFirst(t *testing.T) {
	tests := []struct {
		pattern     string
		better, not string
	}{
		{"irm", "irm https://x", "Invoke-RestMethod -Uri https://x"},   // whole word beats initials
		{"git", "git status", "legit stuff"},                           // word start beats mid-word
		{"gst", "git status", "go get stuff tomorrow"},                 // tighter span
		{"test", "go test ./...", "go build ./... # remember to test"}, // earlier match
		{"elastic", "curl elastic-prod-1", "curl e-l-a-s-t-i-c"},       // contiguous beats scattered
		{"post", "-Method post", "-Method Post"},                       // exact case preferred
	}
	for _, tc := range tests {
		b, _, okB := Match(tc.better, tc.pattern)
		n, _, okN := Match(tc.not, tc.pattern)
		if !okB || !okN {
			t.Fatalf("%q: both should match (%v, %v)", tc.pattern, okB, okN)
		}
		if b <= n {
			t.Errorf("Match(%q): %q scored %.3f, want more than %q at %.3f", tc.pattern, tc.better, b, tc.not, n)
		}
	}
}

func loadHistory(t *testing.T) *store.History {
	t.Helper()
	recs, st, err := store.ReadFile(filepath.Join("testdata", "history.jsonl"))
	if err != nil || st.Corrupt != 0 {
		t.Fatalf("fixture: %+v %v", st, err)
	}
	return store.Build(recs)
}

var fixtureNow = time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

// Golden files keep ranking changes visible: `go test ./internal/search -update` re-blesses.
func TestRankingGolden(t *testing.T) {
	h := loadHistory(t)
	base := Query{Cwd: `C:\dev\hit`, Session: "s3", Host: "box1", Shell: "pwsh", Now: fixtureNow}

	cases := []struct {
		name string
		q    Query
	}{
		{"empty-all", with(base, func(q *Query) { q.Scope = ScopeAll })},
		{"empty-dir", with(base, func(q *Query) { q.Scope = ScopeDir })},
		{"empty-session", with(base, func(q *Query) { q.Scope = ScopeSession })},
		{"elastic-all", with(base, func(q *Query) { q.Scope = ScopeAll; q.Text = "elastic" })},
		{"irm-all", with(base, func(q *Query) { q.Scope = ScopeAll; q.Text = "irm" })},
		{"gs-all", with(base, func(q *Query) { q.Scope = ScopeAll; q.Text = "gs" })},
		{"test-all-hide-failed", with(base, func(q *Query) { q.Scope = ScopeAll; q.Text = "test"; q.HideFailed = true })},
		{"todo-any-shell", with(base, func(q *Query) { q.Scope = ScopeAll; q.Text = "todo"; q.Shell = "" })},
		{"nomatch", with(base, func(q *Query) { q.Scope = ScopeAll; q.Text = "zzzz" })},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			for i, r := range Search(h, tc.q) {
				exit := "?"
				if r.Entry.Exit != nil {
					exit = fmt.Sprint(*r.Entry.Exit)
				}
				fmt.Fprintf(&b, "%d  %.3f  x%d  exit=%s  %s\n     %s\n", i+1, r.Score, r.Count, exit,
					r.Entry.Cwd, strings.ReplaceAll(r.Entry.Cmd, "\n", "⏎"))
			}
			compareGolden(t, tc.name, b.String())
		})
	}
}

func with(q Query, f func(*Query)) Query {
	f(&q)
	return q
}

func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".txt")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run: go test ./internal/search -update)", err)
	}
	if got != string(want) {
		t.Errorf("golden %s differs:\n--- got ---\n%s--- want ---\n%s", name, got, want)
	}
}

func TestScopesAndCollapsing(t *testing.T) {
	h := loadHistory(t)
	base := Query{Cwd: `C:\dev\hit`, Session: "s3", Host: "box1", Shell: "pwsh", Now: fixtureNow, Scope: ScopeAll}

	t.Run("duplicates collapse with a run count and the latest run", func(t *testing.T) {
		got := Search(h, with(base, func(q *Query) { q.Text = "RestMethod -Uri https://elastic-prod-1" }))
		if len(got) != 1 {
			t.Fatalf("got %d results, want 1", len(got))
		}
		if got[0].Count != 2 {
			t.Errorf("count = %d, want 2", got[0].Count)
		}
		if got[0].Entry.Cwd != `C:\dev\hit` { // the more recent of the two runs
			t.Errorf("cwd = %q", got[0].Entry.Cwd)
		}
	})

	t.Run("dir scope ignores case and a trailing separator", func(t *testing.T) {
		got := Search(h, with(base, func(q *Query) { q.Scope = ScopeDir; q.Cwd = `c:/dev/hit\` }))
		var found bool
		for _, r := range got {
			if r.Entry.Cmd == "code ." { // recorded with a trailing backslash
				found = true
			}
			if !samePath(r.Entry.Cwd, `C:\dev\hit`) {
				t.Errorf("out of scope: %q", r.Entry.Cwd)
			}
		}
		if !found {
			t.Error(`"code ." (cwd "C:\dev\hit\") not found in dir scope`)
		}
	})

	t.Run("session scope", func(t *testing.T) {
		for _, r := range Search(h, with(base, func(q *Query) { q.Scope = ScopeSession })) {
			if r.Entry.Session != "s3" {
				t.Errorf("session = %q", r.Entry.Session)
			}
		}
	})

	t.Run("shell family filter, and none when empty", func(t *testing.T) {
		for _, r := range Search(h, base) {
			if r.Entry.Shell == "zsh" {
				t.Errorf("zsh command leaked into pwsh scope: %q", r.Entry.Cmd)
			}
		}
		var found bool
		for _, r := range Search(h, with(base, func(q *Query) { q.Shell = "" })) {
			found = found || r.Entry.Shell == "zsh"
		}
		if !found {
			t.Error("zsh command missing when no shell filter is set")
		}
	})

	t.Run("deleted commands never appear", func(t *testing.T) {
		if got := Search(h, with(base, func(q *Query) { q.Text = "deleted" })); len(got) != 0 {
			t.Errorf("tombstoned command returned: %+v", got[0].Entry)
		}
	})

	t.Run("runs in this directory score higher, wherever the latest run was", func(t *testing.T) {
		here := Search(h, with(base, func(q *Query) { q.Text = "git status" }))
		away := Search(h, with(base, func(q *Query) { q.Text = "git status"; q.Cwd = `C:\dev\other` }))
		if len(here) != 1 || len(away) != 1 {
			t.Fatalf("collapsing broken: %d here, %d away", len(here), len(away))
		}
		// Latest run was in sql-cli, but it has also been run in the query's directory.
		if here[0].Entry.Cwd != `C:\dev\sql-cli` || !here[0].InCwd || away[0].InCwd {
			t.Errorf("here=%+v away.InCwd=%v", here[0], away[0].InCwd)
		}
		if here[0].Score <= away[0].Score {
			t.Errorf("same-dir bonus missing: %.3f vs %.3f", here[0].Score, away[0].Score)
		}
	})

	t.Run("limit", func(t *testing.T) {
		if got := Search(h, with(base, func(q *Query) { q.Limit = 3 })); len(got) != 3 {
			t.Errorf("got %d, want 3", len(got))
		}
	})

	t.Run("hide failed", func(t *testing.T) {
		for _, r := range Search(h, with(base, func(q *Query) { q.HideFailed = true })) {
			if r.Entry.Exit != nil && *r.Entry.Exit != 0 {
				t.Errorf("failed command shown: %+v", r.Entry)
			}
		}
	})
}
