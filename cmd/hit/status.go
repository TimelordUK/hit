package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/TimelordUK/hit/internal/ipc"
	"github.com/TimelordUK/hit/internal/paths"
)

// `hit status` (C-034): what is resident, and whose is it.
//
// Every endpoint on the machine is asked, not only this shell's, because the question
// usually comes from something unexplained — a server that outlived its shell (C-033),
// or one still running the binary before the last install (S-031). Asking only about the
// current session would miss exactly those.

// endpointStatus is one row: an endpoint and what, if anything, answered on it.
type endpointStatus struct {
	Endpoint string      `json:"endpoint"`
	Answered bool        `json:"answered"`
	Version  string      `json:"version,omitempty"`
	Server   *ServerInfo `json:"server,omitempty"`
	// Mine is true when the server's parent is the shell that ran this command.
	Mine bool `json:"mine,omitempty"`
	// Stale is true when the server runs a different version from this binary.
	Stale bool   `json:"stale,omitempty"`
	Error string `json:"error,omitempty"`
}

type statusReport struct {
	Version string           `json:"version"`
	Exe     string           `json:"exe,omitempty"`
	Shell   int              `json:"shell"`
	Servers []endpointStatus `json:"servers"`
}

// pingTimeout bounds each ask. A server busy drawing a finder in another shell will not
// answer until that finder closes, and status must not wait for it.
const pingTimeout = time.Second

func runStatus(args []string, env paths.Env, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("hit status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the report as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	names, err := ipc.List()
	if err != nil {
		fmt.Fprintln(stderr, "hit:", err)
		return 1
	}
	rep := statusReport{Version: version, Shell: os.Getppid(), Servers: []endpointStatus{}}
	if exe, err := os.Executable(); err == nil {
		rep.Exe = exe
	}
	for _, n := range names {
		rep.Servers = append(rep.Servers, pingEndpoint(n, rep.Shell))
	}
	// This shell's first, then oldest first: the one you asked about, then the ones
	// most likely to be leftovers.
	sort.SliceStable(rep.Servers, func(i, j int) bool {
		a, b := rep.Servers[i], rep.Servers[j]
		if a.Mine != b.Mine {
			return a.Mine
		}
		return started(a).Before(started(b))
	})
	debugf(env, "status: %d endpoints", len(names))

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			fmt.Fprintln(stderr, "hit:", err)
			return 1
		}
		return 0
	}
	writeStatus(stdout, rep, time.Now())
	return 0
}

func started(e endpointStatus) time.Time {
	if e.Server == nil {
		return time.Time{}
	}
	return e.Server.Started
}

func pingEndpoint(name string, shell int) endpointStatus {
	st := endpointStatus{Endpoint: ipc.Address(name)}
	conn, err := ipc.DialTimeout(name, pingTimeout)
	if err != nil {
		st.Error = "no answer: busy drawing a finder, or wedged"
		return st
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(pingTimeout))
	if _, err := conn.Write([]byte("{\"ping\":true}\n")); err != nil {
		st.Error = err.Error()
		return st
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		st.Error = "no answer: busy drawing a finder, or wedged"
		return st
	}
	var res Response
	if err := json.Unmarshal(line, &res); err != nil || !res.Pong {
		st.Error = fmt.Sprintf("not a hit server reply: %q", strings.TrimSpace(string(line)))
		return st
	}
	st.Answered, st.Version, st.Server = true, res.Version, res.Server
	st.Stale = res.Version != version
	st.Mine = res.Server != nil && res.Server.Parent != 0 && res.Server.Parent == shell
	return st
}

func writeStatus(w io.Writer, rep statusReport, now time.Time) {
	fmt.Fprintf(w, "hit %s  %s\n", rep.Version, rep.Exe)
	if len(rep.Servers) == 0 {
		fmt.Fprintln(w, "no servers running")
		return
	}
	for _, s := range rep.Servers {
		fmt.Fprintln(w)
		head := s.Endpoint
		if s.Mine {
			head += "  (this shell)"
		}
		fmt.Fprintln(w, head)
		if !s.Answered {
			fmt.Fprintf(w, "  %s\n", s.Error)
			continue
		}
		v := s.Version
		if s.Stale {
			v += fmt.Sprintf("  — not %s: started before the last install", rep.Version)
		}
		fmt.Fprintf(w, "  version  %s\n", v)
		i := s.Server
		if i == nil {
			continue // a server older than status itself: it answers, but says no more
		}
		fmt.Fprintf(w, "  pid      %d\n", i.Pid)
		switch {
		case i.Parent == 0:
			fmt.Fprintln(w, "  parent   none: not watching a shell")
		case i.ParentAlive:
			fmt.Fprintf(w, "  parent   %d, alive\n", i.Parent)
		default:
			fmt.Fprintf(w, "  parent   %d, gone: exits within 5s\n", i.Parent)
		}
		if i.Exe != "" {
			fmt.Fprintf(w, "  exe      %s\n", i.Exe)
		}
		fmt.Fprintf(w, "  up       %s, idle limit %s\n", ago(now.Sub(i.Started)), i.Idle)
		if i.LastUsed.IsZero() {
			fmt.Fprintln(w, "  used     never")
		} else {
			fmt.Fprintf(w, "  used     %d times, last %s ago\n", i.Requests, ago(now.Sub(i.LastUsed)))
		}
	}
}

// ago rounds a duration to what a person reads: seconds under a minute, minutes under an
// hour, hours and minutes beyond.
func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}
