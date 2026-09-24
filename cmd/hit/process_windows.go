//go:build windows

package main

import "golang.org/x/sys/windows"

// parentWatch holds the process the server should not outlive.
//
// It is a *handle*, opened once at startup, and not a process id checked on a timer.
// Windows recycles process ids briskly, so asking "is a process with id N running" asks
// the wrong question: the shell can exit, its id be handed to something else, and the
// check keep answering yes about a stranger. Found from daily use on 2026-09-24 — a
// server whose shell had been killed was still resident, watching a `dotnet` process that
// had inherited its shell's id (C-033).
//
// An open handle also fixes the race it is diagnosing: Windows will not reuse a process
// id while a handle to that process object is open, so from the moment the server starts,
// the id it was given cannot come to mean anything else.
type parentWatch struct{ h windows.Handle }

// watchProcess pins pid. An error means the process was already gone when we looked,
// which the caller treats as "the shell has exited".
func watchProcess(pid int) (*parentWatch, error) {
	// SYNCHRONIZE is the least the handle can carry and is enough to ask whether the
	// process has signalled, which it does when it exits.
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return nil, err
	}
	return &parentWatch{h: h}, nil
}

func (w *parentWatch) alive() bool {
	s, err := windows.WaitForSingleObject(w.h, 0)
	return err == nil && s == uint32(windows.WAIT_TIMEOUT)
}

func (w *parentWatch) close() { windows.CloseHandle(w.h) }
