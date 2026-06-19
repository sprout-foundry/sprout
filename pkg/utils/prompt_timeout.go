package utils

import (
	"bufio"
	"errors"
	"time"
)

// ApprovalPromptTimeout bounds how long an interactive security prompt —
// the CLI yes/no confirmation, the 4-option approval menu, the filesystem
// approval menu, and the pkg/console arrow-key picker — blocks waiting for
// the user before it gives up and denies for safety.
//
// It mirrors security.DefaultTimeout (the WebUI event-bus wait, also 30 min)
// so a user gets the same grace window whether the prompt renders in the
// terminal or the browser. We keep the value here rather than importing
// pkg/security so pkg/utils stays leaf-level.
//
// A finite bound is the fix for the "agent wedged forever / terminal stuck
// in raw mode" failure: when stdin is open but idle (the user walked away,
// or the harness isn't forwarding keystrokes), the old readers blocked
// indefinitely and the raw-mode picker never restored the terminal. Now
// every surface releases stdin, restores cooked mode, and surfaces a clear
// timeout deny.
//
// The previous 5-minute value was too short for a human reviewing a complex
// command (terraform plan, a long migration script). 30 minutes matches the
// webui default and was deliberately chosen there because "a false-deny
// after 5 minutes was a recurring UX complaint" — the same applies to the
// CLI surface.
const ApprovalPromptTimeout = 30 * time.Minute

// ErrPromptTimeout is returned by ReadLineWithTimeout when no line arrives
// within the deadline. Callers treat it as a deny-for-safety signal,
// distinct from a genuine read error (closed stdin).
var ErrPromptTimeout = errors.New("prompt timed out waiting for input")

// ReadLineWithTimeout reads a single newline-terminated line from reader,
// returning ErrPromptTimeout if nothing arrives within d.
//
// The blocking ReadString runs in a goroutine. On timeout the goroutine is
// left to resolve on its own (it completes when a line eventually arrives or
// stdin closes). Callers MUST return after a timeout rather than loop with
// the same reader, so at most one read goroutine is ever outstanding per
// reader — overlapping ReadString calls on one bufio.Reader would race.
func ReadLineWithTimeout(reader *bufio.Reader, d time.Duration) (string, error) {
	type readResult struct {
		line string
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		line, err := reader.ReadString('\n')
		ch <- readResult{line: line, err: err}
	}()

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case res := <-ch:
		return res.line, res.err
	case <-timer.C:
		return "", ErrPromptTimeout
	}
}
