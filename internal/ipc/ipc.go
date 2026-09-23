// Package ipc is the control channel between the shell and a resident `hit serve`
// (C-031). It carries requests only: the finder still draws on the console the server
// inherited when the shell started it, so the drawing path is the same one the cold
// spawn has always used and nothing about the terminal changes.
//
// The endpoint is per user and per shell session. Windows gets a named pipe, which is
// invisible to anything enumerating sockets and is the platform's own IPC; elsewhere a
// unix socket in the user's runtime directory. Neither is reachable from another
// account, and neither involves a listening port.
package ipc

import (
	"fmt"
	"regexp"
	"strings"
)

// safe strips anything that has no business in a pipe name or a path. Session ids are
// ULIDs, so in practice nothing is stripped; this is here so a hand-passed name cannot
// escape the namespace.
var safe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// Name is the endpoint name for a session id.
func Name(session string) string {
	s := safe.ReplaceAllString(session, "")
	if s == "" {
		s = "default"
	}
	if len(s) > 48 {
		s = s[:48]
	}
	return "hit-" + strings.ToLower(s)
}

// ErrNoServer reports that nothing is listening, which is the ordinary case: the shell
// then falls back to spawning the finder the slow way.
var ErrNoServer = fmt.Errorf("no hit server listening")
