package console

import "testing"

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
