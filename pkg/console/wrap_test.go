package console

import (
	"strings"
	"testing"
)

func TestWrapHardLine_Empty(t *testing.T) {
	got := WrapHardLine("", 10)
	if len(got) != 1 || got[0] != "" {
		t.Fatalf("expected [\"\"] for empty input, got %v", got)
	}
}

func TestWrapHardLine_ASCIIUnderLimit(t *testing.T) {
	got := WrapHardLine("hello", 10)
	if len(got) != 1 || got[0] != "hello" {
		t.Fatalf("expected [\"hello\"], got %v", got)
	}
}

func TestWrapHardLine_ASCIIExactlyLimit(t *testing.T) {
	got := WrapHardLine("helloworld", 10)
	if len(got) != 1 || got[0] != "helloworld" {
		t.Fatalf("expected one row, got %v", got)
	}
}

func TestWrapHardLine_ASCIIOverflow(t *testing.T) {
	got := WrapHardLine("aaaaaa", 2)
	want := []string{"aa", "aa", "aa"}
	if !equalStrings(got, want) {
		t.Fatalf("WrapHardLine(\"aaaaaa\", 2) = %v, want %v", got, want)
	}
}

func TestWrapHardLine_WideRune(t *testing.T) {
	// Each CJK rune is 2 cols. With cols=3, adding 好 after 你 (2 cols)
	// would exceed 3, so each rune gets its own row.
	got := WrapHardLine("你好世界", 3)
	want := []string{"你", "好", "世", "界"}
	if !equalStrings(got, want) {
		t.Fatalf("WrapHardLine CJK = %v, want %v", got, want)
	}
}

func TestWrapSteerLayout_BasicWrap(t *testing.T) {
	text := strings.Repeat("a", 200)
	cols := 80
	lines, cursorRow, cursorCol := WrapSteerLayout(text, 100, cols, 6)
	// cols=80 → 3 visual rows (200/80 ceil = 3), cursor at byte 100 → row 1, col 20
	if len(lines) != 3 {
		t.Fatalf("expected 3 visual rows, got %d (%q)", len(lines), lines)
	}
	if cursorRow != 1 || cursorCol != 20 {
		t.Fatalf("cursor at byte 100 in 200a → row=1 col=20, got row=%d col=%d", cursorRow, cursorCol)
	}
}

func TestWrapSteerLayout_HardBreakInsideWrap(t *testing.T) {
	// 10 'a's, \n, then 200 'b's. Cursor at byte 15 = first 'b' + 4.
	first := strings.Repeat("a", 10)
	second := strings.Repeat("b", 200)
	text := first + "\n" + second
	lines, cursorRow, cursorCol := WrapSteerLayout(text, 15, 80, 6)
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 visual rows, got %d (%q)", len(lines), lines)
	}
	if lines[0] != first {
		t.Fatalf("row 0 should be first hard line, got %q", lines[0])
	}
	if cursorRow != 1 || cursorCol != 4 {
		// cursor byte 15 = 10 'a's (0-9) + \n (10) + 4 'b's (11..15) → in
		// second hard line at byte offset 4, wrapped into 1 row at col 4.
		t.Fatalf("cursor at byte 15: expected row=1 col=4, got row=%d col=%d", cursorRow, cursorCol)
	}
}

func TestWrapSteerLayout_CJKWraps(t *testing.T) {
	// 20x "你好" at cols=10. With terminal-autowrap semantics (a rune
	// fitting at the last col is OK; next rune wraps), each row holds
	// up to 5 pairs (10 cols), but the last pair of an odd-count row
	// spills into the next row. 20 pairs → 4 full rows + 2 partial.
	// After the 6-row cap test would crop, but maxRows=6 here so we
	// see all rows.
	text := strings.Repeat("你好", 20)
	lines, _, _ := WrapSteerLayout(text, 0, 10, 0) // 0 = no cap
	if len(lines) != 8 {
		// 4 full "你好你好你" pairs (5 pairs × 2 cols = 10 cols, last rune
		// fits at col 10) + 4 partial "好你好你好" pairs: 8 rows total.
		t.Fatalf("expected 8 visual rows of CJK at cols=10, got %d (%q)", len(lines), lines)
	}
}

func TestWrapSteerLayout_MaxRowsCap(t *testing.T) {
	// 20 hard lines of single chars → 20 visual rows; cap at 6.
	var b strings.Builder
	for i := 0; i < 20; i++ {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteByte('x')
	}
	lines, cursorRow, _ := WrapSteerLayout(b.String(), 0, 80, 6)
	if len(lines) != 6 {
		t.Fatalf("expected cap at 6, got %d", len(lines))
	}
	if !strings.HasPrefix(lines[0], "… ") {
		t.Fatalf("expected topmost visible row to start with '… ', got %q", lines[0])
	}
	// Cursor byte 0 was on the first hard line — now clipped, so cursor at row 0 col 0.
	if cursorRow != 0 {
		t.Fatalf("cursor clipped to row 0, got %d", cursorRow)
	}
}

func TestWrapSteerLayout_CursorAtEnd(t *testing.T) {
	text := "hello"
	lines, cursorRow, cursorCol := WrapSteerLayout(text, 5, 80, 6)
	if len(lines) != 1 {
		t.Fatalf("expected 1 row, got %d", len(lines))
	}
	if cursorRow != 0 || cursorCol != 5 {
		t.Fatalf("expected (0, 5), got (%d, %d)", cursorRow, cursorCol)
	}
}

func TestWrapSteerLayout_EmptyBuffer(t *testing.T) {
	lines, cursorRow, cursorCol := WrapSteerLayout("", 0, 80, 6)
	if len(lines) != 1 || lines[0] != "" {
		t.Fatalf("expected 1 empty row, got %v", lines)
	}
	if cursorRow != 0 || cursorCol != 0 {
		t.Fatalf("expected (0, 0), got (%d, %d)", cursorRow, cursorCol)
	}
}

func TestWrapSteerLayout_CursorAtNewlineByte(t *testing.T) {
	// Cursor byte exactly on the \n separator — should map to end of
	// the previous hard line's last visual row.
	text := "hello\nworld"
	lines, cursorRow, cursorCol := WrapSteerLayout(text, 5, 80, 6)
	if len(lines) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(lines))
	}
	if cursorRow != 0 || cursorCol != 5 {
		t.Fatalf("byte 5 (end of 'hello') → row=0 col=5, got (%d, %d)", cursorRow, cursorCol)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}