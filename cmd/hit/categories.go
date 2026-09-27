package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/TimelordUK/hit/internal/category"
	"github.com/TimelordUK/hit/internal/config"
	"github.com/TimelordUK/hit/internal/paths"
	"github.com/TimelordUK/hit/internal/store"
)

// `hit categories` (C-038): the tuning loop for categories (DESIGN §17.1).
//
// Categories are only worth having if the rules fit the owner's real history, and that
// is found out by trying them against it: edit config.toml, run this, look. So it shows
// what each category catches, and — more usefully — the most frequent commands that no
// category catches, which is where a bucket like `devops` grows from.

// loadCategories reads and compiles config.toml. It cannot fail: a config that cannot be
// read or compiled yields no rules and its problems, never an error on the Ctrl+R path.
func loadCategories(env paths.Env) (*category.Set, string) {
	p, err := paths.ConfigFile(env)
	if err != nil {
		return &category.Set{Problems: []string{err.Error()}}, ""
	}
	return category.Compile(config.Load(p), category.Paths{Home: env.Home, Windows: env.GOOS == "windows"}), p
}

func runCategories(args []string, env paths.Env, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("hit categories", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var (
		top     = flags.Int("top", 8, "commands to show per category")
		explain = flags.String("explain", "", "say which rule matches this command, and why")
		cwd     = flags.String("cwd", "", "with --explain: the directory it ran in (default: here)")
	)
	if err := flags.Parse(args); err != nil {
		return 2
	}
	set, cfgPath := loadCategories(env)

	if *explain != "" {
		dir := *cwd
		if dir == "" {
			dir, _ = os.Getwd()
		}
		writeExplain(stdout, set, *explain, dir)
		writeProblems(stdout, set)
		return 0
	}

	histPath, err := paths.History(env)
	if err != nil {
		fmt.Fprintln(stderr, "hit:", err)
		return 1
	}
	recs, _, err := store.ReadFile(histPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(stderr, "hit:", err)
		return 1
	}
	h := store.Build(recs)
	rep := tally(set, h.Entries)
	writeCategories(stdout, set, rep, cfgPath, len(h.Entries), *top)
	return 0
}

// bucket counts runs of each distinct command.
type bucket struct {
	runs int
	cmds map[string]int
}

func (b *bucket) add(cmd string) {
	if b.cmds == nil {
		b.cmds = map[string]int{}
	}
	b.runs++
	b.cmds[cmd]++
}

type tallyReport struct {
	byRule    []bucket
	none      bucket // simple commands no rule matched
	compound  bucket // chains and blocks, which no rule may match
	anyLabels int    // runs with at least one category
}

func tally(set *category.Set, entries []store.Entry) tallyReport {
	rep := tallyReport{byRule: make([]bucket, len(set.Rules))}
	for _, e := range entries {
		if category.Compound(e.Cmd) != "" {
			rep.compound.add(e.Cmd)
			continue
		}
		hits := set.Classify(e.Cmd, e.Cwd)
		if len(hits) == 0 {
			rep.none.add(e.Cmd)
			continue
		}
		rep.anyLabels++
		for _, i := range hits {
			rep.byRule[i].add(e.Cmd)
		}
	}
	return rep
}

func writeCategories(w io.Writer, set *category.Set, rep tallyReport, cfgPath string, total, top int) {
	if cfgPath != "" {
		if _, err := os.Stat(cfgPath); err != nil {
			fmt.Fprintf(w, "config   %s  (none yet: see DESIGN §17.1 for an example)\n", cfgPath)
		} else {
			fmt.Fprintf(w, "config   %s  (%d categories)\n", cfgPath, len(set.Rules))
		}
	}
	fmt.Fprintf(w, "history  %d commands, %d in a category (%s)\n", total, rep.anyLabels, percent(rep.anyLabels, total))

	for i, r := range set.Rules {
		b := rep.byRule[i]
		fmt.Fprintf(w, "\n%s  %s  %s\n", r.Mark, r.Name, counts(b))
		writeTop(w, b, top)
	}
	fmt.Fprintf(w, "\nuncategorised  %s  — the most frequent, where new rules come from\n", counts(rep.none))
	writeTop(w, rep.none, top*2)
	fmt.Fprintf(w, "\nchains and blocks  %s  — never categorised\n", counts(rep.compound))
	writeProblems(w, set)
}

func counts(b bucket) string {
	return fmt.Sprintf("%d runs, %d distinct", b.runs, len(b.cmds))
}

func percent(n, of int) string {
	if of == 0 {
		return "0%"
	}
	return fmt.Sprintf("%d%%", n*100/of)
}

func writeTop(w io.Writer, b bucket, n int) {
	type row struct {
		cmd  string
		runs int
	}
	rows := make([]row, 0, len(b.cmds))
	for c, k := range b.cmds {
		rows = append(rows, row{c, k})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].runs != rows[j].runs {
			return rows[i].runs > rows[j].runs
		}
		return rows[i].cmd < rows[j].cmd
	})
	for i, r := range rows {
		if i == n {
			break
		}
		fmt.Fprintf(w, "  %5d  %s\n", r.runs, oneLine(r.cmd, 100))
	}
}

// oneLine shows a multi-line command as its first line with a marker, cut to width.
func oneLine(cmd string, width int) string {
	s := cmd
	more := false
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s, more = strings.TrimRight(s[:i], "\r"), true
	}
	if r := []rune(s); len(r) > width {
		s, more = string(r[:width]), true
	}
	if more {
		s += " …"
	}
	return s
}

func writeExplain(w io.Writer, set *category.Set, cmd, cwd string) {
	fmt.Fprintf(w, "%s\n  in %s\n", oneLine(cmd, 100), cwd)
	matches, reason := set.Explain(cmd, cwd)
	if reason != "" {
		fmt.Fprintf(w, "  no category: %s\n", reason)
		return
	}
	for i, m := range matches {
		lead := "  also "
		if i == 0 {
			lead = "  → "
		}
		fmt.Fprintf(w, "%s%s  (%s: %s)\n", lead, m.Name, m.Field, m.What)
	}
	if len(matches) > 1 {
		fmt.Fprintf(w, "  the mark is %s's: it comes first in the file\n", matches[0].Name)
	}
}

func writeProblems(w io.Writer, set *category.Set) {
	if len(set.Problems) == 0 {
		return
	}
	fmt.Fprintln(w, "\nproblems in the config")
	for _, p := range set.Problems {
		fmt.Fprintln(w, "  "+p)
	}
}
