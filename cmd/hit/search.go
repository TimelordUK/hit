package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/TimelordUK/hit/internal/clock"
	"github.com/TimelordUK/hit/internal/paths"
	"github.com/TimelordUK/hit/internal/record"
	"github.com/TimelordUK/hit/internal/search"
	"github.com/TimelordUK/hit/internal/store"
	"github.com/TimelordUK/hit/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

// runSearch is the finder: `hit search` opens the TUI and writes the chosen command to
// --out; `--print` runs the same ranking with no TUI (C-019), for tests, scripts and
// fzf users.
func runSearch(args []string, env paths.Env, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("hit search", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		query   = fs.String("query", "", "initial filter text (the current prompt buffer)")
		scope   = fs.String("scope", string(search.ScopeAll), "dir | session | host | all")
		cwd     = fs.String("cwd", "", "the shell's current directory")
		session = fs.String("session", "", "the shell's session id")
		host    = fs.String("host", "", "this machine's name")
		shell   = fs.String("shell", "", "shell family to show (empty: all)")
		limit   = fs.Int("limit", 0, "maximum results (0: no limit)")
		okOnly  = fs.Bool("ok-only", false, "hide commands that failed")
		out     = fs.String("out", "", "write the chosen command here as JSON")
		print   = fs.Bool("print", false, "print ranked results as JSON lines and exit")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	histPath, err := paths.History(env)
	if err != nil {
		fmt.Fprintln(stderr, "hit:", err)
		return 1
	}
	recs, st, err := store.ReadFile(histPath)
	if err != nil {
		fmt.Fprintln(stderr, "hit:", err)
		return 1
	}
	if st.Corrupt > 0 {
		fmt.Fprintf(stderr, "hit: skipped %d damaged lines in %s\n", st.Corrupt, histPath)
	}
	h := store.Build(recs)

	now, err := clock.FromEnv(env.Getenv)
	if err != nil {
		fmt.Fprintln(stderr, "hit:", err)
		return 1
	}
	q := search.Query{
		Text: *query, Scope: search.Scope(*scope), Cwd: *cwd, Session: *session,
		Host: *host, Shell: *shell, HideFailed: *okOnly, Limit: *limit, Now: now(),
	}

	if *print {
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		for _, r := range search.Search(h, q) {
			if err := enc.Encode(printable(r)); err != nil {
				fmt.Fprintln(stderr, "hit:", err)
				return 1
			}
		}
		return 0
	}

	choice, err := runFinder(h, q)
	if err != nil {
		fmt.Fprintln(stderr, "hit:", err)
		return 1
	}
	if len(choice.Deleted) > 0 {
		if err := tombstone(histPath, choice.Deleted, now()); err != nil {
			fmt.Fprintln(stderr, "hit: could not delete:", err)
		}
	}
	return writeChoice(choice, *out, stdout, stderr)
}

// runFinder is split out so the TUI is the only part that needs a terminal.
func runFinder(h *store.History, q search.Query) (*tui.Choice, error) {
	m := tui.New(h, q)
	// The finder draws on the alternate screen and reads the terminal directly, so a
	// piped stdout (the shell captures it) doesn't disturb it.
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithOutput(os.Stderr))
	final, err := p.Run()
	if err != nil {
		return nil, err
	}
	if c := final.(tui.Model).Choice; c != nil {
		return c, nil
	}
	return &tui.Choice{Action: tui.ActionCancel}, nil
}

func writeChoice(c *tui.Choice, out string, stdout, stderr io.Writer) int {
	b, err := json.Marshal(c)
	if err != nil {
		fmt.Fprintln(stderr, "hit:", err)
		return 1
	}
	b = append(b, '\n')
	if out == "" {
		_, err = stdout.Write(b)
	} else {
		err = os.WriteFile(out, b, 0o600)
	}
	if err != nil {
		fmt.Fprintln(stderr, "hit:", err)
		return 1
	}
	return 0
}

func tombstone(path string, ids []string, now time.Time) error {
	recs := make([]*record.Record, 0, len(ids))
	for _, id := range ids {
		recs = append(recs, &record.Record{K: record.KindDel, ID: id, TS: record.FormatTime(now)})
	}
	return store.Append(path, recs...)
}

// printable is the --print shape: one JSON object per result.
type printableResult struct {
	ID      string  `json:"id"`
	Cmd     string  `json:"cmd"`
	Cwd     string  `json:"cwd,omitempty"`
	TS      string  `json:"ts,omitempty"`
	Exit    *int    `json:"exit,omitempty"`
	Ms      *int64  `json:"ms,omitempty"`
	Count   int     `json:"count"`
	Score   float64 `json:"score"`
	Session string  `json:"sid,omitempty"`
}

func printable(r search.Result) printableResult {
	p := printableResult{
		ID: r.Entry.ID, Cmd: r.Entry.Cmd, Cwd: r.Entry.Cwd, Exit: r.Entry.Exit,
		Ms: r.Entry.Ms, Count: r.Count, Score: r.Score, Session: r.Entry.Session,
	}
	if !r.Entry.Time.IsZero() {
		p.TS = record.FormatTime(r.Entry.Time)
	}
	return p
}
