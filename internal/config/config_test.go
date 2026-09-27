package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestMissingFileIsAnEmptyConfigNotAProblem(t *testing.T) {
	c := Load(filepath.Join(t.TempDir(), "config.toml"))
	if len(c.Problems) != 0 || len(c.Categories) != 0 {
		t.Errorf("got %+v", c)
	}
}

func TestLoadReadsAFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("[[category]]\nname = \"git\"\ncommands = [\"git\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := Load(p)
	if len(c.Categories) != 1 || c.Categories[0].Name != "git" || c.Path != p {
		t.Errorf("got %+v", c)
	}
}

func TestCategoriesKeepFileOrder(t *testing.T) {
	c := Parse("c.toml", `
[[category]]
name = "zeta"
commands = ["z"]

[[category]]
name = "alpha"
commands = ["a"]
`)
	var got []string
	for _, cat := range c.Categories {
		got = append(got, cat.Name)
	}
	if !slices.Equal(got, []string{"zeta", "alpha"}) {
		t.Errorf("order is priority and must be kept: %v", got)
	}
}

// cwd = 'x' and cwd = ['x'] mean the same.
func TestCwdTakesOneOrMany(t *testing.T) {
	c := Parse("c.toml", `
[[category]]
name = "one"
cwd  = '~\dev\**'

[[category]]
name = "many"
cwd  = ['~\dev\**', '\\share\**']
`)
	if len(c.Problems) != 0 {
		t.Fatal(c.Problems)
	}
	if !slices.Equal(c.Categories[0].Cwd, []string{`~\dev\**`}) || len(c.Categories[1].Cwd) != 2 {
		t.Errorf("got %+v", c.Categories)
	}
}

// The typo that matters: a misspelt field would otherwise match nothing, silently.
func TestUnknownFieldIsReportedAgainstItsRule(t *testing.T) {
	c := Parse("c.toml", `
[[category]]
name = "git"
commands = ["git"]

[[category]]
name = "devops"
comands = ["Invoke-Command"]
`)
	if len(c.Categories) != 1 || c.Categories[0].Name != "git" {
		t.Errorf("the good rule should stand alone: %+v", c.Categories)
	}
	if len(c.Problems) != 1 || !strings.Contains(c.Problems[0], "category 2 (devops): unknown field comands") {
		t.Errorf("got %v", c.Problems)
	}
}

func TestWrongTypesAndMissingNames(t *testing.T) {
	c := Parse("c.toml", `
[[category]]
commands = ["git"]

[[category]]
name = "x"
commands = "not-a-list-but-fine"

[[category]]
name = "y"
commands = [1, 2]

[[category]]
name = 5
`)
	joined := strings.Join(c.Problems, "\n")
	for _, want := range []string{
		"category 1: name is required",
		"category 3 (y): commands: expected a list of strings",
		"category 4: name: expected a string",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
	if len(c.Categories) != 1 || c.Categories[0].Name != "x" {
		t.Errorf("a single string is a one-item list: %+v", c.Categories)
	}
}

func TestDuplicateNamesKeepTheFirst(t *testing.T) {
	c := Parse("c.toml", `
[[category]]
name = "git"
commands = ["git"]

[[category]]
name = "git"
commands = ["gh"]
`)
	if len(c.Categories) != 1 || c.Categories[0].Commands[0] != "git" {
		t.Errorf("got %+v", c.Categories)
	}
	if len(c.Problems) != 1 || !strings.Contains(c.Problems[0], "defined twice") {
		t.Errorf("got %v", c.Problems)
	}
}

func TestUnparseableFileIsOneProblemNotAPanic(t *testing.T) {
	c := Parse("c.toml", "[[category]\nname = ")
	if len(c.Problems) != 1 || len(c.Categories) != 0 {
		t.Errorf("got %+v", c)
	}
}

func TestCaptureAndUnknownSections(t *testing.T) {
	c := Parse("c.toml", `
[capture]
env = ["ELASTIC_ENV", "ES_*"]

[guards]
x = 1
`)
	if !slices.Equal(c.Capture.Env, []string{"ELASTIC_ENV", "ES_*"}) {
		t.Errorf("capture: %+v", c.Capture)
	}
	if len(c.Problems) != 1 || !strings.Contains(c.Problems[0], "unknown section guards") {
		t.Errorf("got %v", c.Problems)
	}
}
