// Package config reads config.toml (DESIGN §17.1).
//
// A broken config must never break Ctrl+R, so problems are collected rather than returned
// as failures: a table with an error is dropped and the error is kept for `hit categories`
// and the finder's header to report. Only a file that cannot be parsed at all yields no
// config, and even that is reported the same way rather than stopping anything.
//
// Tables are decoded by hand, not straight into structs, so that an unknown field is
// reported against the rule it is in ("category 3 (devops): unknown field comands"). A typo
// that silently matched nothing would look exactly like a rule that was never needed.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is what config.toml said, as far as it could be understood.
type Config struct {
	Path       string
	Categories []Category
	Capture    Capture
	Finder     Finder
	// Problems are everything that was wrong, in file order. A category with a problem
	// is not in Categories.
	Problems []string
}

// Category is one [[category]] table, not yet compiled. Field meanings are in DESIGN §17.1.
type Category struct {
	Name     string
	Color    string
	Mark     string
	Commands []string
	Match    string
	Cwd      []string
	Scripts  []string
}

// Capture is the [capture] table (DESIGN §18): the variables `hit init` compiles into the
// shell script for its prompt hook to record.
type Capture struct {
	Env []string
}

// Finder is the [finder] table: how the finder behaves by default.
type Finder struct {
	// Case is "ignore" or "smart" (C-041), lower-cased; empty means the default, ignore.
	Case string
}

// Load reads the config at path. A missing file is an empty config, not a problem: most
// people have none.
func Load(path string) Config {
	c := Config{Path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c
	}
	if err != nil {
		c.Problems = append(c.Problems, err.Error())
		return c
	}
	return parse(c, string(b))
}

// Parse reads config text; path is only for messages.
func Parse(path, text string) Config {
	return parse(Config{Path: path}, text)
}

func parse(c Config, text string) Config {
	var raw map[string]any
	if _, err := toml.Decode(text, &raw); err != nil {
		c.Problems = append(c.Problems, err.Error())
		return c
	}
	for _, key := range sortedKeys(raw) {
		switch key {
		case "category":
			tables, ok := raw[key].([]map[string]any)
			if !ok {
				c.Problems = append(c.Problems, "category: write each one as [[category]]")
				continue
			}
			for i, t := range tables {
				cat, problems := category(t)
				if len(problems) > 0 {
					label := fmt.Sprintf("category %d", i+1)
					if cat.Name != "" {
						label += " (" + cat.Name + ")"
					}
					for _, p := range problems {
						c.Problems = append(c.Problems, label+": "+p)
					}
					continue
				}
				c.Categories = append(c.Categories, cat)
			}
		case "capture":
			t, ok := raw[key].(map[string]any)
			if !ok {
				c.Problems = append(c.Problems, "capture: expected a [capture] table")
				continue
			}
			for _, k := range sortedKeys(t) {
				switch k {
				case "env":
					env, err := stringList(t[k])
					if err != nil {
						c.Problems = append(c.Problems, "capture: env: "+err.Error())
						continue
					}
					c.Capture.Env = env
				default:
					c.Problems = append(c.Problems, "capture: unknown field "+k)
				}
			}
		case "finder":
			t, ok := raw[key].(map[string]any)
			if !ok {
				c.Problems = append(c.Problems, "finder: expected a [finder] table")
				continue
			}
			for _, k := range sortedKeys(t) {
				switch k {
				case "case":
					v, _ := t[k].(string)
					switch v = strings.ToLower(v); v {
					case "ignore", "smart":
						c.Finder.Case = v
					default:
						c.Problems = append(c.Problems, fmt.Sprintf("finder: case %v: expected \"ignore\" or \"smart\"; ignoring case", t[k]))
					}
				default:
					c.Problems = append(c.Problems, "finder: unknown field "+k)
				}
			}
		default:
			c.Problems = append(c.Problems, "unknown section "+key)
		}
	}
	// Two rules with one name would make the filter ambiguous; the first one stands.
	seen := map[string]bool{}
	kept := c.Categories[:0]
	for _, cat := range c.Categories {
		if seen[cat.Name] {
			c.Problems = append(c.Problems, fmt.Sprintf("category %s: defined twice; the first one is used", cat.Name))
			continue
		}
		seen[cat.Name] = true
		kept = append(kept, cat)
	}
	c.Categories = kept
	return c
}

func category(t map[string]any) (Category, []string) {
	var cat Category
	var problems []string
	str := func(k string) string {
		s, ok := t[k].(string)
		if !ok {
			problems = append(problems, k+": expected a string")
		}
		return s
	}
	// The name first, so problems with the other fields can say which rule they are in.
	if _, ok := t["name"]; ok {
		cat.Name = str("name")
	}
	for _, k := range sortedKeys(t) {
		switch k {
		case "name":
		case "color":
			cat.Color = str(k)
		case "mark":
			cat.Mark = str(k)
		case "match":
			cat.Match = str(k)
		case "commands", "cwd", "scripts":
			list, err := stringList(t[k])
			if err != nil {
				problems = append(problems, k+": "+err.Error())
				continue
			}
			switch k {
			case "commands":
				cat.Commands = list
			case "cwd":
				cat.Cwd = list
			default:
				cat.Scripts = list
			}
		default:
			problems = append(problems, "unknown field "+k)
		}
	}
	if cat.Name == "" {
		problems = append(problems, "name is required")
	}
	return cat, problems
}

// stringList accepts one string or a list of them, so `cwd = '~\dev\**'` and
// `cwd = ['~\dev\**']` mean the same.
func stringList(v any) ([]string, error) {
	switch v := v.(type) {
	case string:
		return []string{v}, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			s, ok := e.(string)
			if !ok {
				return nil, fmt.Errorf("expected a list of strings")
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, fmt.Errorf("expected a string or a list of strings")
}

// sortedKeys makes problem messages come out in a stable order. TOML tables are maps, so
// the file's own order is not available for keys within a table; arrays of tables keep it.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
