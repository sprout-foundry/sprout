package console

import (
	"slices"
	"unicode"
	"unicode/utf8"
)

// maxUndoSteps bounds the undo history per line; older steps fall off.
const maxUndoSteps = 200

type editKind int

const (
	editNone editKind = iota
	editTyping
	editDeleting
	editOther
)

type editSnapshot struct {
	line   string
	cursor int
	pastes []pasteSpan
}

// editBuffer is the text model shared by the idle prompt and the mid-turn
// steer/queue box: the line, a byte-offset cursor that always sits on a
// rune boundary, the kill buffer behind Ctrl-Y, and undo history. It does
// no locking or drawing; the readers wrap it with their own.
type editBuffer struct {
	line            string
	cursorPos       int
	killBuffer      string
	collapsedPastes []pasteSpan
	undoStack       []editSnapshot
	lastEdit        editKind
}

// checkpoint records the state before an edit of the given kind. Runs of
// typing or deleting collapse into one undo step, split at word starts so
// undo walks back a word at a time rather than a character or a sentence.
func (b *editBuffer) checkpoint(kind editKind, text string) {
	wordStart := kind == editTyping && startsWithSpace(text)
	if kind != editOther && kind == b.lastEdit && !wordStart {
		return
	}
	b.undoStack = append(b.undoStack, editSnapshot{b.line, b.cursorPos, slices.Clone(b.collapsedPastes)})
	if len(b.undoStack) > maxUndoSteps {
		b.undoStack = slices.Delete(b.undoStack, 0, len(b.undoStack)-maxUndoSteps)
	}
	b.lastEdit = kind
}

func startsWithSpace(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsSpace(r)
}

// endEditRun makes the next edit start its own undo step (after a cursor
// move, say, typing elsewhere is a separate change).
func (b *editBuffer) endEditRun() {
	b.lastEdit = editNone
}

// undo restores the state before the most recent edit step.
func (b *editBuffer) undo() bool {
	n := len(b.undoStack)
	if n == 0 {
		return false
	}
	s := b.undoStack[n-1]
	b.undoStack = b.undoStack[:n-1]
	b.line, b.cursorPos, b.collapsedPastes = s.line, s.cursor, s.pastes
	b.lastEdit = editNone
	return true
}

// resetEdit empties the line and forgets its undo history.
func (b *editBuffer) resetEdit() {
	b.line = ""
	b.cursorPos = 0
	b.collapsedPastes = b.collapsedPastes[:0]
	b.undoStack = b.undoStack[:0]
	b.lastEdit = editNone
}

// replaceLine swaps in new text (history recall, a completion) as one
// undoable step with the cursor at its end.
func (b *editBuffer) replaceLine(text string) {
	if text == b.line {
		b.cursorPos = len(text)
		return
	}
	b.checkpoint(editOther, "")
	b.line = text
	b.cursorPos = len(text)
	b.collapsedPastes = b.collapsedPastes[:0]
}

// insertText puts text at the cursor and moves the cursor past it.
func (b *editBuffer) insertText(text string) {
	if text == "" {
		return
	}
	kind := editOther
	if utf8.RuneCountInString(text) == 1 {
		kind = editTyping
	}
	b.checkpoint(kind, text)
	at := b.cursorPos
	b.line = b.line[:at] + text + b.line[at:]
	b.cursorPos += len(text)
	b.shiftPasteSpans(at, len(text))
}

// shiftPasteSpans moves collapsed-paste spans at or after pos by delta
// bytes so they track the text they cover. An edit inside a span is
// ambiguous, so that span expands back to plain text.
func (b *editBuffer) shiftPasteSpans(pos, delta int) {
	if delta == 0 || len(b.collapsedPastes) == 0 {
		return
	}
	kept := b.collapsedPastes[:0]
	for _, span := range b.collapsedPastes {
		if span.end <= pos {
			kept = append(kept, span)
			continue
		}
		if span.start < pos {
			continue
		}
		span.start = max(span.start+delta, 0)
		span.end = min(span.end+delta, len(b.line))
		if span.end > span.start {
			kept = append(kept, span)
		}
	}
	b.collapsedPastes = kept
}

// deleteRange removes [start, end), clamped to the line, keeping the
// cursor on the same text and dropping paste spans the range overlaps.
func (b *editBuffer) deleteRange(start, end int, kind editKind) bool {
	start, end = max(start, 0), min(end, len(b.line))
	if start >= end {
		return false
	}
	b.checkpoint(kind, "")
	removed := end - start
	b.line = b.line[:start] + b.line[end:]
	if b.cursorPos > start {
		b.cursorPos = max(start, b.cursorPos-removed)
	}
	kept := b.collapsedPastes[:0]
	for _, span := range b.collapsedPastes {
		switch {
		case span.end <= start:
			kept = append(kept, span)
		case span.start >= end:
			span.start -= removed
			span.end -= removed
			kept = append(kept, span)
		}
	}
	b.collapsedPastes = kept
	return true
}

// deleteRuneBefore removes the rune before the cursor (Backspace).
func (b *editBuffer) deleteRuneBefore() bool {
	if b.cursorPos == 0 {
		return false
	}
	_, size := utf8.DecodeLastRuneInString(b.line[:b.cursorPos])
	return b.deleteRange(b.cursorPos-size, b.cursorPos, editDeleting)
}

// deleteRuneAt removes the rune under the cursor (Delete / Ctrl-D).
func (b *editBuffer) deleteRuneAt() bool {
	if b.cursorPos >= len(b.line) {
		return false
	}
	_, size := utf8.DecodeRuneInString(b.line[b.cursorPos:])
	return b.deleteRange(b.cursorPos, b.cursorPos+size, editDeleting)
}

// kill removes [start, end) into the kill buffer for a later yank.
func (b *editBuffer) kill(start, end int) bool {
	start, end = max(start, 0), min(end, len(b.line))
	if start >= end {
		return false
	}
	b.killBuffer = b.line[start:end]
	return b.deleteRange(start, end, editOther)
}

func (b *editBuffer) killWordBefore() bool {
	return b.kill(wordBoundary(b.line, b.cursorPos, -1), b.cursorPos)
}

func (b *editBuffer) killWordAfter() bool {
	return b.kill(b.cursorPos, wordBoundary(b.line, b.cursorPos, 1))
}

func (b *editBuffer) killToEnd() bool { return b.kill(b.cursorPos, len(b.line)) }

func (b *editBuffer) killToStart() bool { return b.kill(0, b.cursorPos) }

// moveRunes steps the cursor delta runes left (negative) or right.
func (b *editBuffer) moveRunes(delta int) bool {
	pos := b.cursorPos
	for ; delta < 0 && pos > 0; delta++ {
		_, size := utf8.DecodeLastRuneInString(b.line[:pos])
		pos -= size
	}
	for ; delta > 0 && pos < len(b.line); delta-- {
		_, size := utf8.DecodeRuneInString(b.line[pos:])
		pos += size
	}
	return b.moveTo(pos)
}

// moveTo places the cursor at a byte offset within the line.
func (b *editBuffer) moveTo(pos int) bool {
	pos = max(0, min(pos, len(b.line)))
	if pos == b.cursorPos {
		return false
	}
	b.cursorPos = pos
	b.endEditRun()
	return true
}

func (b *editBuffer) moveWord(direction int) bool {
	return b.moveTo(wordBoundary(b.line, b.cursorPos, direction))
}

// wordBoundary returns the byte offset one word away from pos in the given
// direction (-1 back, +1 forward): past any whitespace, then past the run
// of non-whitespace (unicode.IsSpace) that follows.
func wordBoundary(line string, pos, direction int) int {
	for _, wantSpace := range []bool{true, false} {
		for direction < 0 && pos > 0 {
			r, size := utf8.DecodeLastRuneInString(line[:pos])
			if unicode.IsSpace(r) != wantSpace {
				break
			}
			pos -= size
		}
		for direction > 0 && pos < len(line) {
			r, size := utf8.DecodeRuneInString(line[pos:])
			if unicode.IsSpace(r) != wantSpace {
				break
			}
			pos += size
		}
	}
	return pos
}
