//go:build windows

package ipc

import (
	"fmt"
	"net"
	"os/user"

	"github.com/Microsoft/go-winio"
)

// Address is where a client connects. PowerShell opens this with
// System.IO.Pipes.NamedPipeClientStream, so the shell side needs nothing installed.
func Address(name string) string { return `\\.\pipe\` + name }

// Listen opens the named pipe, reachable only by the account that created it.
//
// The DACL is set explicitly from the current user's SID rather than left to a default,
// because "who can talk to this thing" is the first question anyone reviewing a
// long-lived process will ask, and the answer should be visible in the source.
func Listen(name string) (net.Listener, error) {
	u, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("current user: %w", err)
	}
	// D: a DACL, P: protected from inheritance, A: allow, GA: generic all, to this SID
	// alone. No entry for anyone else, so nothing else can open it.
	sddl := fmt.Sprintf("D:P(A;;GA;;;%s)", u.Uid)
	ln, err := winio.ListenPipe(Address(name), &winio.PipeConfig{SecurityDescriptor: sddl})
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", Address(name), err)
	}
	return ln, nil
}

// Dial connects to a running server, or returns ErrNoServer when there is none.
func Dial(name string) (net.Conn, error) {
	c, err := winio.DialPipe(Address(name), nil)
	if err != nil {
		return nil, ErrNoServer
	}
	return c, nil
}
