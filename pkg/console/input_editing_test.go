package console

import (
	"testing"
)

// newEditingIR constructs an InputReader suitable for exercising the
// editing primitives without any terminal dependencies. The editing
// methods call Refresh() which writes to stdout, so we point termFd at
// stdout (harmless) and set a generous terminal width.
func newEditingIR(line string, cursor int) *InputReader {
	ir := NewInputReader("> ")
	ir.terminalWidth = 80
	ir.line = line
	ir.cursorPos = cursor
	return ir
}

// ─── MoveWord ────────────────────────────────────────────────────────────────

func TestMoveWord_ForwardAndBackward(t *testing.T) {
	ir := newEditingIR("hello world foo", 0)

	// Forward moves to the end of each word.
	ir.MoveWord(1)
	if ir.cursorPos != 5 {
		t.Fatalf("forward word 1: cursorPos = %d, want 5", ir.cursorPos)
	}
	ir.MoveWord(1)
	if ir.cursorPos != 11 {
		t.Fatalf("forward word 2: cursorPos = %d, want 11", ir.cursorPos)
	}
	ir.MoveWord(1)
	if ir.cursorPos != 15 {
		t.Fatalf("forward word 3: cursorPos = %d, want 15 (end of last word)", ir.cursorPos)
	}

	// Backward moves to the start of each word.
	ir.MoveWord(-1)
	if ir.cursorPos != 12 {
		t.Fatalf("backward word 1: cursorPos = %d, want 12", ir.cursorPos)
	}
	ir.MoveWord(-1)
	if ir.cursorPos != 6 {
		t.Fatalf("backward word 2: cursorPos = %d, want 6", ir.cursorPos)
	}
	ir.MoveWord(-1)
	if ir.cursorPos != 0 {
		t.Fatalf("backward word 3: cursorPos = %d, want 0", ir.cursorPos)
	}
}

func TestMoveWord_WhitespaceHandling(t *testing.T) {
	line := "  hello   world  "
	ir := newEditingIR(line, len(line))

	// From the end, backward word lands on the start of "world" (index 10).
	ir.MoveWord(-1)
	if ir.cursorPos != 10 {
		t.Fatalf("backward 1: cursorPos = %d, want 10", ir.cursorPos)
	}
	// Then to the start of "hello" (index 2).
	ir.MoveWord(-1)
	if ir.cursorPos != 2 {
		t.Fatalf("backward 2: cursorPos = %d, want 2", ir.cursorPos)
	}
	// Then to position 0 (leading whitespace skipped).
	ir.MoveWord(-1)
	if ir.cursorPos != 0 {
		t.Fatalf("backward 3: cursorPos = %d, want 0", ir.cursorPos)
	}
}

func TestMoveWord_MultiByteUTF8(t *testing.T) {
	// "café" = c(1) a(1) f(1) é(2 bytes) = 5 bytes total.
	line := "café résumé"
	ir := newEditingIR(line, 0)

	// Forward word: skip non-whitespace → lands at byte offset 5 (after "café").
	ir.MoveWord(1)
	if ir.cursorPos != 5 {
		t.Fatalf("forward word: cursorPos = %d, want 5 (byte offset after é)", ir.cursorPos)
	}

	// Backward word: lands back at 0.
	ir.MoveWord(-1)
	if ir.cursorPos != 0 {
		t.Fatalf("backward word: cursorPos = %d, want 0", ir.cursorPos)
	}
}

// ─── DeleteWordBackward ──────────────────────────────────────────────────────

func TestDeleteWordBackward_Basic(t *testing.T) {
	ir := newEditingIR("hello world", 11)

	// Delete "world" (and no leading space since cursor is at end of word).
	ir.DeleteWordBackward()
	if ir.line != "hello " {
		t.Fatalf("after delete 1: line = %q, want %q", ir.line, "hello ")
	}
	if ir.cursorPos != 6 {
		t.Fatalf("after delete 1: cursorPos = %d, want 6", ir.cursorPos)
	}

	// From cursor 6 on "hello ", backward word skips trailing space then
	// deletes "hello" → empty line.
	ir.DeleteWordBackward()
	if ir.line != "" {
		t.Fatalf("after delete 2: line = %q, want %q", ir.line, "")
	}
	if ir.cursorPos != 0 {
		t.Fatalf("after delete 2: cursorPos = %d, want 0", ir.cursorPos)
	}

	// At cursor 0 → no-op guard.
	ir.DeleteWordBackward()
	if ir.line != "" {
		t.Fatalf("after delete at start: line = %q, want empty", ir.line)
	}
	if ir.cursorPos != 0 {
		t.Fatalf("after delete at start: cursorPos = %d, want 0", ir.cursorPos)
	}
}

func TestDeleteWordBackward_AtStart(t *testing.T) {
	ir := newEditingIR("hello", 0)

	ir.DeleteWordBackward()
	if ir.line != "hello" {
		t.Fatalf("line changed at cursor 0: got %q", ir.line)
	}
	if ir.cursorPos != 0 {
		t.Fatalf("cursorPos = %d, want 0", ir.cursorPos)
	}
}

func TestDeleteWordBackward_MultiByte(t *testing.T) {
	line := "café au lait"
	ir := newEditingIR(line, len(line))

	// Backward from end deletes "lait" (no trailing whitespace), leaving
	// "café au " with cursor at position 9.
	ir.DeleteWordBackward()
	if ir.line != "café au " {
		t.Fatalf("after delete: line = %q, want %q", ir.line, "café au ")
	}
	wantCursor := len("café au ")
	if ir.cursorPos != wantCursor {
		t.Fatalf("after delete: cursorPos = %d, want %d", ir.cursorPos, wantCursor)
	}
}

// ─── KillToEndOfLine / KillToStartOfLine ─────────────────────────────────────

func TestKillToEndOfLine(t *testing.T) {
	ir := newEditingIR("hello world", 5)

	ir.KillToEndOfLine()
	if ir.line != "hello" {
		t.Fatalf("line = %q, want %q", ir.line, "hello")
	}
	if ir.cursorPos != 5 {
		t.Fatalf("cursorPos = %d, want 5", ir.cursorPos)
	}
}

func TestKillToStartOfLine(t *testing.T) {
	ir := newEditingIR("hello world", 5)

	ir.KillToStartOfLine()
	if ir.line != " world" {
		t.Fatalf("line = %q, want %q", ir.line, " world")
	}
	if ir.cursorPos != 0 {
		t.Fatalf("cursorPos = %d, want 0", ir.cursorPos)
	}
}

// ─── deleteRange ─────────────────────────────────────────────────────────────

func TestDeleteRange_Middle(t *testing.T) {
	// line = "hello world" (11 chars). deleteRange(2, 8) removes "llo wo".
	// Cursor at 5 is inside [2,8) → clamped to start=2.
	ir := newEditingIR("hello world", 5)

	ir.deleteRange(2, 8)
	if ir.line != "herld" {
		t.Fatalf("line = %q, want %q", ir.line, "herld")
	}
	if ir.cursorPos != 2 {
		t.Fatalf("cursorPos = %d, want 2", ir.cursorPos)
	}
}

func TestDeleteRange_CursorAfterDeletedRegion(t *testing.T) {
	// deleteRange(0, 5) removes "hello", cursor at 11 shifts back by 5 → 6.
	ir := newEditingIR("hello world", 11)

	ir.deleteRange(0, 5)
	if ir.line != " world" {
		t.Fatalf("line = %q, want %q", ir.line, " world")
	}
	if ir.cursorPos != 6 {
		t.Fatalf("cursorPos = %d, want 6", ir.cursorPos)
	}
}

func TestDeleteRange_NoOp(t *testing.T) {
	ir := newEditingIR("hello", 3)

	// start == end → no-op.
	ir.deleteRange(3, 3)
	if ir.line != "hello" {
		t.Fatalf("line changed for equal range: got %q", ir.line)
	}
	if ir.cursorPos != 3 {
		t.Fatalf("cursorPos changed for equal range: got %d", ir.cursorPos)
	}

	// start > end → no-op (clamped/no-op guard).
	ir.deleteRange(5, 2)
	if ir.line != "hello" {
		t.Fatalf("line changed for inverted range: got %q", ir.line)
	}
	if ir.cursorPos != 3 {
		t.Fatalf("cursorPos changed for inverted range: got %d", ir.cursorPos)
	}
}
