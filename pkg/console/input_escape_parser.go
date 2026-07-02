package console

import "strings"

// EscapeParser handles escape sequences using a simple state machine
type EscapeParser struct {
	state       int
	buffer      []byte
	pendingChar byte   // Stores a character that should be processed next
	hasPending  bool   // Whether there's a pending character
	mouseBuf    []byte // Buffer for mouse event data

	// utf8Buf accumulates bytes of a multi-byte UTF-8 sequence. When
	// a leading byte (>= 0xC0) arrives outside an escape sequence, we
	// buffer it here along with the expected number of continuation
	// bytes. Without this, continuation bytes (0x80-0xBF) are silently
	// dropped by the default case, corrupting pasted text containing
	// non-ASCII characters (curly quotes, emoji, international text).
	utf8Buf  []byte
	utf8Need int // remaining continuation bytes expected
}

// NewEscapeParser creates a new escape sequence parser
func NewEscapeParser() *EscapeParser {
	return &EscapeParser{
		state:  0,
		buffer: make([]byte, 0, 10),
	}
}

// Parse processes a byte and returns an event if complete
func (ep *EscapeParser) Parse(b byte) *InputEvent {
	// If we have a pending character, return it first
	if ep.hasPending {
		ep.hasPending = false
		return &InputEvent{Type: EventChar, Data: string([]byte{ep.pendingChar})}
	}

	switch ep.state {
	case 0: // Waiting for ESC or regular char
		if b == 27 {
			ep.state = 1
			return nil
		}
		// Handle control characters
		switch b {
		case 8, 127:
			return &InputEvent{Type: EventBackspace}
		case 13:
			return &InputEvent{Type: EventEnter}
		case 10:
			// Bare LF — terminals that distinguish Shift+Enter from plain
			// Enter (notably VS Code's integrated terminal) send 0x0A
			// here while plain Enter stays as CR (0x0D). Insert a literal
			// newline so multi-line composition just works.
			return &InputEvent{Type: EventChar, Data: "\n"}
		case 9:
			return &InputEvent{Type: EventTab}
		default:
			// Return regular printable characters as character events.
			// Handle multi-byte UTF-8 sequences (pasted international
			// text, emoji, curly quotes) by buffering continuation bytes.
			// Without this, bytes >= 0x80 are silently dropped,
			// corrupting pasted JSON content that contains non-ASCII
			// characters.
			if ep.utf8Need > 0 {
				// We're mid-sequence: accumulate continuation bytes.
				if b&0xC0 == 0x80 {
					ep.utf8Buf = append(ep.utf8Buf, b)
					ep.utf8Need--
					if ep.utf8Need == 0 {
						runeBytes := ep.utf8Buf
						ep.utf8Buf = nil
						return &InputEvent{Type: EventChar, Data: string(runeBytes)}
					}
					return nil
				}
				// Invalid continuation — the sequence was truncated.
				// Flush what we have (best effort) and process the new
				// byte as a potential new leading byte.
				ep.utf8Buf = nil
				ep.utf8Need = 0
			}
			// Check for UTF-8 leading byte (2-4 byte sequence).
			if b >= 0xC0 {
				ep.utf8Buf = append(ep.utf8Buf, b)
				switch {
				case b&0xE0 == 0xC0:
					ep.utf8Need = 1 // 2-byte sequence
				case b&0xF0 == 0xE0:
					ep.utf8Need = 2 // 3-byte sequence
				case b&0xF8 == 0xF0:
					ep.utf8Need = 3 // 4-byte sequence
				default:
					// Invalid leading byte — emit as-is (best effort).
					ep.utf8Buf = nil
					return &InputEvent{Type: EventChar, Data: string([]byte{b})}
				}
				return nil // wait for continuation bytes
			}
			if b >= 32 && b <= 126 {
				return &InputEvent{Type: EventChar, Data: string([]byte{b})}
			}
			return nil
		}

	case 1: // Got ESC, expecting '[' or other sequence
		ep.buffer = append(ep.buffer, b)
		if b == '[' {
			ep.state = 2
			return nil
		}
		// Handle other ESC sequences (like ESC O for function keys)
		if b == 'O' {
			ep.state = 4
			return nil
		}
		// Alt+Enter (ESC + CR/LF): insert a literal newline into the
		// buffer instead of submitting. Parity with the steer panel —
		// plain Enter still submits, but Alt+Enter lets the user
		// compose multi-line prompts. Most terminals translate the
		// Alt modifier to a leading ESC byte; iTerm2 needs "Option
		// acts as Meta" enabled for this to fire.
		if b == 13 || b == 10 {
			ep.Reset()
			return &InputEvent{Type: EventChar, Data: "\n"}
		}
		// Alt+B (backward word) and Alt+F (forward word). Most terminals
		// send Alt-modified keys as ESC + <key>, which lands here.
		if b == 'b' {
			ep.Reset()
			return &InputEvent{Type: EventWordLeft}
		}
		if b == 'f' {
			ep.Reset()
			return &InputEvent{Type: EventWordRight}
		}
		// Alt+Backspace (Meta-DEL): ESC followed by 0x7F. Delete the
		// previous word, same as Ctrl-W.
		if b == 127 {
			ep.Reset()
			return &InputEvent{Type: EventDeleteWordBackward}
		}
		// CLI-D: Alt+<letter> for any other letter. We surface the
		// letter in .Data so a keymap registry can route it. The
		// legacy pending-char + EventEscape path would also fire for
		// these, but combining them into a single AltLetter event is
		// cleaner: callers don't have to combine a synthetic Escape +
		// pending-char to recover the key. Suppress the pending char
		// path entirely.
		if b >= 32 && b <= 126 {
			ep.Reset()
			return &InputEvent{Type: EventAltLetter, Data: string([]byte{b})}
		}
		// Not a CSI sequence, treat ESC as escape event
		// This character could be printable, save it for next call
		ep.Reset()
		if b >= 32 && b <= 126 {
			ep.pendingChar = b
			ep.hasPending = true
		}
		return &InputEvent{Type: EventEscape}

	case 2: // Got '[', reading sequence
		ep.buffer = append(ep.buffer, b)

		// Check for completed sequences - only look at the last character for simple cases
		switch b {
		case 'A': // Up arrow
			event := &InputEvent{Type: EventUp}
			ep.Reset()
			return event
		case 'B': // Down arrow
			event := &InputEvent{Type: EventDown}
			ep.Reset()
			return event
		case 'C': // Right arrow (or Ctrl+Right = forward word)
			ctrlMod := strings.Contains(string(ep.buffer), ";5")
			ep.Reset()
			if ctrlMod {
				return &InputEvent{Type: EventWordRight}
			}
			return &InputEvent{Type: EventRight}
		case 'D': // Left arrow (or Ctrl+Left = backward word)
			ctrlMod := strings.Contains(string(ep.buffer), ";5")
			ep.Reset()
			if ctrlMod {
				return &InputEvent{Type: EventWordLeft}
			}
			return &InputEvent{Type: EventLeft}
		case 'H': // Home
			event := &InputEvent{Type: EventHome}
			ep.Reset()
			return event
		case 'F': // End
			event := &InputEvent{Type: EventEnd}
			ep.Reset()
			return event
		case '<': // Mouse event (SGR mode): ESC [ < Cb;Cx;Cy M
			ep.state = 5
			ep.mouseBuf = []byte{27, '[', '<'}
			return nil
		case 'M': // Mouse event (X10 mode): ESC [ M Cb Cx Cy
			ep.state = 6
			ep.mouseBuf = []byte{27, '[', 'M'}
			return nil
		default:
			// Handle numeric CSI params and terminated forms like ESC [ 3 ~ and ESC [ 200 ~.
			if (b >= '0' && b <= '9') || b == ';' {
				return nil
			}
			if b == 'u' {
				// CSI u (fixterms / kitty keyboard / xterm modifyOtherKeys
				// reporting). Format: ESC [ <key>;<modifiers> u — used by
				// Windows Terminal, kitty, alacritty, foot, and any iTerm2/
				// xterm configured with modifyOtherKeys. Shift+Enter
				// arrives here as `13;2u`. Modifier bitmask: 1=none,
				// 2=Shift, 3=Alt, 4=Shift+Alt, 5=Ctrl, etc. Any modifier
				// on Enter is treated as "insert newline" — covers
				// Shift+Enter and Alt+Enter alike.
				param := ""
				if len(ep.buffer) >= 2 {
					param = string(ep.buffer[1 : len(ep.buffer)-1])
				}
				keyParam, modParam := param, ""
				if idx := strings.IndexByte(param, ';'); idx >= 0 {
					keyParam = param[:idx]
					modParam = param[idx+1:]
				}
				ep.Reset()
				if keyParam == "13" && modParam != "" && modParam != "1" {
					return &InputEvent{Type: EventChar, Data: "\n"}
				}
				return &InputEvent{Type: EventEscape}
			}
			if b == '~' {
				param := ""
				if len(ep.buffer) >= 3 {
					param = string(ep.buffer[1 : len(ep.buffer)-1])
				}
				firstParam := param
				if idx := strings.IndexByte(param, ';'); idx >= 0 {
					firstParam = param[:idx]
				}
				ep.Reset()
				switch firstParam {
				case "1", "7":
					return &InputEvent{Type: EventHome}
				case "4", "8":
					return &InputEvent{Type: EventEnd}
				case "3":
					return &InputEvent{Type: EventDelete}
				case "200":
					return &InputEvent{Type: EventPasteStart}
				case "201":
					return &InputEvent{Type: EventPasteEnd}
				case "27":
					// Older xterm "modifyOtherKeys" format:
					// ESC [ 27 ; <mods> ; <key> ~ . If key is 13 (Enter)
					// with any modifier, insert a newline.
					parts := strings.Split(param, ";")
					if len(parts) == 3 && parts[2] == "13" && parts[1] != "" && parts[1] != "1" {
						return &InputEvent{Type: EventChar, Data: "\n"}
					}
					return &InputEvent{Type: EventEscape}
				default:
					return &InputEvent{Type: EventEscape}
				}
			}
			// Unknown sequence - treat as standalone ESC
			// This character could be printable, save it for next call
			ep.Reset()
			if b >= 32 && b <= 126 {
				ep.pendingChar = b
				ep.hasPending = true
			}
			return &InputEvent{Type: EventEscape}
		}

	case 3: // Expecting "~" for Delete
		ep.buffer = append(ep.buffer, b)
		if b == '~' {
			event := &InputEvent{Type: EventDelete}
			ep.Reset()
			return event
		}
		// Not Delete, the 'b' could be a printable character
		ep.Reset()
		if b >= 32 && b <= 126 {
			ep.pendingChar = b
			ep.hasPending = true
		}
		return &InputEvent{Type: EventEscape}

	case 4: // ESC O sequences (function keys)
		ep.buffer = append(ep.buffer, b)
		switch b {
		case 'H': // Home
			event := &InputEvent{Type: EventHome}
			ep.Reset()
			return event
		case 'F': // End
			event := &InputEvent{Type: EventEnd}
			ep.Reset()
			return event
		default:
			// Unknown sequence, this character could be printable
			ep.Reset()
			if b >= 32 && b <= 126 {
				ep.pendingChar = b
				ep.hasPending = true
			}
			return &InputEvent{Type: EventEscape}
		}

	case 5: // Mouse event tracking (SGR mode: ESC [ < Cb;Cx;Cy M)
		ep.mouseBuf = append(ep.mouseBuf, b)
		if b == 'M' {
			// Complete mouse event
			mouseData := string(ep.mouseBuf)
			ep.Reset()
			ep.mouseBuf = nil
			return &InputEvent{Type: EventMouse, Data: mouseData}
		}
		if b == 'm' {
			// Complete mouse event (lowercase variant)
			mouseData := string(ep.mouseBuf)
			ep.Reset()
			ep.mouseBuf = nil
			return &InputEvent{Type: EventMouse, Data: mouseData}
		}
		return nil

	case 6: // Mouse event tracking (X10 mode: ESC [ M Cb Cx Cy)
		ep.mouseBuf = append(ep.mouseBuf, b)
		if len(ep.mouseBuf) == 4 {
			// Complete X10 mouse event: ESC [ M Cb Cx Cy
			mouseData := string(ep.mouseBuf)
			ep.Reset()
			ep.mouseBuf = nil
			return &InputEvent{Type: EventMouse, Data: mouseData}
		}
		return nil
	}

	return nil
}

// Reset the parser state
func (ep *EscapeParser) Reset() {
	ep.state = 0
	ep.buffer = ep.buffer[:0]
	ep.mouseBuf = nil
	ep.utf8Buf = nil
	ep.utf8Need = 0
}
