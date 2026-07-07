package console

import (
	"strings"
	"testing"
)

func TestDisplayWidth(t *testing.T) {
	cases := map[string]int{
		"":                   0,
		"hello":              5,
		"日本":                 4, // two wide runes
		"a日b":                4, // 1 + 2 + 1
		"café":               4, // accented (precomposed) = 1 col
		"\x1b[31mred\x1b[0m": 3, // ANSI stripped
	}
	for in, want := range cases {
		if got := displayWidth(in); got != want {
			t.Errorf("displayWidth(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestTruncateToWidth(t *testing.T) {
	if got := truncateToWidth("hello world", 8, "…"); got != "hello w…" {
		t.Errorf("ascii truncate = %q", got)
	}
	if got := truncateToWidth("hello", 10, "…"); got != "hello" {
		t.Errorf("no-trunc = %q", got)
	}
	// Wide runes must not be split and must not overflow the budget.
	got := truncateToWidth("日本語テスト", 5, "…")
	if w := displayWidth(got); w > 5 {
		t.Errorf("truncated %q has width %d > 5", got, w)
	}
}

func TestWrappedGeometry_WideChars(t *testing.T) {
	// "日本" = 4 display cols. At width 10, prompt 0 → one row, cursor at col 4.
	rows, _, _, endRow, endCol := wrappedGeometry(10, 0, "日本", len("日本"))
	if rows != 1 || endRow != 0 || endCol != 4 {
		t.Errorf("wide geometry: rows=%d endRow=%d endCol=%d, want 1/0/4", rows, endRow, endCol)
	}
	// ASCII unchanged: "hello" at width 10 → col 5.
	_, _, _, _, c := wrappedGeometry(10, 0, "hello", len("hello"))
	if c != 5 {
		t.Errorf("ascii endCol = %d, want 5", c)
	}
	// A wide rune at the column edge wraps instead of splitting: width 3,
	// "a日" → 'a' at col0, 日 needs 2 but only col1-2 left... col1+2=3 ok, stays.
	// width 2: 'a' col0→1, 日 needs 2, col1+2=3 > 2 → wraps to row1.
	r, _, _, er, _ := wrappedGeometry(2, 0, "a日", len("a日"))
	if r != 2 || er != 1 {
		t.Errorf("edge wrap: rows=%d endRow=%d, want 2/1", r, er)
	}
}

func TestTruncateLinePreservingANSI(t *testing.T) {
	// Short strings are returned unchanged (no ellipsis).
	if got := truncateLinePreservingANSI("hi", 10); got != "hi" {
		t.Errorf("short string: got %q, want %q", got, "hi")
	}
	// maxCols <= 0 returns empty.
	if got := truncateLinePreservingANSI("hello", 0); got != "" {
		t.Errorf("maxCols=0: got %q, want empty", got)
	}

	// Plain text that overflows gets truncated with an ellipsis and never
	// exceeds the visible width budget.
	got := truncateLinePreservingANSI("hello world", 8)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected trailing ellipsis, got %q", got)
	}
	if w := displayWidth(got); w > 8 {
		t.Errorf("truncated width %d > budget 8 (%q)", w, got)
	}

	// ANSI codes in the kept prefix are preserved. The red color escape on
	// the badge must still be present after truncation.
	red := "\033[31m"
	reset := "\033[0m"
	colored := red + "[coder]" + reset + " running a very long command that surely overflows the terminal width"
	got = truncateLinePreservingANSI(colored, 12)
	if !strings.Contains(got, red) {
		t.Errorf("ANSI color code should be preserved in kept prefix; got %q", got)
	}
	if w := displayWidth(got); w > 12 {
		t.Errorf("colored truncation width %d > 12 (%q)", w, got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected ellipsis on truncated colored string, got %q", got)
	}

	// When truncation cuts BEFORE a trailing reset, the helper appends its
	// own reset so the ellipsis and later output aren't left colored.
	// "AB…CDE" with the color never reset inside the kept prefix.
	openOnly := "\033[36mABCDEF"
	got = truncateLinePreservingANSI(openOnly, 4)
	if !strings.Contains(got, ColorReset) {
		t.Errorf("expected trailing ColorReset when reset was cut; got %q", got)
	}
	if w := displayWidth(got); w > 4 {
		t.Errorf("open-color truncation width %d > 4 (%q)", w, got)
	}

	// Wide-rune safety: never split a CJK rune or overflow the budget.
	wide := "日本語テスト"
	got = truncateLinePreservingANSI(wide, 5)
	if w := displayWidth(got); w > 5 {
		t.Errorf("wide-rune truncation width %d > 5 (%q)", w, got)
	}
}
