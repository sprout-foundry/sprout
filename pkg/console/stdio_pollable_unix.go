//go:build unix && !js

package console

import (
	"os"
	"sync"
	"syscall"

	"golang.org/x/term"
)

var (
	pollableStdioOnce sync.Once
	// originalStdio keeps the replaced *os.File values reachable: their
	// finalizers would otherwise close fds 1 and 2 once collected.
	originalStdio []*os.File
)

// MakeStdioPollable re-opens terminal stdout and stderr as netpoller-managed
// files. The input readers set O_NONBLOCK on the terminal for paste
// detection, and fds 0/1/2 of a terminal share one open file description,
// so stdout and stderr turn non-blocking too. Go opened them as blocking,
// non-pollable files, so a write that fills the tty buffer fails with
// EAGAIN after a partial write and the rest is silently dropped — escape
// sequences cut mid-way, scroll margins never set, a garbled footer.
// Call once, before anything captures os.Stdout/os.Stderr.
func MakeStdioPollable() {
	pollableStdioOnce.Do(func() {
		if !term.IsTerminal(int(os.Stdout.Fd())) || !term.IsTerminal(int(os.Stderr.Fd())) {
			return
		}
		out, okOut := reopenPollable(int(os.Stdout.Fd()), "/dev/stdout")
		errOut, okErr := reopenPollable(int(os.Stderr.Fd()), "/dev/stderr")
		if !okOut || !okErr {
			return
		}
		originalStdio = append(originalStdio, os.Stdout, os.Stderr)
		os.Stdout, os.Stderr = out, errOut
	})
}

// reopenPollable wraps fd in a new *os.File registered with the netpoller,
// whose writes wait out EAGAIN instead of failing. os.NewFile only polls a
// descriptor that is non-blocking when it is wrapped, so the flag is set for
// the call and cleared after: readers that expect a blocking fd see no
// change, and the file stays pollable for when an input reader turns
// non-blocking on again.
func reopenPollable(fd int, name string) (*os.File, bool) {
	if syscall.SetNonblock(fd, true) != nil {
		return nil, false
	}
	f := os.NewFile(uintptr(fd), name)
	_ = syscall.SetNonblock(fd, false)
	return f, true
}
