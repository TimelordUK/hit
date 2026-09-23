package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/TimelordUK/hit/internal/ipc"
	"github.com/TimelordUK/hit/internal/paths"
	"github.com/TimelordUK/hit/internal/store"
)

// The resident finder (C-031).
//
// On a managed machine the expensive part of Ctrl+R is not the search, it is starting a
// process at all: a file the endpoint agent has not seen costs seconds on first launch
// and milliseconds once it is known, and that verdict is evicted through the day. So the
// process is started once per shell and kept, and the shell asks it to draw.
//
// It is deliberately not a service. No registration, no elevation, no autostart, no
// persistence: a child of the shell that started it, on a pipe only its own account can
// open, which exits when the shell goes away or after sitting idle. That is what MSBuild
// nodes, VBCSCompiler and gopls all are.
//
// It draws on the console it inherited at startup, exactly as the cold spawn does, so the
// terminal handling is the same code that has always run.

// Request is one finder invocation asked for over the pipe. The fields mirror the flags
// of `hit search`, because it is the same operation by another route.
type Request struct {
	Query   string `json:"query"`
	Scope   string `json:"scope"`
	Cwd     string `json:"cwd"`
	Session string `json:"session"`
	Host    string `json:"host"`
	Shell   string `json:"shell"`
	OkOnly  bool   `json:"okOnly,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	// Ping asks only whether the server is alive, and draws nothing. The shell uses it
	// to decide whether it can skip the cold spawn.
	Ping bool `json:"ping,omitempty"`
}

// Response is the finder's answer. Its shape is the same JSON the cold path writes to
// --out, so the shell parses one thing however the finder was reached.
type Response struct {
	Action  string   `json:"action,omitempty"`
	Cmd     string   `json:"cmd,omitempty"`
	ID      string   `json:"id,omitempty"`
	Deleted []string `json:"deleted,omitempty"`
	Pong    bool     `json:"pong,omitempty"`
	Error   string   `json:"error,omitempty"`
	Version string   `json:"version,omitempty"`
}

// handler runs one request. Injected so the serve loop can be tested without a console.
type handler func(Request) Response

func runServe(args []string, env paths.Env, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("hit serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		session = fs.String("session", "", "the shell's session id; names the endpoint")
		idle    = fs.Duration("idle", 30*time.Minute, "exit after this long with no request")
		parent  = fs.Int("parent", 0, "exit when this process id goes away (0: don't watch)")
		quiet   = fs.Bool("quiet", false, "say nothing on stdout once listening")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	name := ipc.Name(*session)
	ln, err := ipc.Listen(name)
	if err != nil {
		// Already listening is the ordinary race of two shells starting at once, and is
		// not an error worth shouting about: the other one will serve.
		fmt.Fprintln(stderr, "hit:", err)
		return 1
	}
	defer ln.Close()

	if !*quiet {
		fmt.Fprintln(stdout, ipc.Address(name))
	}
	debugf(env, "serve: listening on %s, idle %s, parent %d", ipc.Address(name), *idle, *parent)

	s := &server{ln: ln, idle: *idle, env: env}
	if *parent > 0 {
		go s.watchParent(*parent)
	}
	s.run(func(r Request) Response { return s.finder(r) })
	debugf(env, "serve: exiting (%s)", s.why)
	return 0
}

type server struct {
	ln   net.Listener
	idle time.Duration
	env  paths.Env

	mu    sync.Mutex
	timer *time.Timer
	why   string
	done  bool
}

// run accepts one caller at a time. Serial by design: there is one console, so two
// finders could not both draw on it anyway.
func (s *server) run(h handler) {
	s.why = "idle"
	s.timer = time.AfterFunc(s.idle, func() { s.stop("idle") })
	defer s.timer.Stop()

	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return // the listener was closed: idle timeout, parent gone, or shutdown
		}
		s.keepAlive()
		s.serveConn(conn, h)
		if s.finished() {
			return
		}
		s.keepAlive()
	}
}

// keepAlive restarts the idle countdown. The clock measures time with nobody asking,
// not time since startup, so a finder left open all afternoon does not expire underneath
// the person using it.
func (s *server) keepAlive() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timer != nil && !s.done {
		s.timer.Reset(s.idle)
	}
}

func (s *server) stop(why string) {
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return
	}
	s.done, s.why = true, why
	s.mu.Unlock()
	s.ln.Close()
}

func (s *server) finished() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done
}

// serveConn reads newline-delimited JSON requests and answers each in turn. A connection
// that dies mid-request is the shell being interrupted, which is not the server's
// problem: it goes back to waiting.
func (s *server) serveConn(conn net.Conn, h handler) {
	defer conn.Close()
	br := bufio.NewReaderSize(conn, 64*1024)
	enc := json.NewEncoder(conn)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) == 0 || err != nil {
			return
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			_ = enc.Encode(Response{Error: "bad request: " + err.Error()})
			return
		}
		if req.Ping {
			if err := enc.Encode(Response{Pong: true, Version: version}); err != nil {
				return
			}
			continue
		}
		if err := enc.Encode(h(req)); err != nil {
			return
		}
	}
}

// watchParent exits when the shell that started us has gone. The console closing usually
// takes the server with it, but a shell that exits while something else holds the console
// would otherwise leave this process behind, and an unexplained resident process is
// exactly what gets asked about on a managed machine.
func (s *server) watchParent(pid int) {
	for {
		time.Sleep(5 * time.Second)
		if s.finished() {
			return
		}
		if !processAlive(pid) {
			s.stop("parent gone")
			return
		}
	}
}

// finder is the real handler: reload the history, draw, answer. The history is re-read
// every time rather than cached, because commands have been appended since the last one
// and a finder that cannot see what you just ran would be worse than a slow one.
func (s *server) finder(r Request) Response {
	tl := newServeTimeline(s.env)
	histPath, err := paths.History(s.env)
	if err != nil {
		return Response{Error: err.Error()}
	}
	tl.Mark("init")
	recs, st, err := store.ReadFile(histPath)
	if err != nil {
		return Response{Error: err.Error()}
	}
	tl.Mark("read")
	if st.Corrupt > 0 {
		debugf(s.env, "serve: skipped %d damaged lines in %s", st.Corrupt, histPath)
	}
	at, err := clockNow(s.env)
	if err != nil {
		return Response{Error: err.Error()}
	}
	h := store.Build(recs)
	tl.Mark("build")
	q := buildQuery(searchArgs{
		Query: r.Query, Scope: r.Scope, Cwd: r.Cwd, Session: r.Session,
		Host: r.Host, Shell: r.Shell, OkOnly: r.OkOnly, Limit: r.Limit,
	}, at)

	var log func(string, ...any)
	if s.env.Getenv("HIT_DEBUG") != "" {
		log = func(format string, args ...any) { debugf(s.env, "serve/tui: "+format, args...) }
	}
	// hasOut is true: the cold path passes it when the choice goes somewhere other than
	// stdout, which makes the finder draw on stdout — the stream terminals handle best.
	// Here the choice goes down the pipe, so the same applies.
	choice, err := runFinder(h, q, true, log, tl)
	if tl != nil {
		timingf(s.env, "timing (go, served): %s", tl)
	}
	if err != nil {
		return Response{Error: err.Error()}
	}
	if len(choice.Deleted) > 0 {
		if terr := tombstone(histPath, choice.Deleted, at); terr != nil {
			debugf(s.env, "serve: could not delete: %v", terr)
		}
	}
	return Response{
		Action: string(choice.Action), Cmd: choice.Cmd, ID: choice.ID, Deleted: choice.Deleted,
	}
}
