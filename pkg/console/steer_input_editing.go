package console

import (
	"slices"
	"unicode"
	"unicode/utf8"
)

// handleBackspace removes the RUNE (not byte) immediately before the
// cursor so a single backspace on a multi-byte character — Greek "α",
// Han "字", emoji "🚀" — deletes the whole glyph rather than corrupting
// it. Walks backward from the cursor skipping UTF-8 continuation bytes
// (10xxxxxx) until it finds a lead byte (or ASCII) to drop. A no-op
// when the cursor is at position 0.
//
// Also exits history navigation: editing a recalled entry treats it
// as a fresh in-progress message.
func (r *SteerInputReader) handleBackspace() {
	r.mu.Lock()
	if r.cursorPos > 0 {
		// Find the start of the rune just before the cursor by walking
		// back over continuation bytes (0x80..0xBF).
		i := r.cursorPos - 1
		for i > 0 && r.buffer[i]&0xC0 == 0x80 {
			i--
		}
		r.buffer = slices.Delete(r.buffer, i, r.cursorPos)
		r.cursorPos = i
	}
	r.historyIndex = -1
	r.pendingBuffer = nil
	r.resetCompletionCycleLocked()
	r.mu.Unlock()
	r.renderLine()
}

// insertAtCursor inserts a byte sequence at the cursor position and
// advances the cursor. Edits exit history navigation. Caller must NOT
// hold r.mu.
func (r *SteerInputReader) insertAtCursor(data []byte) {
	r.mu.Lock()
	r.buffer = slices.Insert(r.buffer, r.cursorPos, data...)
	r.cursorPos += len(data)
	r.historyIndex = -1
	r.pendingBuffer = nil
	r.resetCompletionCycleLocked()
	r.mu.Unlock()
	r.renderLine()
}

// moveCursorStart moves the cursor to byte 0 (Ctrl+A / Home).
func (r *SteerInputReader) moveCursorStart() {
	r.mu.Lock()
	r.cursorPos = 0
	r.mu.Unlock()
	r.renderLine()
}

// moveCursorEnd moves the cursor to the end of the buffer (Ctrl+E / End).
func (r *SteerInputReader) moveCursorEnd() {
	r.mu.Lock()
	r.cursorPos = len(r.buffer)
	r.mu.Unlock()
	r.renderLine()
}

// moveCursorBackward moves the cursor back one rune (Ctrl+B / Left).
// A no-op when the cursor is already at the start.
func (r *SteerInputReader) moveCursorBackward() {
	r.mu.Lock()
	if r.cursorPos > 0 {
		_, sz := utf8.DecodeLastRune(r.buffer[:r.cursorPos])
		r.cursorPos -= sz
	}
	r.mu.Unlock()
	r.renderLine()
}

// moveCursorForward moves the cursor forward one rune (Ctrl+F / Right).
// A no-op when the cursor is already at the end.
func (r *SteerInputReader) moveCursorForward() {
	r.mu.Lock()
	if r.cursorPos < len(r.buffer) {
		_, sz := utf8.DecodeRune(r.buffer[r.cursorPos:])
		r.cursorPos += sz
	}
	r.mu.Unlock()
	r.renderLine()
}

// moveWord moves the cursor by one word (delta -1 = backward, +1 =
// forward). A word is a maximal run of non-whitespace (unicode.IsSpace),
// matching the main InputReader's MoveWord semantics.
func (r *SteerInputReader) moveWord(delta int) {
	r.mu.Lock()
	pos := r.cursorPos
	buf := r.buffer
	if delta < 0 {
		// Skip whitespace backward.
		for pos > 0 {
			rr, sz := utf8.DecodeLastRune(buf[:pos])
			if unicode.IsSpace(rr) {
				pos -= sz
			} else {
				break
			}
		}
		// Skip non-whitespace backward.
		for pos > 0 {
			rr, sz := utf8.DecodeLastRune(buf[:pos])
			if !unicode.IsSpace(rr) {
				pos -= sz
			} else {
				break
			}
		}
	} else {
		// Skip whitespace forward.
		for pos < len(buf) {
			rr, sz := utf8.DecodeRune(buf[pos:])
			if unicode.IsSpace(rr) {
				pos += sz
			} else {
				break
			}
		}
		// Skip non-whitespace forward.
		for pos < len(buf) {
			rr, sz := utf8.DecodeRune(buf[pos:])
			if !unicode.IsSpace(rr) {
				pos += sz
			} else {
				break
			}
		}
	}
	r.cursorPos = pos
	r.mu.Unlock()
	r.renderLine()
}

// deleteWordBackward deletes the word before the cursor (Ctrl-W /
// Alt-Backspace). A no-op when the cursor is at the start or only
// whitespace precedes it.
func (r *SteerInputReader) deleteWordBackward() {
	r.mu.Lock()
	if r.cursorPos == 0 {
		r.mu.Unlock()
		return
	}
	pos := r.cursorPos
	buf := r.buffer
	// Skip whitespace backward.
	for pos > 0 {
		rr, sz := utf8.DecodeLastRune(buf[:pos])
		if unicode.IsSpace(rr) {
			pos -= sz
		} else {
			break
		}
	}
	// Skip non-whitespace backward.
	for pos > 0 {
		rr, sz := utf8.DecodeLastRune(buf[:pos])
		if !unicode.IsSpace(rr) {
			pos -= sz
		} else {
			break
		}
	}
	r.buffer = slices.Delete(r.buffer, pos, r.cursorPos)
	r.cursorPos = pos
	r.historyIndex = -1
	r.pendingBuffer = nil
	r.resetCompletionCycleLocked()
	r.mu.Unlock()
	r.renderLine()
}

// killToEnd deletes from the cursor to the end of the buffer (Ctrl-K).
// A no-op when the cursor is already at the end.
func (r *SteerInputReader) killToEnd() {
	r.mu.Lock()
	if r.cursorPos >= len(r.buffer) {
		r.mu.Unlock()
		return
	}
	r.buffer = r.buffer[:r.cursorPos]
	r.historyIndex = -1
	r.pendingBuffer = nil
	r.resetCompletionCycleLocked()
	r.mu.Unlock()
	r.renderLine()
}

// killToStart deletes from the start of the buffer to the cursor
// (Ctrl-U). A no-op when the cursor is already at the start.
func (r *SteerInputReader) killToStart() {
	r.mu.Lock()
	if r.cursorPos == 0 {
		r.mu.Unlock()
		return
	}
	r.buffer = slices.Delete(r.buffer, 0, r.cursorPos)
	r.cursorPos = 0
	r.historyIndex = -1
	r.pendingBuffer = nil
	r.resetCompletionCycleLocked()
	r.mu.Unlock()
	r.renderLine()
}

// deleteForward deletes the rune at the cursor (Ctrl-D on non-empty).
// A no-op when the cursor is at the end.
func (r *SteerInputReader) deleteForward() {
	r.mu.Lock()
	if r.cursorPos >= len(r.buffer) {
		r.mu.Unlock()
		return
	}
	_, sz := utf8.DecodeRune(r.buffer[r.cursorPos:])
	r.buffer = slices.Delete(r.buffer, r.cursorPos, r.cursorPos+sz)
	r.historyIndex = -1
	r.pendingBuffer = nil
	r.resetCompletionCycleLocked()
	r.mu.Unlock()
	r.renderLine()
}
