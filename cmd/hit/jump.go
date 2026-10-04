package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/TimelordUK/hit/internal/category"
	"github.com/TimelordUK/hit/internal/jump"
	"github.com/TimelordUK/hit/internal/paths"
	"github.com/TimelordUK/hit/internal/store"
)

// `hit cd <terms>` (F-030, DESIGN §8.1): where cd would jump, without going there. The
// shell's cd asks this — through the resident server when there is one — only after it
// has ruled out a real path itself, so an ordinary `cd ..` never reaches hit at all.

// jumpAnswer is one resolution: the matches best first, and why there are none.
type jumpAnswer struct {
	matches    []jump.Match
	candidates int
	why        string // set when there is no jump
}

func resolveJump(env paths.Env, cats *category.Set, terms []string, cwd string) (jumpAnswer, error) {
	if !cats.HasJump() {
		return jumpAnswer{why: "no category has jump = true, so cd is plain Set-Location"}, nil
	}
	histPath, err := paths.History(env)
	if err != nil {
		return jumpAnswer{}, err
	}
	recs, _, err := store.ReadFile(histPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return jumpAnswer{}, err
	}
	now, err := clockNow(env)
	if err != nil {
		return jumpAnswer{}, err
	}
	o := jump.Options{Windows: env.GOOS == "windows", Exists: isDir}
	cands := jump.Candidates(store.Build(recs), cats.Jumps, o, now)
	a := jumpAnswer{matches: jump.Explain(cands, cwd, terms, o), candidates: len(cands)}
	switch {
	case len(a.matches) > 0:
	case len(cands) == 0:
		a.why = "no directories reached by a jump command yet, so cd is plain Set-Location"
	default:
		a.why = "nothing matches, so cd is plain Set-Location"
	}
	return a, nil
}

// response is the answer as the shell reads it, from the pipe or from --json.
func (a jumpAnswer) response() Response {
	if len(a.matches) == 0 {
		return Response{Action: "none"}
	}
	m := a.matches[0]
	return Response{Action: "jump", Cwd: m.Dir, Step: string(m.Step), Exact: m.Exact, ByServer: m.Server}
}

func isDir(dir string) bool {
	fi, err := os.Stat(dir)
	return err == nil && fi.IsDir()
}

func runCd(args []string, env paths.Env, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("hit cd", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var (
		cwd     = flags.String("cwd", "", "the directory to jump from (default: here)")
		asJSON  = flags.Bool("json", false, "answer as the shell reads it")
		explain = flags.Bool("explain", false, "list every match, best first, and the step that takes it")
		top     = flags.Int("top", 10, "with --explain: matches to show")
	)
	if err := flags.Parse(args); err != nil {
		return 2
	}
	terms := flags.Args()
	if len(terms) == 0 {
		fmt.Fprintln(stderr, "usage: hit cd [--explain] [--json] [--cwd DIR] [--] <terms…>")
		return 2
	}
	dir := *cwd
	if dir == "" {
		dir, _ = os.Getwd()
	}
	cats, _ := loadCategories(env)
	a, err := resolveJump(env, cats, terms, dir)
	if err != nil {
		fmt.Fprintln(stderr, "hit:", err)
		return 1
	}

	switch {
	case *asJSON:
		if err := writeJSONLine(stdout, a.response()); err != nil {
			fmt.Fprintln(stderr, "hit:", err)
			return 1
		}
		return 0
	case *explain:
		writeJumpExplain(stdout, a, terms, dir, *top)
		writeProblems(stdout, cats)
		return 0
	}
	if len(a.matches) == 0 {
		fmt.Fprintln(stderr, "hit:", a.why)
		return 1
	}
	fmt.Fprintln(stdout, a.matches[0].Dir)
	return 0
}

func writeJumpExplain(w io.Writer, a jumpAnswer, terms []string, cwd string, top int) {
	fmt.Fprintf(w, "cd %s\n  from %s\n", strings.Join(terms, " "), cwd)
	if len(a.matches) == 0 {
		fmt.Fprintf(w, "  %s\n", a.why)
		return
	}
	if a.matches[0].Step == jump.Real {
		fmt.Fprintf(w, "  a real path: Set-Location %s, nothing searched\n", a.matches[0].Dir)
		return
	}
	fmt.Fprintf(w, "  %d of %d places match: below, then the fewest folders up, then anywhere; a folder name before a server name, exact before partial, then frecency\n\n",
		len(a.matches), a.candidates)
	for i, m := range a.matches {
		if i == top {
			fmt.Fprintf(w, "  … %d more\n", len(a.matches)-top)
			break
		}
		step, exact, mark := string(m.Step), "", ""
		if m.Step == jump.Nearest {
			step = fmt.Sprintf("%d up", m.Up)
		}
		switch {
		case m.Exact:
			exact = "exact"
		case m.Server:
			exact = "server"
		}
		if i == 0 {
			mark = "  <- jumps here"
		}
		fmt.Fprintf(w, "  %-8s %-6s %7.2f  %s%s\n", step, exact, m.Score, m.Dir, mark)
	}
}

func writeJSONLine(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}
