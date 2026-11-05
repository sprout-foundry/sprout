// Package console: terminal re-sync after out-of-band TTY interactions.
//
// A "stuck in a weird state" terminal happens when something OUTSIDE the
// console chrome touches the TTY and leaves it desynced from what the
// footer + input reader believe:
//
//   - a `!` shell command whose child took the TTY over (pager / editor /
//     TUI: entered the alternate screen, hid the cursor, left termios in
//     raw mode) and did not fully restore it;
//   - a 30-minute security-prompt timeout, after which the denial line and
//     the resumed stream land on a terminal the user's terminal emulator
//     may have reflowed or that another process has touched.
//
// The footer's per-draw self-heal (DECSTBM re-assert + absolute CUP rows)
// fixes the SCROLL REGION and the FOOTER's own rows, but it cannot fix
// the terminal's MODES: cooked termios, cursor visibility, and which
// screen buffer the terminal is showing. Those are what this file fixes.
//
// ResyncTerminal is idempotent and safe to call at any return-to-prompt
// seam: every piece it emits is a no-op when the terminal is already in
// the expected state (cooked termios re-asserted from the startup
// snapshot; `?1049l` has no effect when not in the alt screen; `?25h` is
// a re-show of an already-visible cursor). It is deliberately NOT called
// mid-turn: while a turn is in flight the SteerInputReader / streaming
// writer owns the raw-mode TTY and the cursor, and a re-sync there would
// fight the in-progress chrome.

package console

import (
	"os"
	"sync"

	"golang.org/x/term"
)

var (
	globalGroundTruth   *GroundTruthTermios
	globalGroundTruthMu sync.RWMutex
)

// RegisterGlobalGroundTruth installs the process-wide termios snapshot
// used by ResyncTerminal to restore cooked mode. Pass nil to clear.
// Mirrors RegisterGlobalStatusFooter; called once at session start when
// the terminal is in its pristine cooked state (the same snapshot the
// InputReader and SteerInputReader already receive).
func RegisterGlobalGroundTruth(gt *GroundTruthTermios) {
	globalGroundTruthMu.Lock()
	defer globalGroundTruthMu.Unlock()
	globalGroundTruth = gt
}

// ResyncTerminal restores the terminal to a known-good baseline after an
// out-of-band TTY interaction. It fixes the four modes the footer's
// self-healing draw cannot reach:
//
//  1. termios — re-asserts the startup (cooked) snapshot if one is
//     registered. Catches a child that left stdin in raw mode (the
//     "can't type" symptom: ICANON off swallows the user's lines).
//     No-op on non-Unix and when no snapshot is registered (e.g. direct
//     mode, which never captures ground truth).
//  2. alternate screen — exits it (?1049l) if a pager/editor left the
//     terminal in the alt buffer (the "footer broken / everything frozen"
//     symptom: all of sprout's chrome would otherwise keep drawing into
//     the alt screen). ?1049l is a no-op when the terminal is already on
//     the primary screen.
//  3. cursor visibility — shows the cursor (?25h). Catches a child that
//     hid it and never re-showed (the "invisible cursor" symptom).
//  4. footer region — asks the registered global footer to refresh,
//     re-asserting the DECSTBM scroll region and absolutely redrawing the
//     pinned rows so the footer's model and the terminal agree again.
//     Refresh is a no-op when the footer is inactive or prose is
//     streaming, so it is safe at the idle-prompt seam.
//
// All writes go to os.Stdout; the footer writes to its own (typically
// os.Stderr) writer. Both are the same TTY in the normal REPL layout.
//
// Call this at return-to-prompt seams ONLY (after the `!` fast path,
// before each ReadLine). Never mid-turn: while a turn owns the TTY,
// re-showing the cursor and re-asserting the region would race the
// streaming writer and the steer reader's raw mode.
func ResyncTerminal() {
	// 1. termios: cooked mode from the startup snapshot (idempotent;
	// no-op when unregistered or non-Unix).
	if gt := globalGroundTruthSnapshot(); gt != nil {
		gt.EnsureCooked()
	}

	// The escape sequences below only matter when the real output is a
	// terminal; on a pipe they would just pollute the stream.
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return
	}

	// Hold the output lock across the mode writes so a background footer
	// Refresh (SIGWINCH, event subscriber) can't interleave between the
	// alt-screen exit and the cursor re-show.
	LockOutput()
	// 2. exit the alternate screen (no-op if already on primary) and
	// 3. re-show the cursor (idempotent). One write so the two mode
	// changes are atomic with respect to the lock.
	_, _ = os.Stdout.Write([]byte("\x1b[?1049l\x1b[?25h"))
	UnlockOutput()

	// 4. re-assert the footer's scroll region + repaint its pinned rows.
	// Refresh() is internally guarded (no-op when inactive / streaming).
	GetGlobalStatusFooter().Refresh()
}

func globalGroundTruthSnapshot() *GroundTruthTermios {
	globalGroundTruthMu.RLock()
	defer globalGroundTruthMu.RUnlock()
	return globalGroundTruth
}
