//go:build !windows

package main

import "syscall"

// parentWatch holds the process the server should not outlive. See the Windows file for
// why this is a type rather than a bare "is this id running" call.
//
// Unix has no handle that pins a process id, so this keeps the id and the caveat: ids are
// recycled here too, only far more slowly, and a shell exiting normally takes the server
// with it long before the id comes round again. A pidfd (Linux 5.3+) would close the gap
// properly and is worth having when the server ships beyond Windows (C-033).
type parentWatch struct{ pid int }

func watchProcess(pid int) (*parentWatch, error) {
	w := &parentWatch{pid: pid}
	if !w.alive() {
		return nil, syscall.ESRCH
	}
	return w, nil
}

// alive asks with signal 0, which performs the permission and existence checks without
// delivering anything.
func (w *parentWatch) alive() bool { return syscall.Kill(w.pid, 0) == nil }

func (w *parentWatch) close() {}
