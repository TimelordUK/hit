package main

import (
	"bufio"
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/TimelordUK/hit/internal/ipc"
)

// dialAndAsk sends one request and reads one response.
func dialAndAsk(t *testing.T, name string, req Request) Response {
	t.Helper()
	conn, err := ipc.Dial(name)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	return ask(t, conn, req)
}

func ask(t *testing.T, conn net.Conn, req Request) Response {
	t.Helper()
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(append(b, '\n')); err != nil {
		t.Fatalf("write: %v", err)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var res Response
	if err := json.Unmarshal(line, &res); err != nil {
		t.Fatalf("decode %q: %v", line, err)
	}
	return res
}

// serveInBackground starts a server on a test-unique endpoint with a stub handler, and
// returns its name plus a channel that closes when it stops.
func serveInBackground(t *testing.T, idle time.Duration, h handler) (string, *server, chan struct{}) {
	t.Helper()
	name := ipc.Name(t.Name() + "-" + time.Now().Format("150405.000000000"))
	ln, err := ipc.Listen(name)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &server{ln: ln, idle: idle}
	stopped := make(chan struct{})
	go func() { s.run(h); close(stopped) }()
	t.Cleanup(func() {
		s.stop("test over")
		<-stopped
	})
	return name, s, stopped
}

func echo(r Request) Response {
	return Response{Action: "insert", Cmd: "ran: " + r.Query}
}

func TestServeAnswersARequest(t *testing.T) {
	name, _, _ := serveInBackground(t, time.Minute, echo)
	got := dialAndAsk(t, name, Request{Query: "git st"})
	if got.Cmd != "ran: git st" {
		t.Errorf("got %+v", got)
	}
	if got.Action != "insert" {
		t.Errorf("action: got %q", got.Action)
	}
}

// The shell pings before trusting the server, and a ping must not draw anything.
func TestPingDoesNotReachTheHandler(t *testing.T) {
	var called int
	name, _, _ := serveInBackground(t, time.Minute, func(r Request) Response {
		called++
		return echo(r)
	})
	got := dialAndAsk(t, name, Request{Ping: true})
	if !got.Pong {
		t.Errorf("no pong: %+v", got)
	}
	if called != 0 {
		t.Errorf("ping ran the finder %d times", called)
	}
}

// Several recalls in one session is the whole point: the server must stay up between
// them rather than serving once and stopping.
func TestServeHandlesManyRequestsInSequence(t *testing.T) {
	name, _, stopped := serveInBackground(t, time.Minute, echo)
	for i := 0; i < 25; i++ {
		if got := dialAndAsk(t, name, Request{Query: "q"}); got.Cmd != "ran: q" {
			t.Fatalf("request %d: %+v", i, got)
		}
	}
	select {
	case <-stopped:
		t.Fatal("server stopped while still being used")
	default:
	}
}

// One connection may carry several requests, which is what the shell does when it keeps
// the pipe open for a session.
func TestOneConnectionCanCarryManyRequests(t *testing.T) {
	name, _, _ := serveInBackground(t, time.Minute, echo)
	conn, err := ipc.Dial(name)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	br := bufio.NewReader(conn)
	for i := 0; i < 5; i++ {
		b, _ := json.Marshal(Request{Query: "x"})
		if _, err := conn.Write(append(b, '\n')); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		line, err := br.ReadBytes('\n')
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		var res Response
		if err := json.Unmarshal(line, &res); err != nil {
			t.Fatalf("decode %d: %v", i, err)
		}
		if res.Cmd != "ran: x" {
			t.Fatalf("request %d: %+v", i, res)
		}
	}
}

func TestIdleTimeoutStopsTheServer(t *testing.T) {
	_, s, stopped := serveInBackground(t, 120*time.Millisecond, echo)
	select {
	case <-stopped:
		if s.why != "idle" {
			t.Errorf("stopped because %q, want idle", s.why)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server outlived its idle timeout")
	}
}

// The countdown measures time with nobody asking. A finder left open must not expire
// under the person using it, and nor must a steady drip of recalls ever kill the server.
func TestUseKeepsTheServerAlive(t *testing.T) {
	name, _, stopped := serveInBackground(t, 250*time.Millisecond, echo)
	for i := 0; i < 6; i++ {
		time.Sleep(100 * time.Millisecond)
		if got := dialAndAsk(t, name, Request{Query: "q"}); got.Cmd == "" {
			t.Fatalf("request %d got nothing", i)
		}
		select {
		case <-stopped:
			t.Fatalf("server expired after %d requests despite being used", i)
		default:
		}
	}
}

func TestMalformedRequestIsRejectedWithoutStoppingTheServer(t *testing.T) {
	name, _, stopped := serveInBackground(t, time.Minute, echo)
	conn, err := ipc.Dial(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("this is not json\n")); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatalf("no answer to a bad request: %v", err)
	}
	var res Response
	_ = json.Unmarshal(line, &res)
	if res.Error == "" {
		t.Errorf("a malformed request should be answered with an error, got %+v", res)
	}
	conn.Close()

	select {
	case <-stopped:
		t.Fatal("one bad request took the server down")
	default:
	}
	if got := dialAndAsk(t, name, Request{Query: "after"}); got.Cmd != "ran: after" {
		t.Errorf("server did not recover: %+v", got)
	}
}

// A caller that connects and vanishes must not wedge the server for everyone else.
func TestAbandonedConnectionDoesNotWedgeTheServer(t *testing.T) {
	name, _, _ := serveInBackground(t, time.Minute, echo)
	conn, err := ipc.Dial(name)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close() // connected, said nothing, went away

	done := make(chan Response, 1)
	go func() { done <- dialAndAsk(t, name, Request{Query: "next"}) }()
	select {
	case got := <-done:
		if got.Cmd != "ran: next" {
			t.Errorf("got %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server wedged after a caller abandoned its connection")
	}
}

// Two shells starting at once must not both claim the endpoint.
func TestSecondListenerOnTheSameNameIsRefused(t *testing.T) {
	name, _, _ := serveInBackground(t, time.Minute, echo)
	if ln, err := ipc.Listen(name); err == nil {
		ln.Close()
		t.Error("a second server was allowed to listen on the same endpoint")
	}
}

func TestDialWithNoServerSaysSo(t *testing.T) {
	if _, err := ipc.Dial(ipc.Name("definitely-not-running-" + time.Now().Format("150405.000000000"))); err == nil {
		t.Error("dialling nothing should fail")
	}
}

func TestEndpointNamesAreSafeAndStable(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"01J8Z4KХ", "hit-01j8z4k"}, // a non-ASCII rune is dropped, not encoded
		{"", "hit-default"},
		{"../../etc/passwd", "hit-etcpasswd"},
		{`a\b/c`, "hit-abc"},
	} {
		if got := ipc.Name(tc.in); got != tc.want {
			t.Errorf("Name(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Requests arriving together are served one at a time, because there is one console.
func TestRequestsAreServedSerially(t *testing.T) {
	var mu sync.Mutex
	var concurrent, peak int
	name, _, _ := serveInBackground(t, time.Minute, func(r Request) Response {
		mu.Lock()
		concurrent++
		if concurrent > peak {
			peak = concurrent
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		concurrent--
		mu.Unlock()
		return echo(r)
	})

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); dialAndAsk(t, name, Request{Query: "q"}) }()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if peak > 1 {
		t.Errorf("%d finders drew at once; there is only one console", peak)
	}
}
