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
func RestoreTerminal() {
	StopGlobalStatusFooter()
	if term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Fprint(os.Stdout, bracketedPasteDisable+MouseTrackingDisable+modifyOtherKeysDisable+"\033[?25h")
	}
	restoreStartupConsoleModes()
}
