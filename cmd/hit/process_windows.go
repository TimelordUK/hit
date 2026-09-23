//go:build windows

package main

import "golang.org/x/sys/windows"

// processAlive reports whether pid is still running. SYNCHRONIZE is the least the
// handle can carry and is enough to ask whether the process has signalled, which it
// does when it exits.
func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	s, err := windows.WaitForSingleObject(h, 0)
	return err == nil && s == uint32(windows.WAIT_TIMEOUT)
}
