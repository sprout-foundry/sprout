package agent

import (
	"errors"
	"testing"
)

func TestCountLinesInSegment(t *testing.T) {
	tests := []struct {
		name    string
		segment string
		want    int
	}{
		{"empty", "", 0},
		{"no_newline", "hello", 1},
		{"trailing_newline", "hello\n", 1},
		{"two_lines", "a\nb", 2},
		{"three_lines_trailing", "a\nb\nc\n", 3},
		{"multiple_newlines", "a\n\nb", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countLinesInSegment(tt.segment); got != tt.want {
				t.Errorf("countLinesInSegment(%q) = %d, want %d", tt.segment, got, tt.want)
			}
		})
	}
}

func TestBuildTruncationNoticeWithPath(t *testing.T) {
	notice := buildTruncationNotice(800, 1700, 500, 120, "/path/to/output", nil)

	want := "[Output truncated: omitted 500 middle token(s) across ~120 line(s). Showing first 800 tokens and last 1700 tokens. Full output saved to /path/to/output]"
	if notice != want {
		t.Errorf("buildTruncationNotice(with path) =\n%q\nwant\n%q", notice, want)
	}
}

func TestBuildTruncationNoticeWithoutPath(t *testing.T) {
	notice := buildTruncationNotice(800, 1700, 500, 120, "", nil)

	want := "[Output truncated: omitted 500 middle token(s) across ~120 line(s). Showing first 800 tokens and last 1700 tokens. Full output path unavailable]"
	if notice != want {
		t.Errorf("buildTruncationNotice(no path) =\n%q\nwant\n%q", notice, want)
	}
}

func TestBuildTruncationNoticeWithError(t *testing.T) {
	saveErr := errors.New("permission denied")
	notice := buildTruncationNotice(800, 1700, 500, 120, "", saveErr)

	want := "[Output truncated: omitted 500 middle token(s) across ~120 line(s). Showing first 800 tokens and last 1700 tokens. Failed to save full output: permission denied]"
	if notice != want {
		t.Errorf("buildTruncationNotice(with error) =\n%q\nwant\n%q", notice, want)
	}
}
