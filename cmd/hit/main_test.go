package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TimelordUK/hit/internal/paths"
)

func testEnv(vars map[string]string) paths.Env {
	return paths.Env{Getenv: func(k string) string { return vars[k] }, GOOS: "windows"}
}

func TestPath(t *testing.T) {
	env := testEnv(map[string]string{"HIT_DATA_DIR": "D", "HIT_CONFIG": "C.toml"})
	for arg, want := range map[string]string{
		"data":    "D",
		"history": filepath.Join("D", "history.jsonl"),
		"config":  "C.toml",
	} {
		var out, errb bytes.Buffer
		if code := run([]string{"path", arg}, env, &out, &errb); code != 0 {
			t.Fatalf("path %s: exit %d: %s", arg, code, errb.String())
		}
		if got := strings.TrimSpace(out.String()); got != want {
			t.Errorf("path %s = %q, want %q", arg, got, want)
		}
	}
	for _, args := range [][]string{{"path"}, {"path", "nope"}, {"path", "data", "x"}} {
		var out, errb bytes.Buffer
		if code := run(args, env, &out, &errb); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
	var out, errb bytes.Buffer
	if code := run([]string{"path", "data"}, testEnv(nil), &out, &errb); code != 1 {
		t.Errorf("unresolvable data dir: exit %d, want 1", code)
	}
}

func TestInitPwshEmbedsHistoryPath(t *testing.T) {
	env := testEnv(map[string]string{"HIT_DATA_DIR": `C:\it's here`})
	var out, errb bytes.Buffer
	if code := run([]string{"init", "pwsh"}, env, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	want := "Enable-Hit -HistoryPath '" + strings.ReplaceAll(filepath.Join(`C:\it's here`, "history.jsonl"), "'", "''") + "'"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("missing %q", want)
	}
}
