package main

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/TimelordUK/hit/internal/ipc"
)

// The ping reply says which process answered, so status can name it (C-034).
func TestPingSaysWhoAnswered(t *testing.T) {
	name, _, _ := serveInBackground(t, time.Minute, echo)
	got := dialAndAsk(t, name, Request{Ping: true})
	if got.Server == nil {
		t.Fatalf("a ping reply carries no server info: %+v", got)
	}
	if got.Server.Pid != os.Getpid() {
		t.Errorf("pid: got %d, want %d", got.Server.Pid, os.Getpid())
	}
	if got.Server.Endpoint != ipc.Address(name) {
		t.Errorf("endpoint: got %q, want %q", got.Server.Endpoint, ipc.Address(name))
	}
}

// "Last used" means the last finder, not the last ping: status pings, and a report that
// every status check refreshed would say a server abandoned all afternoon was just used.
func TestPingsDoNotCountAsUse(t *testing.T) {
	name, _, _ := serveInBackground(t, time.Minute, echo)
	for i := 0; i < 3; i++ {
		dialAndAsk(t, name, Request{Ping: true})
	}
	if got := dialAndAsk(t, name, Request{Ping: true}).Server; got.Requests != 0 || !got.LastUsed.IsZero() {
		t.Errorf("pings counted as use: %+v", got)
	}
	dialAndAsk(t, name, Request{Query: "q"})
	dialAndAsk(t, name, Request{Query: "q"})
	got := dialAndAsk(t, name, Request{Ping: true}).Server
	if got.Requests != 2 || got.LastUsed.IsZero() {
		t.Errorf("two finder requests: got %d, last used %v", got.Requests, got.LastUsed)
	}
}

func TestListFindsARunningServer(t *testing.T) {
	name, _, _ := serveInBackground(t, time.Minute, echo)
	names, err := ipc.List()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(names, name) {
		t.Errorf("%q not in %v", name, names)
	}
}

func TestStatusAnswersForALiveServer(t *testing.T) {
	name, _, _ := serveInBackground(t, time.Minute, echo)
	st := pingEndpoint(name, 0)
	if !st.Answered || st.Server == nil {
		t.Fatalf("no answer: %+v", st)
	}
	if st.Stale {
		t.Errorf("same binary reported as stale: %+v", st)
	}
}

// A server drawing a finder in another shell cannot answer until it closes. Status must
// say so and move on, not wait for the person at that shell.
func TestStatusDoesNotWaitOnABusyServer(t *testing.T) {
	release := make(chan struct{})
	name, _, _ := serveInBackground(t, time.Minute, func(r Request) Response {
		<-release
		return echo(r)
	})
	busy := make(chan struct{})
	go func() { dialAndAsk(t, name, Request{Query: "held"}); close(busy) }()
	t.Cleanup(func() { close(release); <-busy })
	time.Sleep(50 * time.Millisecond) // let the held request be accepted

	start := time.Now()
	st := pingEndpoint(name, 0)
	if el := time.Since(start); el > 3*pingTimeout {
		t.Errorf("status waited %s on a busy server", el)
	}
	if st.Answered || st.Error == "" {
		t.Errorf("a busy server should be reported as not answering: %+v", st)
	}
}

func TestNoEndpointIsReportedAsNotAnswering(t *testing.T) {
	st := pingEndpoint(ipc.Name("not-running-"+time.Now().Format("150405.000000000")), 0)
	if st.Answered {
		t.Errorf("nothing there, yet it answered: %+v", st)
	}
}

// The two things status exists to surface: a server from before the last install, and
// one whose shell has gone. Both must be said in words, not left to a field to compare.
func TestStatusNamesStaleAndOrphanedServers(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	rep := statusReport{Version: "v2", Exe: `C:\bin\hit.exe`, Servers: []endpointStatus{
		{Endpoint: `\\.\pipe\hit-a`, Answered: true, Version: "v2", Mine: true, Server: &ServerInfo{
			Pid: 10, Parent: 20, ParentAlive: true, Started: now.Add(-90 * time.Minute), Idle: "30m0s",
			LastUsed: now.Add(-3 * time.Minute), Requests: 7,
		}},
		{Endpoint: `\\.\pipe\hit-b`, Answered: true, Version: "v1", Stale: true, Server: &ServerInfo{
			Pid: 11, Parent: 23740, Started: now.Add(-5 * time.Hour), Idle: "30m0s",
		}},
		{Endpoint: `\\.\pipe\hit-c`, Error: "no answer: busy drawing a finder, or wedged"},
	}}
	var b bytes.Buffer
	writeStatus(&b, rep, now)
	out := b.String()
	for _, want := range []string{
		`\\.\pipe\hit-a  (this shell)`,
		"pid      10",
		"parent   20, alive",
		"up       1h30m",
		"used     7 times, last 3m ago",
		"v1  — not v2: started before the last install",
		"parent   23740, gone",
		"used     never",
		"no answer: busy drawing a finder",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestStatusWithNothingRunning(t *testing.T) {
	var b bytes.Buffer
	writeStatus(&b, statusReport{Version: "v2"}, time.Now())
	if !strings.Contains(b.String(), "no servers running") {
		t.Errorf("got %q", b.String())
	}
}
