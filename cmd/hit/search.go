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
	"github.com/TimelordUK/hit/internal/timing"
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
		order   = fs.String("sort", string(search.SortRank), "rank | recent")
		cwd     = fs.String("cwd", "", "the shell's current directory")
		session = fs.String("session", "", "the shell's session id")
		host    = fs.String("host", "", "this machine's name")
		shell   = fs.String("shell", "", "shell family to show (empty: all)")
		limit   = fs.Int("limit", 0, "maximum results (0: no limit)")
		okOnly  = fs.Bool("ok-only", false, "hide commands that failed")
		out     = fs.String("out", "", "write the chosen command here as JSON")
		print   = fs.Bool("print", false, "print ranked results as JSON lines and exit")
		started = fs.Int64("started-at", 0, "unix ms when the shell spawned us (for --timing)")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// The timeline starts when the shell called Process.Start, when it told us, so the
	// first span is process creation — the part neither side can see on its own.
	tl := newTimeline(env, *started)

	histPath, err := paths.History(env)
	if err != nil {
		fmt.Fprintln(stderr, "hit:", err)
		return 1
	}
	tl.Mark("init")
	recs, st, err := store.ReadFile(histPath)
	if err != nil {
		fmt.Fprintln(stderr, "hit:", err)
		return 1
	}
	tl.Mark("read")
	if st.Corrupt > 0 {
		fmt.Fprintf(stderr, "hit: skipped %d damaged lines in %s\n", st.Corrupt, histPath)
	}
	h := store.Build(recs)
	tl.Mark("build")

	at, err := clockNow(env)
	if err != nil {
		fmt.Fprintln(stderr, "hit:", err)
		return 1
	}
	q := buildQuery(searchArgs{
		Query: *query, Scope: *scope, Sort: *order, Cwd: *cwd, Session: *session,
		Host: *host, Shell: *shell, OkOnly: *okOnly, Limit: *limit,
	}, at)

	if *print {
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		for _, r := range search.Search(h, q) {
			if err := enc.Encode(printable(r)); err != nil {
				fmt.Fprintln(stderr, "hit:", err)
				return 1
			}
		}
		// --print with --started-at is the no-TUI way to measure process creation on its
		// own: the terminal is out of the picture, so what is left is the spawn.
		tl.Mark("rank")
		timingf(env, "timing (go, --print): %s", tl)
		return 0
	}

	// cwd, session and host are logged because they decide what every scope but `all`
	// shows, and a scope that comes up empty is indistinguishable from a broken finder
	// without them.
	debugf(env, "search: %d entries, scope=%s sort=%s query=%q cwd=%q session=%q host=%q out=%q",
		len(h.Entries), q.Scope, q.Sort, q.Text, q.Cwd, q.Session, q.Host, *out)
	var log func(string, ...any)
	if env.Getenv("HIT_DEBUG") != "" {
		log = func(format string, args ...any) { debugf(env, "tui: "+format, args...) }
	}
	choice, err := runFinder(h, q, *out != "", log, tl)
	if tl != nil {
		timingf(env, "timing (go): %s", tl)
	}
	if err != nil {
		debugf(env, "finder failed: %v", err)
		fmt.Fprintln(stderr, "hit:", err)
		return 1
	}
	debugf(env, "finder returned: action=%s len(cmd)=%d deleted=%d", choice.Action, len(choice.Cmd), len(choice.Deleted))
	if len(choice.Deleted) > 0 {
		if err := tombstone(histPath, choice.Deleted, at); err != nil {
			fmt.Fprintln(stderr, "hit: could not delete:", err)
		}
	}
	return writeChoice(choice, *out, stdout, stderr)
}

// searchArgs is one finder invocation, however it arrived: from the flags of
// `hit search`, or from a request on the pipe of a resident `hit serve`. Both build their
// search.Query through buildQuery, so the two routes cannot drift apart.
type searchArgs struct {
	Query, Scope, Sort, Cwd, Session, Host, Shell string
	OkOnly                                        bool
	Limit                                         int
}

func buildQuery(a searchArgs, at time.Time) search.Query {
	return search.Query{
		Text: a.Query, Scope: search.Scope(a.Scope), Sort: search.Sort(a.Sort),
		Cwd: a.Cwd, Session: a.Session, Host: a.Host, Shell: a.Shell,
		HideFailed: a.OkOnly, Limit: a.Limit, Now: at,
	}
}

// clockNow is the current time, which HIT_NOW pins for tests.
func clockNow(env paths.Env) (time.Time, error) {
	now, err := clock.FromEnv(env.Getenv)
	if err != nil {
		return time.Time{}, err
	}
	return now(), nil
}

// runFinder is split out so the TUI is the only part that needs a terminal.
// With --out the result goes to that file, so the finder can draw on stdout, which is the
// stream terminals handle best. Without it the result goes to stdout, so the finder draws
// on stderr instead to keep stdout parseable.
func runFinder(h *store.History, q search.Query, hasOut bool, log func(string, ...any),
	tl *timing.Timeline) (*tui.Choice, error) {
	m := tui.New(h, q)
	tl.Mark("rank")
	m.Log = log
	m.Timing = tl
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if !hasOut {
		opts = append(opts, tea.WithOutput(os.Stderr))
	}
	p := tea.NewProgram(m, opts...)
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
