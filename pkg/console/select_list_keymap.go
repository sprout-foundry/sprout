// Key and mouse event handling
package console

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// processKey handles a single keypress (or escape-prefixed sequence)
// and returns (done, val, ok). done=true means the caller should stop
// (confirm, cancel, or dismiss).  n is the number of bytes read; buf
// holds them.  Called from runTTY.
func (s *SelectList) processKey(b byte, n int, buf []byte) (done bool, val string, ok bool) {
	switch {
	case b == 0x03: // Ctrl+C
		return true, "", false
	case b == 0x0D, b == 0x0A: // Enter (CR or LF)
		// Handle Enter key - confirm the selection
		// Track whether we've processed an Enter to avoid re-processing
		// multi-byte sequences like \r\n which can cause the picker to
		// hang or behave unexpectedly
		if s.lastEnterProcessed {
			// Already processed an Enter - this is likely the second byte
			// of a \r\n sequence. Skip it to avoid re-confirming.
			return false, "", false
		}
		s.lastEnterProcessed = true
		val, ok := s.confirm()
		// Reset the flag after processing
		s.lastEnterProcessed = false
		return true, val, ok
	case b == 0x1B: // Esc or arrow-key prefix
		return s.handleEscape(n, buf[:])
	case b == 0x7F, b == 0x08: // Backspace / DEL
		if s.opts.Searchable {
			s.filterBackspace()
			s.render()
		} else if s.opts.DismissOnAnyKey {
			s.recordDismissKey(string(b))
			return true, "", false
		}
		return false, "", false
	case b >= 0x20 && b < 0x7F: // printable ASCII
		if s.opts.Searchable {
			s.filterAppend(string(b))
			s.render()
		} else if s.opts.DismissOnAnyKey {
			s.recordDismissKey(string(b))
			return true, "", false
		}
		return false, "", false
	case b >= 0xC0: // UTF-8 lead byte
		if s.opts.Searchable {
			s.consumeUTF8(b, n, buf[:])
			s.render()
		} else if s.opts.DismissOnAnyKey {
			s.recordDismissKey(utf8RuneFromBuf(b, n, buf[:]))
			return true, "", false
		}
		return false, "", false
	}
	return false, "", false
}

// recordDismissKey stores the printable text of the key that dismissed
// the picker so callers can forward it (e.g. into the REPL input buffer).
// Backspace/DEL (0x7F/0x08) is intentionally NOT recorded — it's not a
// character the user would want pre-filled into a prompt.
func (s *SelectList) recordDismissKey(text string) {
	if b := text[0]; b == 0x7F || b == 0x08 {
		return
	}
	s.dismissKey = text
}

// handleEscape dispatches the bytes that follow ESC. Returns done=true
// (with val/ok) when the user wants to cancel; done=false when the
// sequence was just an arrow key or other navigation that mutated
// cursor/filter state.
func (s *SelectList) handleEscape(n int, buf []byte) (done bool, val string, ok bool) {
	if n == 1 {
		// Could be a plain ESC or the start of a CSI sequence. Read
		// one more byte non-blockingly via a short poll; if nothing
		// arrives, treat as cancel.
		var follow [1]byte
		deadline := time.Now().Add(20 * time.Millisecond)
		for time.Now().Before(deadline) {
			m, _ := os.Stdin.Read(follow[:])
			if m == 1 {
				if follow[0] != '[' && follow[0] != 'O' {
					// Not a CSI sequence — treat ESC as cancel.
					return true, "", false
				}
				// Check if the next byte after '[' is '<' (SGR mouse)
				// or something else (CSI arrow key).
				var next [1]byte
				for time.Now().Before(deadline) {
					k, _ := os.Stdin.Read(next[:])
					if k == 1 {
						if next[0] == '<' {
							// SGR mouse event — consume until M/m.
							s.consumeSGRMouse()
							s.render()
							return false, "", false
						}
						// Regular CSI — dispatch the final byte if it's
						// in the valid range, otherwise keep reading.
						if next[0] >= 0x40 && next[0] <= 0x7E {
							s.dispatchCSI(next[0])
							s.render()
							return false, "", false
						}
						// Parameter/intermediate byte — fall through to
						// consumeCSI to drain the rest.
						return false, "", s.consumeCSI()
					}
					time.Sleep(1 * time.Millisecond)
				}
				// Timed out reading third byte — treat as CSI with no
				// final byte (ignore).
				return false, "", false
			}
			time.Sleep(2 * time.Millisecond)
		}
		// No follow-up byte → plain ESC means cancel.
		return true, "", false
	}
	// We got the whole sequence in one read.
	if n >= 3 && buf[1] == '[' {
		if buf[2] == '<' {
			// SGR mouse event — the full sequence may span multiple
			// reads; consume what we have and drain the rest.
			s.consumeSGRMouseFromBuf(buf[2:])
			s.render()
			return false, "", false
		}
		s.dispatchCSI(buf[2])
		s.render()
		return false, "", false
	}
	return true, "", false
}

// consumeCSI reads bytes from stdin until it finds the CSI final byte
// (0x40..0x7E), then dispatches based on it. Always returns false (no
// confirm/cancel — just navigation).
func (s *SelectList) consumeCSI() bool {
	var ch [1]byte
	deadline := time.Now().Add(50 * time.Millisecond)
	for time.Now().Before(deadline) {
		n, _ := os.Stdin.Read(ch[:])
		if n == 0 {
			time.Sleep(1 * time.Millisecond)
			continue
		}
		b := ch[0]
		if b >= 0x40 && b <= 0x7E {
			s.dispatchCSI(b)
			s.render()
			return false
		}
		// Parameter byte (0x30..0x3F) or intermediate (0x20..0x2F) —
		// keep reading.
	}
	return false
}

// consumeSGRMouse reads bytes from stdin until it finds the SGR mouse
// terminator ('M' for press/motion or 'm' for release), then parses
// and dispatches the event.
//
// SGR format: ESC [ < button;col;row M
// We've already consumed "ESC [<" by the time this is called.
func (s *SelectList) consumeSGRMouse() {
	var buf strings.Builder
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		var ch [1]byte
		n, _ := os.Stdin.Read(ch[:])
		if n == 0 {
			time.Sleep(1 * time.Millisecond)
			continue
		}
		b := ch[0]
		buf.WriteByte(b)
		if b == 'M' || b == 'm' {
			// Normalize lowercase 'm' (release) to 'M' for parsing.
			seq := buf.String()
			seq = strings.TrimSuffix(seq, "m") + "M"
			s.dispatchMouseEvent(seq)
			return
		}
	}
}

// consumeSGRMouseFromBuf is like consumeSGRMouse but starts with
// partial bytes already in buf (the bytes after "ESC [<").
func (s *SelectList) consumeSGRMouseFromBuf(buf []byte) {
	var b strings.Builder
	b.Write(buf)
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		var ch [1]byte
		n, _ := os.Stdin.Read(ch[:])
		if n == 0 {
			time.Sleep(1 * time.Millisecond)
			continue
		}
		byteVal := ch[0]
		b.WriteByte(byteVal)
		if byteVal == 'M' || byteVal == 'm' {
			seq := b.String()
			seq = strings.TrimSuffix(seq, "m") + "M"
			s.dispatchMouseEvent(seq)
			return
		}
	}
}

// dispatchMouseEvent parses the SGR mouse payload (the part after
// "ESC [<", e.g., "0;10;5M") and dispatches wheel events to scroll
// the list. Non-wheel events are ignored (tap-to-select is out of
// scope per SP-106).
//
// SGR button encoding: button_number | (modifiers << 2) | motion(32) | release(128)
// Extract base button with: cb & 0x63 (strip modifier, motion, release bits).
func (s *SelectList) dispatchMouseEvent(payload string) {
	// Remove the trailing 'M' to get "button;col;row".
	payload = strings.TrimSuffix(payload, "M")
	parts := strings.Split(payload, ";")
	if len(parts) != 3 {
		return
	}
	cb, err := strconv.Atoi(parts[0])
	if err != nil {
		return
	}
	// Extract base button number: strip modifiers (bits 2-4), motion (bit 5),
	// release (bit 7).  0x63 = 0b01100011 preserves bits 0,1,5,6 which is
	// the button number for regular (0-3) and wheel (64-67) buttons.
	button := cb & 0x63
	switch button {
	case 64:
		s.dispatchMouseWheel(MouseEventWheelUp)
	case 65:
		s.dispatchMouseWheel(MouseEventWheelDown)
	case 66:
		s.dispatchMouseWheel(MouseEventWheelLeft)
	case 67:
		s.dispatchMouseWheel(MouseEventWheelRight)
	}
}

// dispatchMouseWheel handles a mouse wheel event by moving the cursor
// and re-rendering. WheelUp/Down scroll the list; WheelLeft/Right are
// no-ops (no horizontal scrolling).
func (s *SelectList) dispatchMouseWheel(kind MouseEventKind) {
	switch kind {
	case MouseEventWheelUp:
		s.mu.Lock()
		s.moveCursor(-1)
		s.mu.Unlock()
	case MouseEventWheelDown:
		s.mu.Lock()
		s.moveCursor(1)
		s.mu.Unlock()
	}
}

// dispatchCSI maps a final CSI byte onto a navigation action.
func (s *SelectList) dispatchCSI(final byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch final {
	case 'A': // Up
		s.moveCursor(-1)
	case 'B': // Down
		s.moveCursor(1)
	case 'H': // Home
		s.cursor = 0
		s.adjustOffset()
	case 'F': // End
		s.cursor = len(s.filtered) - 1
		if s.cursor < 0 {
			s.cursor = 0
		}
		s.adjustOffset()
	case '5': // PgUp prefix (CSI 5~) — but we already ate the final char
	case '6': // PgDn prefix
	}
}

// moveCursor changes the cursor position with bounds clamping and
// updates the scroll offset so the cursor stays visible. Must be
// called with s.mu held.
func (s *SelectList) moveCursor(delta int) {
	if len(s.filtered) == 0 {
		s.cursor = 0
		s.offset = 0
		return
	}
	s.cursor += delta
	if s.cursor < 0 {
		s.cursor = 0
	}
	if s.cursor >= len(s.filtered) {
		s.cursor = len(s.filtered) - 1
	}
	s.adjustOffset()
}

// adjustOffset moves the page-top so cursor is visible. Must be called
// with s.mu held.
func (s *SelectList) adjustOffset() {
	if s.cursor < s.offset {
		s.offset = s.cursor
		return
	}
	if s.cursor >= s.offset+s.opts.PageSize {
		s.offset = s.cursor - s.opts.PageSize + 1
	}
	if s.offset < 0 {
		s.offset = 0
	}
}

// confirm returns the value of the currently-selected filtered item.
// Returns ok=false if the filter excludes every item.
func (s *SelectList) confirm() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.filtered) == 0 || s.cursor < 0 || s.cursor >= len(s.filtered) {
		return "", false
	}
	return s.opts.Items[s.filtered[s.cursor]].Value, true
}

// DismissKey returns the printable key that dismissed the picker under
// DismissOnAnyKey (empty for Enter/Esc/Ctrl+C exits or when the
// feature is off). Callers can forward it into their own input reader
// so the user's keystroke isn't lost.
func (s *SelectList) DismissKey() string {
	return s.dismissKey
}
