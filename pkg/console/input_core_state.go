// Package console: InputReader state setters, read error handling, suspend, and search mode (split from input_core.go)

package console

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

// SetPrompt updates the input reader's prompt prefix. Call this between
// ReadLine invocations (not during) to reflect state changes such as the
// active model name after a `/model` switch. SP-048-5d follow-up.
func (ir *InputReader) SetPrompt(p string) {
	ir.prompt = p
}

// SetInitialContent pre-fills the input buffer with text that should
// appear as if the user typed it. Used by the REPL loop to carry over
// unsent steer-panel text into the main prompt after a turn ends.
// The content is consumed on the next ReadLine call and then cleared.
func (ir *InputReader) SetInitialContent(content string) {
	ir.initialContent = content
}

// SetGroundTruth installs the terminal's pristine cooked-mode termios
// snapshot for pre-flight sanity checks. Call once at REPL startup,
// before the first ReadLine.
func (ir *InputReader) SetGroundTruth(gt *GroundTruthTermios) {
	ir.groundTruth = gt
}

// SetFooterTooltip installs the tooltip controller invoked by Alt+T.
// Pass nil to disable the keybinding entirely. The default
// controller writes to os.Stderr.
func (ir *InputReader) SetFooterTooltip(t *FooterTooltip) {
	ir.footerTooltip = t
}

// tooltipVisible returns true if the footer tooltip is currently rendered.
func (ir *InputReader) tooltipVisible() bool {
	if ir.footerTooltip == nil {
		return false
	}
	return ir.footerTooltip.Visible()
}

// hideTooltip dismisses the footer tooltip if visible. Safe to call
// when the tooltip is nil or already hidden.
func (ir *InputReader) hideTooltip() {
	if ir.footerTooltip == nil {
		return
	}
	ir.footerTooltip.Hide()
}

// footerSize returns the terminal size for the tooltip positioning.
// Defaults to 80x24 if the footer is not attached to a TTY. Reads via
// the global status footer when available, else falls back to a
// conservative default.
func (ir *InputReader) footerSize() (cols, rows int) {
	if f := GetGlobalStatusFooter(); f != nil {
		c, r := f.TerminalSize()
		if c > 0 && r > 0 {
			return c, r
		}
	}
	// Fallback: try /dev/tty directly.
	if w, err := openDevTtyForSize(); err == nil {
		c, r := readTermSize(w)
		closeDevTty(w)
		if c > 0 && r > 0 {
			return c, r
		}
	}
	return 80, 24
}

// handleReadError classifies a stdin read error and decides whether the
// read loop should continue (EAGAIN/EINTR) or abort. Returns (continueLoop bool, err error).
// When continueLoop is true, err is nil and the caller should `continue` the read loop.
// When err is non-nil, the caller should return it.
func (ir *InputReader) handleReadError(err error, nonBlocking bool, resizeCh chan os.Signal, parser *EscapeParser) (bool, error) {
	errStr := err.Error()
	// Check if it's just "no data available" (EAGAIN/EWOULDBLOCK)
	// Common error messages: "no data", "resource temporarily unavailable", "EAGAIN"
	isNoData := strings.Contains(errStr, "no data") ||
		strings.Contains(errStr, "temporarily unavailable") ||
		errStr == "EAGAIN" ||
		errStr == "EWOULDBLOCK"
	isInterrupted := strings.Contains(errStr, "interrupted system call") ||
		errStr == "EINTR"

	if nonBlocking && isNoData {
		if ir.processPendingResize(resizeCh, parser) {
			return true, nil
		}
		time.Sleep(pastePollInterval)
		return true, nil
	}
	if isInterrupted && ir.processPendingResize(resizeCh, parser) {
		return true, nil
	}
	// Real error — return it wrapped with context.
	// When stdin EOF arrives in the raw read loop (not the Ctrl-D path
	// at line ~381), log a diagnostic to help distinguish an attached-TTY
	// EOF (unexpected but possible) from a TTY that was revoked underneath
	// the process (pane closed, SSH timeout, parent exited).
	if errors.Is(err, io.EOF) {
		if term.IsTerminal(ir.termFd) {
			fmt.Fprintf(os.Stderr, "[console] stdin EOF received on attached terminal (fd=%d); exiting REPL\n", ir.termFd)
		} else {
			fmt.Fprintf(os.Stderr, "[console] stdin EOF: terminal no longer attached (fd=%d); this typically means the controlling TTY was closed (terminal pane closed, SSH timeout, parent process exited). Exiting REPL.\n", ir.termFd)
		}
	}
	return false, fmt.Errorf("stdin read error: %w", err)
}

// handleSuspend processes Ctrl-Z: suspends the process (SIGTSTP), waits for
// resume (SIGCONT), drains stale input, re-enters raw mode, and restores the
// prompt. Returns the new raw-mode *term.State (or nil if re-entering failed).
func (ir *InputReader) handleSuspend(oldState *term.State, nonBlocking bool) *term.State {
	// Re-enter cooked mode before suspension so the shell
	// state is clean while the user is away.
	term.Restore(ir.termFd, oldState)
	suspendTerminal()

	// Execution resumes here after SIGCONT (e.g. "fg").
	ignoreTerminalSignals()

	// Drain any bytes that arrived while suspended (e.g.
	// keystrokes, newline from "fg" command).  Only possible
	// when the fd is in non-blocking mode; otherwise there is
	// no safe way to poll without blocking indefinitely.
	if nonBlocking {
		time.Sleep(suspendDrainDelay)
		discardBuf := make([]byte, 256)
		for {
			n, _ := os.Stdin.Read(discardBuf)
			if n <= 0 {
				break
			}
		}
	}

	// Re-enter raw mode.
	newState, err := term.MakeRaw(ir.termFd)
	if err != nil {
		return nil
	}

	// Re-enable bracketed paste mode (lost when we exited raw mode).
	fmt.Print(bracketedPasteEnable)

	resetTerminalSignals()

	// Redisplay the prompt and the preserved line content.
	// Unlike a fresh prompt, we keep whatever the user had
	// typed before suspending (mirrors runExternalEditor).
	fmt.Printf("\r%s%s", ClearLineSeq(), ir.prompt)
	ir.Refresh()
	return newState
}

// searchByteResult communicates what the ReadLine loop should do after
// handleSearchModeByte processes a byte.
type searchByteResult int

const (
	searchContinue searchByteResult = iota // continue the read loop
	searchReturn                           // return (input, nil) — accepted match
)

// handleSearchModeByte dispatches a byte while in Ctrl-R reverse-search mode.
// The `input` out-param is set only when result is searchReturn.
//
// Note: Ctrl-C (byte 3) never reaches here — it is intercepted earlier in
// the ReadLine loop and aborts the whole read. We therefore do not handle
// it here; doing so would be dead code.
func (ir *InputReader) handleSearchModeByte(b byte) (result searchByteResult, input string) {
	if b == 13 || b == 10 { // Enter — accept match
		ir.exitSearchMode(true)
		ir.Refresh()
		fmt.Println()
		input := ir.line
		if input != "" {
			ir.AddToHistory(input)
		}
		return searchReturn, input
	} else if b == 27 { // Escape — cancel
		ir.exitSearchMode(false)
		ir.Refresh()
		return searchContinue, ""
	} else {
		ir.handleSearchByte(b)
		ir.renderSearchPrompt()
		return searchContinue, ""
	}
}
