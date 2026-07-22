// Package console: main ReadLine loop and non-terminal fallback (split from input_core.go)

package console

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"golang.org/x/term"
)

// ReadLine reads a line of input with proper escape sequence handling
func (ir *InputReader) ReadLine() (string, error) {
	// Check if we're in a terminal
	if !term.IsTerminal(ir.termFd) {
		return ir.fallbackReadLine()
	}

	// Pre-flight: unconditionally force a known-good cooked termios
	// before MakeRaw saves the to-be-restored state. ICANON-on alone
	// (what EnsureSane checks) doesn't catch every way input can be
	// broken across turns — VMIN=0 leftover from steer mode, IXOFF
	// stop bit, missing OPOST — and any one of those silently latches
	// for the rest of the session because MakeRaw → defer Restore
	// faithfully restores whatever-broken-state was current at entry.
	// Defensive: also clear any leaked O_NONBLOCK from a prior session.
	if ir.groundTruth != nil {
		ir.groundTruth.EnsureCooked()
	}
	_ = setNonblock(ir.termFd, false)

	// Save terminal state and set raw mode. setupInputTerm (called
	// further down, after the line-state reset and prompt render)
	// installs the bracketed paste + SGR mouse + modifyOtherKeys modes,
	// registers this reader as the active one, sets up the SIGWINCH
	// channel, and flips the fd into non-blocking for paste detection.
	// teardownInputTerm (deferred inside setupInputTerm's call site)
	// undoes those modes in LIFO order: clear the reader slot, disable
	// the SGR sequences, then flip non-blocking off. The final cooked
	// termios restore is this outer `defer term.Restore` which runs last.
	oldState, err := term.MakeRaw(ir.termFd)
	if err != nil {
		return ir.fallbackReadLine()
	}
	defer term.Restore(ir.termFd, oldState)

	// Initialize line state
	ir.line = ""
	ir.cursorPos = 0
	ir.historyIndex = -1
	ir.hasEditedLine = false
	ir.updateTerminalWidth()
	ir.lastLineLength = 0
	ir.lastVisualRows = 1
	ir.lastWrapPending = false
	ir.currentPhysicalLine = 0
	ir.pasteBuffer.Reset()
	ir.pasteActive = false
	ir.bracketedPaste = false
	ir.bracketedMatch = 0
	ir.bracketedSawCR = false
	ir.collapsedPastes = ir.collapsedPastes[:0]
	ir.rawPasteBuffer = nil
	ir.lastCharTime = time.Now()
	ir.resetCompletionCycle()
	// SP-048-4e: reset search state
	ir.searchMode = false
	ir.searchQuery = ""
	ir.searchResult = ""
	ir.searchResultIndex = -1
	ir.preSearchLine = ""
	ir.preSearchCursorPos = 0
	// SP-048 follow-up: defensively clear the current line before printing
	// the prompt. Otherwise, if the cursor was left mid-line by prior
	// output (e.g. partial content under the status footer's scroll region
	// after a redraw), the prompt would render *on top of* that content
	// and produce the prompt-overlap-on-startup bug we caught in real use.
	//
	// The prompt draw is wrapped in LockOutput so a background footer
	// Refresh (SIGWINCH, event subscriber) can't emit DECSC/DECRC
	// between the bare Printf and the read loop entry. Without the
	// lock, the footer's DECRC could restore the cursor to a pre-prompt
	// position (column 0), making the first typed character appear at
	// the start of the row instead of after the prompt prefix.
	LockOutput()
	fmt.Printf("\r\033[K%s", ir.prompt)
	UnlockOutput()

	// SP-055 follow-up: if the REPL carried over unsent steer text,
	// pre-fill it into the line buffer and render it so the user can
	// pick up where they left off. Consumed once then cleared.
	if ir.initialContent != "" {
		ir.line = ir.initialContent
		ir.cursorPos = len(ir.initialContent)
		ir.initialContent = ""
		ir.hasEditedLine = true
		ir.Refresh()
	}

	parser := NewEscapeParser()
	buf := make([]byte, 32)
	// setupInputTerm does (in order):
	//   - enable bracketed paste, SGR mouse tracking, modifyOtherKeys
	//   - register this reader as the active one (for PrintExternal)
	//   - create the SIGWINCH channel (nil on platforms without resize)
	//   - flip the fd to non-blocking (false on platforms that reject it)
	// The matching teardownInputTerm (deferred below) undoes the SGR
	// sequences and clears the active reader slot; the final
	// `defer term.Restore` reverts cooked termios.
	resizeCh, nonBlocking := ir.setupInputTerm()
	defer signal.Stop(resizeCh)
	// teardownInputTerm undoes the terminal modes that setupInputTerm
	// enabled (bracketed paste, SGR mouse, modifyOtherKeys, active
	// reader). It must run while the fd is still in raw mode, so it
	// is the first defer installed and therefore the first to fire.
	// The terminal is restored to cooked mode by the outer
	// `defer term.Restore` below.
	defer ir.teardownInputTerm()
	if nonBlocking {
		defer func() {
			_ = setNonblock(ir.termFd, false)
		}()
	}

	for {
		if ir.processPendingResize(resizeCh, parser) {
			continue
		}

		n, err := os.Stdin.Read(buf)

		// Handle non-blocking read errors
		if err != nil {
			continueLoop, err := ir.handleReadError(err, nonBlocking, resizeCh, parser)
			if err != nil {
				return "", err
			}
			if continueLoop {
				continue
			}
		}

		if n == 0 {
			// In non-blocking mode, this means no data
			if nonBlocking {
				time.Sleep(pastePollInterval)
			}
			continue
		}

		// Process each byte through the escape parser
		for i := 0; i < n; i++ {
			b := buf[i]
			now := time.Now()

			if ir.bracketedPaste {
				if ir.consumeBracketedPasteByte(b) {
					ir.finalizePaste()
				}
				ir.lastCharTime = now
				continue
			}

			// Handle Ctrl+C and Ctrl+Z directly before parsing
			if b == 3 { // Ctrl+C
				fmt.Printf("\r%s", ClearToEndOfLineSeq()) // Clear line
				fmt.Println("^C")
				return "", fmt.Errorf("interrupted")
			}

			// SP-048-4f: Ctrl-X Ctrl-E opens $EDITOR with the current buffer
			// pre-filled. Two-keystroke sequence: first byte 24 (Ctrl-X)
			// arms pendingCtrlX; the next byte either triggers the editor
			// escape (Ctrl-E = 5) or aborts the sequence and falls through
			// to normal processing.
			if ir.pendingCtrlX {
				ir.pendingCtrlX = false
				if b == 5 { // Ctrl-E
					if newState := ir.runExternalEditor(oldState, nonBlocking); newState != nil {
						oldState = newState
					}
					ir.Refresh()
					continue
				}
				// Not Ctrl-E — fall through and process `b` normally below.
			}
			if b == 24 { // Ctrl-X (start of Ctrl-X Ctrl-E sequence)
				ir.pendingCtrlX = true
				continue
			}

			// Emacs/readline-style control characters. These match the
			// bindings users expect from bash/zsh/python REPLs. Skipped
			// while in Ctrl-R reverse-search mode — there, only
			// Enter/Esc/Ctrl-C/Ctrl-R/Backspace/printable bytes affect
			// the search query (handled further below). Without this
			// guard, Ctrl-D on an empty prompt during search would
			// terminate the session, and Ctrl-A/E/U/K/W would corrupt
			// the underlying line buffer instead of the query.
			if !ir.searchMode {
				switch b {
				case 1: // Ctrl-A — move to start of line
					ir.SetCursor(0)
					continue
				case 4: // Ctrl-D — EOF on empty line, else forward-delete
					if len(ir.line) == 0 {
						fmt.Println()
						return "", io.EOF
					}
					ir.Delete()
					continue
				case 5: // Ctrl-E — move to end of line (standalone; the
					// two-keystroke Ctrl-X Ctrl-E editor escape is handled
					// above via pendingCtrlX)
					ir.SetCursor(len(ir.line))
					continue
				case 2: // Ctrl-B — move back one char
					ir.MoveCursor(-1)
					continue
				case 6: // Ctrl-F — move forward one char
					ir.MoveCursor(1)
					continue
				case 11: // Ctrl-K — kill to end of line
					ir.KillToEndOfLine()
					continue
				case 21: // Ctrl-U — kill to start of line
					ir.KillToStartOfLine()
					continue
				case 23: // Ctrl-W — delete previous word
					ir.DeleteWordBackward()
					continue
				}
			}

			if b == 26 { // Ctrl+Z
				if newState := ir.handleSuspend(oldState, nonBlocking); newState != nil {
					oldState = newState
				}
				continue
			}

			// SP-048-4e: Ctrl-R reverse search
			if b == 0x12 { // Ctrl-R
				if !ir.searchMode {
					ir.enterSearchMode()
				} else {
					ir.cycleSearchResult()
				}
				ir.renderSearchPrompt()
				continue
			}

			// SP-048-4e: Search mode byte handler
			if ir.searchMode {
				result, input := ir.handleSearchModeByte(b)
				switch result {
				case searchContinue:
					continue
				case searchReturn:
					return input, nil
				}
			}

			ir.lastCharTime = now

			// Parse the byte through the escape parser
			event := parser.Parse(b)
			if event != nil {
				if event.Type == EventPasteStart {
					ir.bracketedPaste = true
					ir.bracketedMatch = 0
					ir.bracketedSawCR = false
					ir.pasteActive = true
					ir.pasteBuffer.Reset()
					ir.rawPasteBuffer = nil
					continue
				}
				if event.Type == EventPasteEnd {
					ir.bracketedPaste = false
					ir.bracketedMatch = 0
					ir.bracketedSawCR = false
					ir.finalizePaste()
					continue
				}
				if event.Type == EventMouse {
					// Handle mouse event
					ir.handleMouseEvent(event.Data)
					continue
				}
				if event.Type == EventEnter {
					// If the autocomplete dropdown is visible, accept the
					// selected candidate before submitting the line.
					if ir.autocomplete != nil && ir.autocomplete.visible {
						text := ir.autocomplete.accept()
						if text != "" {
							ir.line = text
							ir.cursorPos = len(ir.line)
						}
						// hide() marks the dropdown invisible; the next
						// Refresh() → refreshLocked() → clear() erases
						// the old rows, then redraws the input line with
						// the accepted text so the user sees the final
						// command (e.g. "/help") instead of the partial
						// text they typed ("/he").
						ir.autocomplete.hide()
						ir.Refresh()
					}
					// End of input
					fmt.Println() // Move to next line
					input := ir.line
					if input != "" {
						ir.AddToHistory(input)
					}
					return input, nil
				}
				ir.HandleEvent(event)
			}
			for parser.hasPending {
				pending := parser.Parse(0)
				if pending == nil {
					break
				}
				ir.HandleEvent(pending)
			}
		}
	}
}

// fallbackReadLine provides simple input for non-terminal environments
// (piped stdin). Uses bufio.Reader so multi-word input isn't truncated at
// whitespace (the old fmt.Scanln did that) and returns the error directly
// so EOF on a closed pipe is reported faithfully rather than always wrapped
// as "failed to read fallback input: EOF" (the old fmt.Errorf("%w", err)
// produced a non-nil error even on success).
func (ir *InputReader) fallbackReadLine() (string, error) {
	fmt.Print(ir.prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
