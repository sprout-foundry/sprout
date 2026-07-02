package console

import (
	"os"
	"os/signal"
	"time"
	"unicode/utf8"
)

// readLoop is the input-handling goroutine. Steer mode sets VMIN=0
// and VTIME=0 on the termios (see steer_termios_*.go) so Read returns
// immediately with 0 bytes when nothing is ready — no need for an
// O_NONBLOCK file descriptor flag. The poll interval (10ms) is short
// enough that typing feels instantaneous and Stop() observes the exit
// signal within one frame.
//
// Input is read in multi-byte chunks (up to 64 bytes per Read) and fed
// through the shared EscapeParser — the same parser InputReader uses —
// instead of hand-rolling escape-sequence detection and polling loops
// here. Emacs-style control keys are dispatched directly before the
// parser so they aren't shadowed by the parser's own handling.
func (r *SteerInputReader) readLoop(stopCh, doneCh chan struct{}) {
	defer close(doneCh)

	parser := NewEscapeParser()
	buf := make([]byte, 64)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	// Subscribe to terminal resize events so the steer panel
	// re-renders with correct dimensions after a SIGWINCH. The footer's
	// own resize watcher handles the scroll region; we just need to
	// refresh our pinned line content.
	var resizeCh chan os.Signal
	if sig := resizeSignal(); sig != nil {
		resizeCh = make(chan os.Signal, 1)
		signal.Notify(resizeCh, sig)
		defer signal.Stop(resizeCh)
	}

	// pasteMatch tracks how many bytes of the bracketed-paste end
	// sequence (ESC [ 201~) have been seen consecutively while a
	// paste is in flight. A local rather than a struct field because
	// readLoop is the sole consumer and bracketed-paste handling is
	// strictly sequential (single goroutine, no overlap).
	pasteMatch := 0

	for {
		select {
		case <-stopCh:
			return
		default:
		}

		n, err := os.Stdin.Read(buf)
		if n == 0 {
			// No byte ready (or EOF). Sleep briefly via the ticker
			// instead of busy-spinning, then re-check stopCh.
			if err != nil && !isEAGAIN(err) {
				// Real error (stdin closed, etc.) — exit.
				return
			}
			select {
			case <-stopCh:
				return
			case <-resizeCh:
				// Terminal resized — re-render the steer line so the
				// caret and content adapt to the new width.
				r.renderLine()
			case <-ticker.C:
			}
			continue
		}

		for i := 0; i < n; i++ {
			b := buf[i]

			// While a bracketed paste is in flight, ALL bytes go into
			// pasteBuf verbatim — including newlines and what would
			// otherwise be control characters. The only escape is the
			// "ESC [ 201~" terminator, which we detect inline by
			// matching the byte stream against bracketedPasteEndSeq.
			r.mu.Lock()
			inPaste := r.pasteActive
			r.mu.Unlock()
			if inPaste {
				if b == bracketedPasteEndSeq[pasteMatch] {
					pasteMatch++
					if pasteMatch == len(bracketedPasteEndSeq) {
						pasteMatch = 0
						r.endPaste()
					}
					continue
				}
				// Mismatch: flush any partially-matched prefix as
				// literal bytes, then handle the current byte.
				if pasteMatch > 0 {
					for _, pb := range []byte(bracketedPasteEndSeq[:pasteMatch]) {
						r.appendPasteByte(pb)
					}
					pasteMatch = 0
				}
				r.appendPasteByte(b)
				continue
			}

			// Ctrl-X Ctrl-E editor escape (SP-048-4f parity). The first
			// Ctrl-X sets pendingCtrlX; if the next byte is Ctrl-E we
			// launch the external editor, otherwise we fall through to
			// normal processing.
			if r.pendingCtrlX {
				r.pendingCtrlX = false
				if b == 0x05 { // Ctrl-E
					r.runExternalEditor()
					continue
				}
				// Not Ctrl-E — fall through to normal processing.
			}

			// While in Ctrl-R reverse-search mode, route keystrokes to
			// the search handler instead of the normal buffer/edit
			// dispatch. Enter accepts the match, Esc cancels, Ctrl-R
			// cycles to the next older match, Backspace trims the
			// query, and printable/UTF-8 bytes extend the query.
			if r.searchMode {
				switch {
				case b == 0x0D: // Enter — accept match
					r.exitSearchMode(true)
					r.renderLine()
					continue
				case b == 0x1B: // Esc — cancel
					r.exitSearchMode(false)
					r.renderLine()
					continue
				case b == 0x7F || b == 0x08: // Backspace
					r.handleSearchBackspace()
					r.renderLine()
					continue
				case b >= 0x20 && b < 0x7F: // Printable ASCII
					r.searchQuery += string(rune(b))
					r.refreshSearchForQuery()
					r.renderLine()
					continue
				case b >= 0x80: // UTF-8 — buffer until full rune
					r.searchBuf = append(r.searchBuf, b)
					if utf8.FullRune(r.searchBuf) {
						r.searchQuery += string(r.searchBuf)
						r.searchBuf = r.searchBuf[:0]
						r.refreshSearchForQuery()
						r.renderLine()
					}
					continue
				}
				// Other control chars in search mode: ignore.
				continue
			}

			// Pre-handle control characters that the EscapeParser
			// doesn't produce events for (emacs/readline-style
			// editing). The parser handles backspace (8/127), enter
			// (13), bare newline (10), tab (9), escape sequences (27),
			// and printable / UTF-8 characters.
			switch b {
			case 0x01: // Ctrl+A — move to start
				r.moveCursorStart()
				continue
			case 0x02: // Ctrl+B — move back one rune
				r.moveCursorBackward()
				continue
			case 0x03: // Ctrl+C — interrupt
				r.handleInterrupt()
				continue
			case 0x04: // Ctrl+D — forward-delete rune at cursor
				r.deleteForward()
				continue
			case 0x05: // Ctrl+E — move to end
				r.moveCursorEnd()
				continue
			case 0x06: // Ctrl+F — move forward one rune
				r.moveCursorForward()
				continue
			case 0x0B: // Ctrl+K — kill to end
				r.killToEnd()
				continue
			case 0x12: // Ctrl+R — reverse search
				if r.searchMode {
					r.cycleSearchResult()
				} else {
					r.enterSearchMode()
				}
				r.renderLine()
				continue
			case 0x15: // Ctrl+U — kill to start
				r.killToStart()
				continue
			case 0x17: // Ctrl+W — delete previous word
				r.deleteWordBackward()
				continue
			case 0x18: // Ctrl+X — start of Ctrl-X Ctrl-E sequence
				r.pendingCtrlX = true
				continue
			case 0x1d: // Ctrl+] — completion cycle (SP-078 Phase 2)
				r.handleSteerCompletion()
				continue
			}

			// Feed everything else through the shared EscapeParser.
			event := parser.Parse(b)
			if event == nil {
				continue
			}
			r.handleEvent(event)
			// Drain any pending events the parser queued (e.g. a
			// printable byte carried over after an escape sequence).
			for parser.hasPending {
				pending := parser.Parse(0)
				if pending == nil {
					break
				}
				r.handleEvent(pending)
			}
		}
	}
}
