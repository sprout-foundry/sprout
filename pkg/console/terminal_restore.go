package console

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

// RestoreTerminal undoes the session's terminal changes ahead of a hard
// exit. os.Exit skips deferred cleanup, which left the footer painted, the
// scroll region confining the shell's prompt to the top rows, and input
// modes (bracketed paste, mouse reporting, raw console input) active in the
// user's shell after sprout was gone.
//
// It also re-asserts the terminal MODES a `!` child (or anything that took
// the TTY over) may have left desynced: cooked termios from the startup
// snapshot, the primary screen (a pager/editor may have left the terminal
// in the alternate screen buffer), and a visible cursor. All idempotent.
func RestoreTerminal() {
	StopGlobalStatusFooter()
	// termios: cooked mode from the startup snapshot (no-op when
	// unregistered or non-Unix).
	if gt := globalGroundTruthSnapshot(); gt != nil {
		gt.EnsureCooked()
	}
	if term.IsTerminal(int(os.Stdout.Fd())) {
		// ?1049l exits the alternate screen (no-op if already on the
		// primary); ?25h re-shows the cursor.
		_, _ = fmt.Fprint(os.Stdout, "\033[?1049l"+bracketedPasteDisable+MouseTrackingDisable+modifyOtherKeysDisable+"\033[?25h")
	}
	restoreStartupConsoleModes()
}
