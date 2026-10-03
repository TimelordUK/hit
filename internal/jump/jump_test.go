package jump

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TimelordUK/hit/internal/store"
)

// home is where the tests stand. Paths under it are real folders in testdata/home (the
// smoke tree), so step 0 checks a real disk; anything else, a share or another drive,
// exists only as history.
const home = `C:\Users\owner`

// exists maps a path under home onto the smoke tree. It answers on Linux too, so the
// Windows rules are tested on every CI runner.
func exists(t *testing.T) func(string) bool {
	root, err := filepath.Abs(filepath.Join("testdata", "home"))
	if err != nil {
		t.Fatal(err)
	}
	return func(dir string) bool {
		rel, ok := strings.CutPrefix(strings.ToLower(dir), strings.ToLower(home))
		if !ok {
			return false
		}
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(strings.ReplaceAll(rel, `\`, "/"))))
		return err == nil && info.IsDir()
	}
}

func tilde(p string) string {
	if p == "~" || strings.HasPrefix(p, `~\`) {
		return home + p[1:]
	}
	return p
}

// world is the owner's day, renamed: one big project with a sibling that is visited far
// more, the personal-docs repo most devops runs from, and a few shares. The score is the
// frecency a directory would have; only its order matters.
var world = []Candidate{
	{Dir: `~\dev\trd-platform`, Score: 5},
	{Dir: `~\dev\trd-platform-launch`, Score: 40},
	{Dir: `~\dev\trd-platform\docs`, Score: 3},
	{Dir: `~\dev\trd-platform\source`, Score: 6},
	{Dir: `~\dev\trd-platform\source\ui`, Score: 4},
	{Dir: `~\dev\trd-platform\source\messaging`, Score: 2},
	{Dir: `~\dev\trd-platform\source\core`, Score: 8},
	{Dir: `~\dev\trd-platform\source\test`, Score: 1},
	{Dir: `~\dev\trd-platform\data\logs`, Score: 2},
	{Dir: `~\dev\trd-platform-launch\logs`, Score: 30},
	{Dir: `~\dev\personal-docs`, Score: 20},
	{Dir: `~\dev\personal-docs\scripts`, Score: 25},
	{Dir: `~\dev\personal-docs\elastic`, Score: 10},
	{Dir: `\\devserv001\logs`, Score: 50},
	{Dir: `\\devserv002\logs`, Score: 5},
	{Dir: `\\symbolserver\symbols`, Score: 3},
	{Dir: `D:\data\logs`, Score: 12},
}

// TestResolve is the tuning table (DESIGN §8.1). A bad jump from daily use becomes a row
// here before the rules change.
func TestResolve(t *testing.T) {
	cases := []struct {
		name  string
		cwd   string
		query string
		world []Candidate // nil: the owner's world
		want  string      // "" when there is no jump and cd is plain Set-Location
		step  Step
		exact bool
	}{
		// C-035: zoxide's daily failure.
		{name: "exact name beats frecency", cwd: `~`, query: `trd-platform`,
			want: `~\dev\trd-platform`, step: Below, exact: true},
		{name: "exact name beats frecency from a sibling", cwd: `~\dev\personal-docs`, query: `trd-platform`,
			want: `~\dev\trd-platform`, step: Nearest, exact: true},
		{name: "case is ignored", cwd: `~`, query: `TRD-Platform`,
			want: `~\dev\trd-platform`, step: Below, exact: true},
		{name: "substring only: frecency decides", cwd: `~`, query: `platform`,
			want: `~\dev\trd-platform-launch`, step: Below},

		// Local beats distant.
		{name: "a real folder wins although never visited", cwd: `~\dev\trd-platform`, query: `logs`,
			want: `logs`, step: Real},
		{name: "the project's logs, not the busier ones elsewhere", cwd: `~\dev\trd-platform\source\core`, query: `logs`,
			want: `~\dev\trd-platform\data\logs`, step: Nearest, exact: true},
		{name: "a sibling subsystem by substring", cwd: `~\dev\trd-platform\source\ui`, query: `mess`,
			want: `~\dev\trd-platform\source\messaging`, step: Nearest},
		{name: "below the current folder", cwd: `~\dev\trd-platform`, query: `core`,
			want: `~\dev\trd-platform\source\core`, step: Below, exact: true},
		{name: "the project root is near", cwd: `~\dev\trd-platform\source\core`, query: `trd`,
			want: `~\dev\trd-platform`, step: Nearest},
		{name: "a sibling repo before anything further", cwd: `~\dev\trd-platform\source\core`, query: `scripts`,
			want: `~\dev\personal-docs\scripts`, step: Nearest, exact: true},
		{name: "near beats another drive", cwd: `~`, query: `data logs`,
			want: `~\dev\trd-platform\data\logs`, step: Below, exact: true},

		// Shares: the host is a segment, matched by substring.
		{name: "share by host and folder", cwd: `~\dev\trd-platform`, query: `devs logs`,
			want: `\\devserv001\logs`, step: Anywhere, exact: true},
		{name: "host digits pick the other server", cwd: `~\dev\trd-platform`, query: `002 logs`,
			want: `\\devserv002\logs`, step: Anywhere, exact: true},
		{name: "anywhere only when nothing is nearer", cwd: `~\dev\personal-docs`, query: `symbols`,
			want: `\\symbolserver\symbols`, step: Anywhere, exact: true},

		// Terms.
		{name: "a separator in a term splits it", cwd: `~`, query: `source\core`,
			want: `~\dev\trd-platform\source\core`, step: Below, exact: true},
		{name: "the last term must match the last segment", cwd: `~`, query: `trd-platform docs`,
			want: `~\dev\trd-platform\docs`, step: Below, exact: true},
		{name: "terms in order", cwd: `~`, query: `docs trd-platform`},

		// Step 0: what Set-Location does anyway, never searched.
		{name: "real relative path with a separator", cwd: `~\dev`, query: `trd-platform\source`,
			want: `trd-platform\source`, step: Real},
		{name: "UNC path", cwd: `~`, query: `\\symbolserver\symbols`, want: `\\symbolserver\symbols`, step: Real},
		{name: "drive path", cwd: `~`, query: `D:\data`, want: `D:\data`, step: Real},
		{name: "parent", cwd: `~\dev`, query: `..`, want: `..`, step: Real},
		{name: "back", cwd: `~\dev`, query: `-`, want: `-`, step: Real},
		{name: "home", cwd: `~\dev`, query: `~`, want: `~`, step: Real},
		{name: "dot-relative", cwd: `~\dev`, query: `.\nothing-here`, want: `.\nothing-here`, step: Real},

		// No jump: cd falls through to Set-Location, which reports the error.
		{name: "nothing matches", cwd: `~`, query: `nowhere`},
		{name: "never jump to where you stand", cwd: `~\dev\personal-docs\scripts`, query: `scripts`},
		{name: "no history, no jump", cwd: `~`, query: `trd-platform`, world: []Candidate{}},
	}

	opts := Options{Windows: true, Exists: exists(t)}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := tc.world
			if w == nil {
				w = world
			}
			cands := make([]Candidate, len(w))
			for i, c := range w {
				cands[i] = Candidate{Dir: tilde(c.Dir), Score: c.Score}
			}
			got, ok := Resolve(cands, tilde(tc.cwd), strings.Fields(tc.query), opts)
			if tc.want == "" {
				if ok {
					t.Fatalf("cd %s from %s: jumped to %s (%s), want no jump", tc.query, tc.cwd, got.Dir, got.Step)
				}
				return
			}
			want := tc.want
			if tc.step != Real {
				want = tilde(want)
			}
			if !ok {
				t.Fatalf("cd %s from %s: no jump, want %s", tc.query, tc.cwd, want)
			}
			if got.Dir != want || got.Step != tc.step || got.Exact != tc.exact {
				t.Errorf("cd %s from %s:\n got  %s (%s, exact=%v)\n want %s (%s, exact=%v)",
					tc.query, tc.cwd, got.Dir, got.Step, got.Exact, want, tc.step, tc.exact)
			}
		})
	}
}

// TestCandidates: a jump candidate is where a jump-category command took you, read from
// the cd record the prompt writes after it in the same session.
func TestCandidates(t *testing.T) {
	t0 := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	h := &store.History{
		Entries: []store.Entry{
			{Time: at(10), Session: "a", Cwd: home, Cmd: `cd dev\trd-platform`},
			{Time: at(20), Session: "a", Cwd: `C:\Users\owner\dev\trd-platform`, Cmd: `.\build.ps1`},          // a script moved us
			{Time: at(30), Session: "a", Cwd: `C:\Users\owner\dev\trd-platform\source\ui`, Cmd: `cd nowhere`}, // failed: no cd record
			{Time: at(40), Session: "a", Cwd: `C:\Users\owner\dev\trd-platform\source\ui`, Cmd: `ls`},
			{Time: at(50), Session: "a", Cwd: `C:\Users\owner\dev\trd-platform\source\ui`, Cmd: `Set-Location \\devserv001\logs`},
			{Time: at(51), Session: "b", Cwd: home, Cmd: `cd \\devserv002\logs`}, // another shell, between a's cmd and its cd
			{Time: at(70), Session: "a", Cwd: `\\devserv001\logs`, Cmd: `cd ~`},
			{Time: at(80), Session: "a", Cwd: home, Cmd: `cd \\DEVSERV001\logs\`},
		},
		Visits: []store.Visit{
			{Time: at(0), Session: "a", Dir: home}, // the shell started here
			{Time: at(11), Session: "a", Dir: `C:\Users\owner\dev\trd-platform`},
			{Time: at(21), Session: "a", Dir: `C:\Users\owner\dev\trd-platform\source\ui`},
			{Time: at(52), Session: "b", Dir: `\\devserv002\logs`},
			{Time: at(53), Session: "a", Dir: `\\devserv001\logs`},
			{Time: at(71), Session: "a", Dir: home},
			{Time: at(81), Session: "a", Dir: `\\DEVSERV001\logs\`},
		},
	}
	isJump := func(cmd, cwd string) bool {
		w, _, _ := strings.Cut(strings.ToLower(cmd), " ")
		return w == "cd" || w == "set-location"
	}
	got := Candidates(h, isJump, Options{Windows: true}, at(100))

	byDir := map[string]Candidate{}
	for _, c := range got {
		byDir[c.Dir] = c
	}
	for _, dir := range []string{`C:\Users\owner\dev\trd-platform`, home, `\\devserv002\logs`} {
		if _, ok := byDir[dir]; !ok {
			t.Errorf("missing candidate %s", dir)
		}
	}
	if _, ok := byDir[`C:\Users\owner\dev\trd-platform\source\ui`]; ok {
		t.Error("a folder a script moved us to is a candidate")
	}
	// Both spellings of devserv001 are one directory; the latest spelling is kept.
	share, ok := byDir[`\\DEVSERV001\logs\`]
	if !ok || len(got) != 4 {
		t.Fatalf("want 4 candidates with devserv001 once, as last spelled; got %+v", got)
	}
	if share.Score <= byDir[`\\devserv002\logs`].Score {
		t.Errorf("two visits should outrank one: devserv001 %v, devserv002 %v", share.Score, byDir[`\\devserv002\logs`].Score)
	}
}

func TestScoreFavoursRecent(t *testing.T) {
	now := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	if hour, month := score(now.Add(-30*time.Minute), now), score(now.Add(-30*24*time.Hour), now); hour <= month {
		t.Errorf("a visit this hour (%v) should outweigh one last month (%v)", hour, month)
	}
}
