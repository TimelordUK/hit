//go:build !windows

package ipc

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Address is the socket path. XDG_RUNTIME_DIR when there is one (it is already per user
// and cleaned up at logout), the temp directory otherwise.
func Address(name string) string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, name+".sock")
}

// Listen opens the socket with permissions that admit only its owner. A socket left
// behind by a killed server is removed first, since bind fails on an existing path.
func Listen(name string) (net.Listener, error) {
	path := Address(name)
	if c, err := net.Dial("unix", path); err == nil {
		c.Close()
		return nil, fmt.Errorf("a server is already listening on %s", path)
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, fmt.Errorf("chmod %s: %w", path, err)
	}
	return ln, nil
}

// Dial connects to a running server, or returns ErrNoServer when there is none.
func Dial(name string) (net.Conn, error) {
	c, err := net.Dial("unix", Address(name))
	if err != nil {
		return nil, ErrNoServer
	}
	return c, nil
}

// DialTimeout is Dial with a bound on connecting.
func DialTimeout(name string, d time.Duration) (net.Conn, error) {
	c, err := net.DialTimeout("unix", Address(name), d)
	if err != nil {
		return nil, ErrNoServer
	}
	return c, nil
}

// List names every hit endpoint in the socket directory. A socket left by a killed server
// is listed too; it simply will not answer.
func List() ([]string, error) {
	paths, err := filepath.Glob(Address("hit-*"))
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(paths))
	for _, p := range paths {
		names = append(names, strings.TrimSuffix(filepath.Base(p), ".sock"))
	}
	return names, nil
}
