//go:build !windows

package main

import "syscall"

// processAlive reports whether pid is still running. Signal 0 performs the permission
// and existence checks without delivering anything.
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
