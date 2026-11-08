package utils

import (
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

// ErrPromptTimeout is returned by LineReader.ReadLine when no line arrives
// within the deadline. Callers treat it as a deny-for-safety signal,
// distinct from a genuine read error (closed stdin).
//
// On a timeout nothing is left parked on stdin: the read was poll-gated
// and synchronous, so bytes the user typed stay queued on the descriptor
// for whichever reader takes over next.
var ErrPromptTimeout = errors.New("prompt timed out waiting for input")
