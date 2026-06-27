package console

import (
	"bytes"
	"strings"
	"testing"
)

// Regression: SP-078-4 — wraps the steer panel rendering path to prevent regressions.
func TestStatusFooter_SteerPanel_Wrap(t *testing.T) {
	tests := []struct {
		name            string
		text            string
		cursorByte      int
		cols            int
		maxRows         int
		wantMinLines    int
		wantRow         int
		wantCol         int
		wantEllipsis    bool
		wantVisibleW    int // >0: assert visibleRuneWidth == this; 0: assert >= 0
	}{
		{
			name:         "narrow_ascii",
			text:         strings.Repeat("a", 200),
			cursorByte:   100,
			cols:         80,
			maxRows:      6,
			wantMinLines: 3, // 200 / 80 = 2.5 → 3 visual rows (80+80+40)
			wantRow:      1, // byte 100 falls in row 1 (bytes 80–159)
			wantCol:      20, // 100 - 80
			wantEllipsis: false,
		},
		{
			name:         "wide_cjk",
			text:         "你好世界你好世界你好世界你好", // 14 CJK chars, 42 bytes
			cursorByte:   16, // inside char 5 (row 1, col 1) — byte 15 is row 0's inclusive end
			cols:         10, // 5 CJK chars per row (2 cols each = 10)
			maxRows:      6,
			wantMinLines: 3, // 14 chars / 5 per row = 3 rows (5+5+4)
			wantRow:      1, // byte 16 falls in row 1
			wantCol:      1, // 16 - 15 (row 1's start byte)
			wantEllipsis: false,
		},
		{
			name:          "combining_chars",
			text:          "cafe\u0301", // e + combining acute accent = café
			cursorByte:    5, // inside the 2-byte combining acute (bytes 4–5)
			cols:          80,
			maxRows:       6,
			wantMinLines:  1, // single line, combining char has zero display width
			wantRow:       0,
			wantCol:       5, // byte offset within the single line
			wantEllipsis:  false,
			wantVisibleW:  4, // "cafe" = 4 visible; combining acute = 0
		},
		{
			name:         "cursor_at_wrap_boundary",
			text:         strings.Repeat("a", 80) + "X", // 81 bytes: 80-char row 0 + 1-char row 1
			cursorByte:   80, // exactly at the soft-wrap boundary
			cols:         80,
			maxRows:      6,
			wantMinLines: 2, // 81 bytes → 2 rows (80+1)
			wantRow:      0, // byte 80 is inclusive-end of row 0's byte range [0,80]
			wantCol:      80, // at end of row 0's text
			wantEllipsis: false,
		},
		{
			name:         "overflow_dropped_rows",
			text:         strings.Repeat("a", 500), // 500 chars → 7 visual rows at 80 cols
			cursorByte:   5, // in first (dropped) row
			cols:         80,
			maxRows:      6,
			wantMinLines: 6, // capped at maxRows
			wantRow:      0, // cursor in dropped row gets clamped to (0,0)
			wantCol:      0,
			wantEllipsis: true, // first visible row prefixed with "… "
		},
		{
			name:         "empty_buffer",
			text:         "",
			cursorByte:   0,
			cols:         80,
			maxRows:      6,
			wantMinLines: 1, // empty input yields one empty row
			wantRow:      0,
			wantCol:      0,
			wantEllipsis: false,
		},
		{
			name:         "single_line_no_wrap",
			text:         "hello",
			cursorByte:   3,
			cols:         120,
			maxRows:      6,
			wantMinLines: 1, // fits entirely in one row
			wantRow:      0,
			wantCol:      3,
			wantEllipsis: false,
		},
		{
			name:         "hard_line_break",
			text:         "first line\nsecond line that wraps because it is very long and exceeds the terminal width",
			cursorByte:   12, // "first line"=bytes 0-9, '\n'=byte 10, byte 12='e' in "second"
			cols:         40, // forces wrap on the second hard line
			maxRows:      6,
			wantMinLines: 3, // "first line" → row 0; second hard line wraps into rows 1 and 2
			wantRow:      1, // byte 12 falls in row 1 (bytes 11–50)
			wantCol:      1, // 12 - 11 = 1 (offset within the wrapped line)
			wantEllipsis: false,
		},
		{
			name:         "cursor_at_start",
			text:         strings.Repeat("a", 200),
			cursorByte:   0,
			cols:         80,
			maxRows:      6,
			wantMinLines: 3, // 200 / 80 → 3 visual rows
			wantRow:      0, // byte 0 is the first row
			wantCol:      0,
			wantEllipsis: false,
		},
		{
			name:         "very_narrow_terminal",
			text:         "你好世界", // 4 CJK chars, each 2 display cols; 12 bytes
			cursorByte:   3, // first char '你' starts at byte 0
			cols:         5, // very narrow: 2 CJK chars per row (2+2=4 cols ≤ 5)
			maxRows:      6,
			wantMinLines: 2, // "你好" → row 0 (4 cols), "世界" → row 1 (4 cols)
			wantRow:      0, // byte 3 falls in "你好" (bytes 0–5)
			wantCol:      3, // 3 - 0 = 3
			wantEllipsis: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// --- WrapSteerLayout assertions ---
			lines, row, col := WrapSteerLayout(tt.text, tt.cursorByte, tt.cols, tt.maxRows)

			if len(lines) < tt.wantMinLines {
				t.Errorf("lines count = %d, want >= %d", len(lines), tt.wantMinLines)
			}
			if row != tt.wantRow {
				t.Errorf("cursor row = %d, want %d", row, tt.wantRow)
			}
			if col != tt.wantCol {
				t.Errorf("cursor col = %d, want %d", col, tt.wantCol)
			}

			if tt.wantEllipsis {
				if !strings.HasPrefix(lines[0], "\u2026 ") {
					t.Errorf("first line should have \"… \" prefix for overflow, got %q", lines[0][:min(20, len(lines[0]))])
				}
			} else {
				if strings.HasPrefix(lines[0], "\u2026 ") {
					t.Errorf("first line should NOT have \"… \" prefix, got %q", lines[0][:min(20, len(lines[0]))])
				}
			}

			// --- WriteWrappedLines produces valid output with caret ---
			var buf bytes.Buffer
			err := WriteWrappedLines(&buf, lines, tt.cols, row, col)
			if err != nil {
				t.Fatalf("WriteWrappedLines error: %v", err)
			}

			output := buf.String()
			// Split output back into visual rows (each row is exactly `cols` display
			// columns of bytes; since output is just concatenated rows we walk it
			// using visibleLen to find row boundaries).
			// Easiest check: output should contain exactly len(lines) segments
			// each padded to `cols` display width. Verify total display width.
			totalVisibleLen := visibleLen(output)
			if totalVisibleLen != len(lines)*tt.cols {
				t.Errorf("total visible output width = %d, want %d (len(lines)=%d * cols=%d)",
					totalVisibleLen, len(lines)*tt.cols, len(lines), tt.cols)
			}

			// Verify caret appears in the output when there's a valid cursor position.
			if len(lines) > 0 && row >= 0 {
				const caret = "▏"
				if !strings.Contains(output, caret) {
					t.Errorf("output should contain caret %q but does not; output preview: %q",
						caret, output[:min(100, len(output))])
				}
			}

			// For non-empty buffers, verify visibleRuneWidth handles the text without crashing.
			// For the combining_chars case we assert the exact width (combining acute = 0).
			// For all other cases we just check non-negative.
			if tt.text != "" {
				w := visibleRuneWidth(tt.text)
				if tt.wantVisibleW > 0 {
					if w != tt.wantVisibleW {
						t.Errorf("visibleRuneWidth(%q) = %d, want %d (combining chars must have zero display width)", tt.text, w, tt.wantVisibleW)
					}
				} else if w < 0 {
					t.Errorf("visibleRuneWidth returned negative value %d for %q", w, tt.text)
				}
			}
		})
	}
}
