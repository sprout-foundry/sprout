package console

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// Terminal resize semantics differ in ways the footer's stale-row clear
// window must know about:
//
//   - Top-anchored (xterm and most descendants): on a grow, existing rows
//     keep their absolute screen positions and new blank rows appear at the
//     bottom — a stranded footer from the old geometry stays mid-screen and
//     must be cleared by its OLD absolute position.
//   - Bottom-anchored (Termux's TerminalBuffer.resize): on a grow, content is
//     shifted DOWN as transcript rows are pulled into the view, so everything
//     (including a stale footer) stays glued to the bottom and lands inside
//     the NEW-geometry window. Clearing by the old absolute position instead
//     wipes the freshly pulled-down conversation lines above the footer.
//
// Termux identifies itself in the secondary device attributes (DA2) reply
// with a distinctive middle field: CSI > 41 ; 320 ; 0 c. The probe works both
// for native sessions and through ssh (the emulator at the far end answers).
var (
	flavorOnce    sync.Once
	flavorCached  bool
	probeOverride func() bool
)

// probeDeadline bounds one DA2 request/reply round trip. Generous vs
// the emulator's typical <10ms answer; still short enough that a
// silent terminal doesn't stall footer startup noticeably.
const probeDeadline = 250 * time.Millisecond

// bottomAnchoredResize reports whether the controlling terminal reflows
// bottom-anchored on resize (Termux and kin). Probed at most once; every
// later call returns the cached answer. While a test override is installed
// it takes precedence over the cache so tests stay order-independent.
// Errors and non-TTY stdin default to false (top-anchored), the
// conservative historical behavior.
func bottomAnchoredResize() bool {
	if probeOverride != nil {
		return probeOverride()
	}
	flavorOnce.Do(func() {
		flavorCached = probeBottomAnchored(os.Stdin.Fd(), os.Stdout)
	})
	return flavorCached
}

// resetFlavorCache exists for tests.
func resetFlavorCache() {
	flavorOnce = sync.Once{}
	flavorCached = false
}

// probeBottomAnchored writes a DA2 request to w and reads the reply from the
// fd, returning true when the reply matches Termux's identifier. The fd is
// put in raw mode for the round trip and always restored. Anything the user
// typed during the 250ms window is consumed; at footer start that window is
// acceptable in exchange for correct resize painting for the whole session.
//
// The reply is read with raw poll+read syscalls, NOT os.NewFile(fd). Wrap-
// ping a borrowed descriptor in an *os.File is fatal here: os.NewFile
// registers a runtime finalizer that closes the wrapped fd when the object
// becomes unreachable, so the next GC cycle after the probe returned would
// close(0) — the caller's stdin — and the interactive REPL would die with
// "stdin read error: read /dev/stdin: bad file descriptor" (EBADF) on the
// next turn. Raw syscalls never close the borrowed fd.
func probeBottomAnchored(fd uintptr, w *os.File) bool {
	old, err := term.MakeRaw(int(fd))
	if err != nil {
		return false
	}
	// The probe result is best-effort; a failed restore would leave the
	// user's terminal in raw mode, so surface it rather than swallow it.
	defer func() {
		if restoreErr := term.Restore(int(fd), old); restoreErr != nil {
			fmt.Fprintf(os.Stderr, "console: failed to restore terminal state after DA2 probe: %v\n", restoreErr)
		}
	}()

	if _, err := w.WriteString("\033[>c"); err != nil {
		return false
	}
	reply := string(readTTYReply(int(fd), probeDeadline))
	// Match the reply from its escape-sequence start; leading consumed
	// keystrokes in the buffer must not shift the DA2 parse.
	if idx := strings.Index(reply, "\033[>"); idx >= 0 {
		reply = reply[idx:]
	}
	return isTermuxDA2(reply)
}

// isTermuxDA2 reports whether a DA2 reply names Termux. Termux answers
// "CSI > 41 ; 320 ; 0 c"; the 320 in the version-level field is the
// fingerprint (xterm reports its patch number there, iTerm2 95, and so on).
func isTermuxDA2(reply string) bool {
	return strings.HasPrefix(reply, "\033[>") && strings.Contains(reply, ";320;")
}
