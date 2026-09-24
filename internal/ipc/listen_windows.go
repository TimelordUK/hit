//go:build windows

package ipc

import (
	"fmt"
	"net"
	"os/user"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
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

// DialTimeout is Dial with a bound on waiting for a free pipe instance. A server that is
// drawing the finder has none free, so without one a status check would wait for the
// person at that other shell to finish.
func DialTimeout(name string, d time.Duration) (net.Conn, error) {
	c, err := winio.DialPipe(Address(name), &d)
	if err != nil {
		return nil, ErrNoServer
	}
	return c, nil
}

// List names every hit endpoint this machine has open, whichever shell owns it. The pipe
// namespace is a directory that FindFirstFile can walk; os.ReadDir cannot open it.
func List() ([]string, error) {
	var fd windows.Win32finddata
	h, err := windows.FindFirstFile(windows.StringToUTF16Ptr(Address("*")), &fd)
	if err != nil {
		return nil, fmt.Errorf("list pipes: %w", err)
	}
	defer windows.FindClose(h)
	var names []string
	for {
		if n := windows.UTF16ToString(fd.FileName[:]); strings.HasPrefix(n, "hit-") {
			names = append(names, n)
		}
		if err := windows.FindNextFile(h, &fd); err != nil {
			break
		}
	}
	return names, nil
}
