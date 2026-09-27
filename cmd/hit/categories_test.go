package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/TimelordUK/hit/internal/paths"
	"github.com/TimelordUK/hit/internal/record"
	"github.com/TimelordUK/hit/internal/store"
)

// isolated gives a command its own history and config, never the owner's real ones.
func isolated(t *testing.T, config string, cmds ...string) paths.Env {
	t.Helper()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	if config != "" {
		if err := os.WriteFile(cfg, []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	vars := map[string]string{"HIT_DATA_DIR": filepath.Join(dir, "data"), "HIT_CONFIG": cfg}
	env := paths.Env{Getenv: func(k string) string { return vars[k] }, GOOS: runtime.GOOS, Home: dir}
	hist, err := paths.History(env)
	if err != nil {
		t.Fatal(err)
	}
	var recs []*record.Record
	for i, c := range cmds {
		recs = append(recs, &record.Record{K: record.KindCmd, ID: ulidN(i), Cmd: c, Cwd: `C:\dev\hit`, Sh: "pwsh", Sid: "s1"})
	}
	if len(recs) > 0 {
		if err := store.Append(hist, recs...); err != nil {
			t.Fatal(err)
		}
	}
	return env
}

// ulidN is a valid, distinct id for the nth test record.
func ulidN(n int) string {
	const digits = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	return "01K5HQ8ZJ2A7Q3M8V4W6X9Y0" + string(digits[n/32%32]) + string(digits[n%32])
}

func runCmd(t *testing.T, env paths.Env, args ...string) string {
	t.Helper()
	var out, errb bytes.Buffer
	if code := run(args, env, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	return out.String()
}

const twoCategories = `
[[category]]
name     = "git"
commands = ["git", "gh"]

[[category]]
name     = "content"
commands = ["curl", "Expand-Archive"]
`

func TestCategoriesReportsWhatEachCatchesAndWhatNoneDo(t *testing.T) {
	env := isolated(t, twoCategories,
		"git status", "git status", "git log", "gh pr list",
		"curl https://x", "ls", "ls", "ls", "jq . x.json",
		"cd ..; git status")
	out := runCmd(t, env, "categories")
	for _, want := range []string{
		"(2 categories)",
		"history  10 commands, 5 in a category (50%)",
		"G  git  4 runs, 3 distinct",
		"      2  git status",
		"C  content  1 runs, 1 distinct",
		"uncategorised  4 runs, 2 distinct",
		"      3  ls", // the most frequent uncategorised command comes first
		"chains and blocks  1 runs, 1 distinct",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// With no config the report is still useful: every command is uncategorised, ranked, which
// is where the first rules come from.
func TestCategoriesWithNoConfig(t *testing.T) {
	env := isolated(t, "", "ls", "ls", "git status")
	out := runCmd(t, env, "categories")
	for _, want := range []string{"none yet", "uncategorised  3 runs, 2 distinct", "      2  ls"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestCategoriesShowsConfigProblems(t *testing.T) {
	env := isolated(t, twoCategories+"\n[[category]]\nname = \"devops\"\ncomands = [\"x\"]\n", "git status")
	out := runCmd(t, env, "categories")
	if !strings.Contains(out, "problems in the config") || !strings.Contains(out, "unknown field comands") {
		t.Errorf("got:\n%s", out)
	}
}

func TestCategoriesExplain(t *testing.T) {
	env := isolated(t, twoCategories)
	out := runCmd(t, env, "categories", "--explain", "git log | Select-String fix", "--cwd", `C:\dev`)
	if !strings.Contains(out, "→ git  (commands: git)") {
		t.Errorf("got:\n%s", out)
	}
	out = runCmd(t, env, "categories", "--explain", "cd ..; git status")
	if !strings.Contains(out, "no category: it is a chain") {
		t.Errorf("got:\n%s", out)
	}
}

// No history file yet is a fresh install, not an error.
func TestCategoriesWithNoHistory(t *testing.T) {
	env := isolated(t, twoCategories)
	if out := runCmd(t, env, "categories"); !strings.Contains(out, "history  0 commands") {
		t.Errorf("got:\n%s", out)
	}
}
