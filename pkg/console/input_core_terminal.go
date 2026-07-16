// Package console: terminal setup and InputReader constructor (split from input_core.go)

package console

import (
	"fmt"
	"io"
	"os"
)

// webTerminalEnabled reports whether the current process is running inside
// the sprout webui embedded terminal. The webui backend sets
// SPROUT_WEB_TERMINAL=1 (and the legacy LEDIT_WEB_TERMINAL=1) when
// spawning the PTY that hosts the user-visible shell. We honor that flag
// to skip terminal capabilities that xterm.js does not implement, so the
// REPL falls back to the legacy xterm escape parser that still
// understands the same keys via conventional sequences.
func webTerminalEnabled() bool {
	return os.Getenv("SPROUT_WEB_TERMINAL") != "" || os.Getenv("LEDIT_WEB_TERMINAL") != ""
}

// writeModifyOtherKeysEnable writes the CSI > 4 ; 1 m enable sequence
// unless we're inside the sprout webui terminal. xterm.js (which backs
// the webui terminal) does not implement the modifyOtherKeys / CSI u
// keyboard protocol — the enable sequence is silently dropped, and worse,
// modified keystrokes (Shift+Enter, Ctrl+Arrow, etc.) then never reach
// us. The legacy xterm escape parser in input_escape_parser.go still
// understands Shift+Enter and the modified-arrow forms via the
// conventional ESC[1;2A..ESC[1;6A encoding, so disabling the protocol
// here is a graceful degradation rather than a feature loss.
func writeModifyOtherKeysEnable(w io.Writer) {
	if webTerminalEnabled() {
		return
	}
	_, _ = fmt.Fprint(w, modifyOtherKeysEnable)
}

// writeModifyOtherKeysDisable writes the CSI > 4 ; 0 m disable sequence
// unless we're inside the sprout webui terminal. Mirrors
// writeModifyOtherKeysEnable: if we never enabled the protocol, we must
// not emit a disable that xterm.js would either ignore or render as
// garbage on the way out.
func writeModifyOtherKeysDisable(w io.Writer) {
	if webTerminalEnabled() {
		return
	}
	_, _ = fmt.Fprint(w, modifyOtherKeysDisable)
}

// NewInputReader creates a new input reader
func NewInputReader(prompt string) *InputReader {
	ir := &InputReader{
		prompt:          prompt,
		termFd:          int(os.Stdin.Fd()),
		history:         make([]string, 0, 100),
		historyIndex:    -1,
		collapsedPastes: make([]pasteSpan, 0, 8),
		contextMenu:     NewContextMenu(),
		autocomplete:    newInlineAutocomplete(),
	}
	ir.updateTerminalWidth()
	return ir
}
