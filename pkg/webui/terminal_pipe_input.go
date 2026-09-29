//go:build !js

package webui

import "unicode/utf8"

// writeInputLocked sends user keystrokes to the shell. Caller holds s.mutex.
//
// A PTY's line discipline turns xterm.js's CR into NL, echoes keystrokes and
// applies backspace before the shell sees a line. Pipe (NoPTY) sessions have
// none of that — Enter would never submit a command — so it is emulated here.
func (s *TerminalSession) writeInputLocked(input []byte) (int, error) {
	if !s.NoPTY {
		return s.Pty.Write(input)
	}
	if out := s.cookPipeInputLocked(input); len(out) > 0 {
		if _, err := s.Pty.Write(out); err != nil {
			return 0, err
		}
	}
	return len(input), nil
}

// cookPipeInputLocked buffers input into s.pipeLine, broadcasts the echo, and
// returns the completed lines to write to the shell.
func (s *TerminalSession) cookPipeInputLocked(input []byte) []byte {
	var out, echo []byte
	for i := 0; i < len(input); i++ {
		b := input[i]
		switch {
		case b == '\r' || b == '\n':
			if b == '\r' && i+1 < len(input) && input[i+1] == '\n' {
				i++
			}
			out = append(append(out, s.pipeLine...), '\n')
			s.pipeLine = s.pipeLine[:0]
			echo = append(echo, '\r', '\n')
		case b == 0x7f || b == '\b':
			if len(s.pipeLine) > 0 {
				_, size := utf8.DecodeLastRune(s.pipeLine)
				s.pipeLine = s.pipeLine[:len(s.pipeLine)-size]
				echo = append(echo, '\b', ' ', '\b')
			}
		case b == 0x03:
			s.pipeLine = s.pipeLine[:0]
			echo = append(echo, '^', 'C', '\r', '\n')
		case b == 0x1b:
			i = skipEscapeSequence(input, i)
		case b == '\t' || b >= 0x20:
			s.pipeLine = append(s.pipeLine, b)
			echo = append(echo, b)
		}
	}
	if len(echo) > 0 {
		s.broadcast(echo)
	}
	return out
}

// skipEscapeSequence returns the index of the last byte of the escape
// sequence starting at input[start] (arrow keys, function keys, focus
// events), which have no meaning without line editing.
func skipEscapeSequence(input []byte, start int) int {
	i := start + 1
	if i >= len(input) {
		return start
	}
	switch input[i] {
	case '[':
		for i++; i < len(input); i++ {
			if input[i] >= 0x40 && input[i] <= 0x7e {
				return i
			}
		}
		return len(input) - 1
	case 'O':
		if i+1 < len(input) {
			return i + 1
		}
		return i
	default:
		return i
	}
}
