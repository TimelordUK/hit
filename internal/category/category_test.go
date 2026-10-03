package category

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/TimelordUK/hit/internal/config"
)

var home = Paths{Home: `C:\Users\owner`, Windows: true}

func ownerSet(t *testing.T) *Set {
	t.Helper()
	b, err := os.ReadFile("testdata/owner.toml")
	if err != nil {
		t.Fatal(err)
	}
	s := Compile(config.Parse("owner.toml", string(b)), home)
	if len(s.Problems) > 0 {
		t.Fatalf("the owner's config should compile cleanly: %v", s.Problems)
	}
	return s
}

func names(s *Set, idx []int) []string {
	out := []string{}
	for _, i := range idx {
		out = append(out, s.Rules[i].Name)
	}
	return out
}

// The owner's own examples from 2026-09-26, one per line, with the answer they gave.
func TestOwnerCategories(t *testing.T) {
	s := ownerSet(t)
	for _, tc := range []struct {
		cmd, cwd string
		want     []string
	}{
		// git: the command starts with git or gh
		{"git status", `C:\Users\owner\dev\hit`, []string{"git"}},
		{"gh pr list", ``, []string{"git"}},
		{"GIT log -5", ``, []string{"git"}},
		{"git.exe fetch", ``, []string{"git"}},
		// a pipeline that starts with git is git
		{"git log --oneline | Select-String fix", ``, []string{"git"}},
		{"git log |\n  Select-String fix", ``, []string{"git"}},
		{"git log\n  | Select-String fix", ``, []string{"git"}},
		// a chain or block with git inside is not
		{"cd ..; git status", ``, nil},
		{"git add . && git commit -m x", ``, nil},
		{"git fetch\ngit status", ``, nil},
		{"& { git status }", ``, nil},
		{"Get-ChildItem | ForEach-Object { git -C $_ status }", ``, nil},

		// navigation: typed moves
		{"cd ..", ``, []string{"navigation"}},
		{`Set-Location \\srv\share\logs`, ``, []string{"navigation"}},
		{"pushd C:\\temp", ``, []string{"navigation"}},
		// a fuzzy jump is a query, not a place (z is not listed)
		{"z platform", ``, nil},

		// content: fetching and archives
		{"Expand-Archive .\\logs.zip -DestinationPath out", ``, []string{"content"}},
		{"iwr https://example.com/x.zip -OutFile x.zip", ``, []string{"content"}},
		{"tar -xzf pkg.tgz", ``, []string{"content"}},

		// environment
		{"$env:ES_URL = 'https://es:9200'", ``, []string{"environment"}},
		{"$cred = Get-Credential", ``, []string{"environment"}},
		{"[Environment]::SetEnvironmentVariable('X', '1', 'User')", ``, []string{"environment"}},
		{"$env:PATH", ``, nil}, // reading is not setting

		// devops, by command, by pattern, and by where it ran
		{"Invoke-Command -ComputerName box1 { Get-Process }", ``, []string{"devops"}},
		{"& .\\Get-EsProcessTree.ps1 -Node 3", ``, []string{"devops"}},
		{`& 'C:\tools\Get-EsProcessTree.ps1'`, ``, []string{"devops"}},
		{".\\reindex.ps1 -Index logs", `C:\Users\owner\dev\ops-scripts\elastic`, []string{"devops"}},
		{".\\reindex.ps1", `C:\Users\owner\dev\ops-scripts`, []string{"devops"}},
		{"Get-ChildItem", `\\ops-share\drops\today`, []string{"devops"}},
		// devops first in the file, so an elastic curl is devops *and* content, and
		// devops is the one that decides the mark
		{"curl -s https://elastic-prod-1:9200/_cat/health", ``, []string{"devops", "content"}},

		// no category: found better by typing into Ctrl+R
		{"jq '.hits.hits[]' out.json", ``, nil},
		{"Get-ChildItem", `C:\Users\owner\dev\hit`, nil},
	} {
		got := names(s, s.Classify(tc.cmd, tc.cwd))
		want := tc.want
		if want == nil {
			want = []string{}
		}
		if !slices.Equal(got, want) {
			t.Errorf("%q in %q: got %v, want %v", tc.cmd, tc.cwd, got, want)
		}
	}
}

func TestStringsAndBracketsHideSeparators(t *testing.T) {
	for _, cmd := range []string{
		`git commit -m "first; second"`,
		`git commit -m 'a && b'`,
		"git log --format=\"%h`\"; %s\"", // an escaped quote does not end the string
		`git log --grep="x|y"`,
		`git branch --list 'fix/*' # tidy up; later`,
	} {
		if c := Compound(cmd); c != "" {
			t.Errorf("%q called %s", cmd, c)
		}
	}
}

func TestExplain(t *testing.T) {
	s := ownerSet(t)
	m, why := s.Explain("git status", "")
	if why != "" || len(m) != 1 || m[0].Name != "git" || m[0].Field != FieldCommands || m[0].What != "git" {
		t.Errorf("git status: %+v, %q", m, why)
	}
	m, _ = s.Explain("curl https://elastic-prod-1:9200", "")
	if len(m) != 2 || m[0].Field != FieldMatch || m[0].What != "elastic-prod" {
		t.Errorf("elastic curl: %+v", m)
	}
	if _, why = s.Explain("cd ..; git status", ""); !strings.Contains(why, "chain") {
		t.Errorf("a chain should say so: %q", why)
	}
	if _, why = s.Explain("jq . x.json", ""); !strings.Contains(why, `"jq"`) {
		t.Errorf("no match should name the first word it tried: %q", why)
	}
}

// A broken rule is reported and dropped; the rest still work.
func TestBrokenRulesAreDroppedNotFatal(t *testing.T) {
	s := Compile(config.Parse("c.toml", `
[[category]]
name  = "bad-regex"
match = '(?<=lookbehind)x'

[[category]]
name     = "git"
commands = ["git"]

[[category]]
name = "empty"

[[category]]
name     = "has spaces"
commands = ["x"]
`), home)
	if got := names(s, s.Classify("git status", "")); !slices.Equal(got, []string{"git"}) {
		t.Errorf("the good rule should survive: %v", got)
	}
	if len(s.Rules) != 1 {
		t.Errorf("rules: %v", names(s, []int{0}))
	}
	joined := strings.Join(s.Problems, "\n")
	for _, want := range []string{"bad-regex: match:", "empty: has no commands", "has spaces: name may use only"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing problem %q in:\n%s", want, joined)
		}
	}
}

// A wrong colour or mark is presentation: the category keeps working on the default.
func TestPresentationProblemsKeepTheRule(t *testing.T) {
	s := Compile(config.Parse("c.toml", `
[[category]]
name     = "git"
color    = "chartreuse"
mark     = "GG"
commands = ["git"]
`), home)
	if len(s.Rules) != 1 {
		t.Fatalf("rule dropped: %v", s.Problems)
	}
	if r := s.Rules[0]; r.Color != "" || r.Mark != "G" {
		t.Errorf("defaults not applied: %+v", r)
	}
	if len(s.Problems) != 2 {
		t.Errorf("both problems should be reported: %v", s.Problems)
	}
}

func TestMarkDefaultsToTheFirstLetter(t *testing.T) {
	s := ownerSet(t)
	for i, want := range []string{"D", "G", "N", "C", "E"} {
		if s.Rules[i].Mark != want {
			t.Errorf("%s: mark %q, want %q", s.Rules[i].Name, s.Rules[i].Mark, want)
		}
	}
}

func TestCwdGlobs(t *testing.T) {
	for _, tc := range []struct {
		glob, dir string
		windows   bool
		want      bool
	}{
		{`~\dev\**`, `C:\Users\owner\dev`, true, true},
		{`~\dev\**`, `C:\Users\owner\dev\hit\cmd`, true, true},
		{`~\dev\**`, `C:\Users\owner\devices`, true, false},
		{`~/dev/**`, `c:\users\OWNER\DEV\hit`, true, true}, // case and slashes, on Windows
		{`\\**`, `\\srv\share\logs`, true, true},
		{`\\**`, `C:\logs`, true, false},
		{`C:\logs\*`, `C:\logs\today`, true, true},
		{`C:\logs\*`, `C:\logs\today\deep`, true, false},
		{`~/dev/**`, `/home/owner/dev/hit`, false, true},
		{`~/dev/**`, `/home/owner/DEV/hit`, false, false}, // case matters off Windows
	} {
		p := Paths{Home: `C:\Users\owner`, Windows: tc.windows}
		if !tc.windows {
			p.Home = "/home/owner"
		}
		re, err := globRegexp(tc.glob, p)
		if err != nil {
			t.Fatalf("%q: %v", tc.glob, err)
		}
		if got := re.MatchString(tc.dir); got != tc.want {
			t.Errorf("%q vs %q: got %v, want %v (%s)", tc.glob, tc.dir, got, tc.want, re)
		}
	}
}

func TestNoConfigMeansNoCategories(t *testing.T) {
	s := Compile(config.Config{}, home)
	if got := s.Classify("git status", ""); got != nil {
		t.Errorf("got %v", got)
	}
	if _, why := s.Explain("git status", ""); !strings.Contains(why, "no categories") {
		t.Errorf("got %q", why)
	}
}

// scripts matches where the script file is, however it was typed (C-040). The owner's
// case: anything in ~\dev\personal-docs\scripts is devops, run as scripts\X.ps1 from the
// repo (where zoxide lands), .\X.ps1 from the folder itself, or by a full path from
// anywhere — and nothing else run in that repo, and no other repo's scripts\.
func TestScriptsMatchWhereTheFileIs(t *testing.T) {
	cfg := config.Parse("c.toml", `
[[category]]
name    = "devops"
scripts = '~\dev\personal-docs\scripts\*.ps1'
`)
	unixCfg := config.Parse("c.toml", `
[[category]]
name    = "devops"
scripts = '~/dev/personal-docs/scripts/*.ps1'
`)
	for _, p := range []Paths{home, {Home: "/home/owner"}} {
		s := Compile(cfg, p)
		if !p.Windows {
			s = Compile(unixCfg, p)
		}
		if len(s.Problems) > 0 {
			t.Fatal(s.Problems)
		}
		repo, hit, elsewhere := `C:\Users\owner\dev\personal-docs`, `C:\Users\owner\dev\hit`, `C:\temp`
		cases := []struct {
			cmd, cwd string
			want     bool
		}{
			{`scripts\Restart-Agent.ps1`, repo, true},
			{`.\scripts\Restart-Agent.ps1 -Force`, repo, true},
			{`& '.\scripts\Restart Agent.ps1'`, repo, true},
			{`.\Restart-Agent.ps1`, repo + `\scripts`, true},
			{`..\scripts\Restart-Agent.ps1`, repo + `\docs`, true},
			{`~\dev\personal-docs\scripts\Restart-Agent.ps1`, elsewhere, true},
			{`C:\Users\OWNER\dev\personal-docs\scripts\Restart-Agent.ps1`, elsewhere, true},
			{`. .\scripts\Set-Env.ps1`, repo, true},
			{`scripts\Restart-Agent.ps1 | Out-File x`, repo, true},
			{`scripts\sub\Deep.ps1`, repo, false}, // * stays within one directory
			{`scripts\notes.md`, repo, false},
			{`Restart-Agent.ps1`, repo + `\scripts`, false}, // bare word: pwsh looks on PATH
			{`git status`, repo, false},
			{`Get-ChildItem`, repo + `\scripts`, false},
			{`scripts\install.ps1`, hit, false},
			{`scripts\Restart-Agent.ps1`, ``, false}, // relative, and no cwd to resolve it
		}
		if !p.Windows {
			repo, hit = "/home/owner/dev/personal-docs", "/home/owner/dev/hit"
			cases = []struct {
				cmd, cwd string
				want     bool
			}{
				{`scripts/Restart-Agent.ps1`, repo, true},
				{`./Restart-Agent.ps1`, repo + `/scripts`, true},
				{`~/dev/personal-docs/scripts/Restart-Agent.ps1`, "/tmp", true},
				{`scripts/install.ps1`, hit, false},
				{`scripts/Restart-Agent.ps1`, "/home/owner/dev/Personal-Docs", false}, // case matters
			}
		}
		for _, tc := range cases {
			if got := len(s.Classify(tc.cmd, tc.cwd)) > 0; got != tc.want {
				t.Errorf("windows=%v %q in %q: got %v, want %v", p.Windows, tc.cmd, tc.cwd, got, tc.want)
			}
		}
	}
}

// --explain names the file the command resolved to, so a near miss is visible.
func TestExplainScripts(t *testing.T) {
	s := Compile(config.Parse("c.toml", `
[[category]]
name    = "devops"
scripts = ['~\dev\personal-docs\scripts\*.ps1']
`), home)
	m, _ := s.Explain(`scripts\Restart-Agent.ps1`, `C:\Users\owner\dev\personal-docs`)
	if len(m) != 1 || m[0].Field != FieldScripts || m[0].What != `C:\Users\owner\dev\personal-docs\scripts\Restart-Agent.ps1` {
		t.Errorf("got %+v", m)
	}
}

// Alt+G's picker jumps by mark letter (T-019), so two groups sharing a letter leave the
// second reachable only by cycling. Reported, so `mark = "…"` can fix it; both rules stay.
func TestDuplicateMarksAreReported(t *testing.T) {
	s := Compile(config.Parse("c.toml", `
[[category]]
name     = "devops"
commands = ["x"]

[[category]]
name     = "docs"
commands = ["y"]
`), home)
	if len(s.Rules) != 2 {
		t.Fatalf("both rules should stay: %d", len(s.Rules))
	}
	if len(s.Problems) != 1 || !strings.Contains(s.Problems[0], `"D"`) || !strings.Contains(s.Problems[0], "devops") {
		t.Errorf("got %v", s.Problems)
	}
}

// Only a jump = true category's commands make jump targets, and a chain never does.
func TestJumps(t *testing.T) {
	s := ownerSet(t)
	for cmd, want := range map[string]bool{
		`cd ~\dev\trd-platform`:            true,
		`Set-Location \devserv001\logs`:    true,
		`git status`:                       false,
		`cd ~\dev; git pull`:               false,
		`.\scripts\Import-Credentials.ps1`: false,
	} {
		if got := s.Jumps(cmd, home.Home); got != want {
			t.Errorf("Jumps(%q) = %v, want %v", cmd, got, want)
		}
	}
	if Compile(config.Parse("c.toml", "[[category]]\nname = \"git\"\ncommands = [\"git\"]\n"), home).HasJump() {
		t.Error("a config without jump = true must not jump")
	}
	var none *Set
	if none.HasJump() || none.Jumps("cd x", "") {
		t.Error("no config, no jump")
	}
}
