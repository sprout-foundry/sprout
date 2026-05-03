package tools

import (
	"strings"
	"testing"
)

func TestCountLines(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected int
	}{
		{"empty", "", 0},
		{"single line no newline", "hello", 1},
		{"two lines", "hello\nworld", 2},
		{"three lines no trailing newline", "a\nb\nc", 3},
		{"trailing newline counts as extra line", "a\nb\nc\n", 4},
		{"just newline", "\n", 2},
		{"two newlines", "\n\n", 3},
		{"single char", "x", 1},
		{"many lines", "1\n2\n3\n4\n5\n6\n7\n8\n9\n10", 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countLines([]byte(tt.content))
			if got != tt.expected {
				t.Errorf("countLines(%q) = %d, want %d", tt.content, got, tt.expected)
			}
		})
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{"empty", "", nil},
		{"single line", "hello", []string{"hello"}},
		{"two lines", "hello\nworld", []string{"hello", "world"}},
		{"three lines", "a\nb\nc", []string{"a", "b", "c"}},
		{"trailing newline", "a\n", []string{"a", ""}},
		{"just newline", "\n", []string{"", ""}},
		{"two newlines", "\n\n", []string{"", "", ""}},
		{"single char", "x", []string{"x"}},
		{"multi with trailing", "a\nb\nc\n", []string{"a", "b", "c", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitLines([]byte(tt.content))
			if !sliceEqual(got, tt.expected) {
				t.Errorf("splitLines(%q) = %v, want %v", tt.content, got, tt.expected)
			}
		})
	}
}

func TestJoinLines(t *testing.T) {
	tests := []struct {
		name     string
		lines    []string
		expected string
	}{
		{"empty", []string{}, ""},
		{"nil", nil, ""},
		{"single", []string{"a"}, "a"},
		{"two", []string{"a", "b"}, "a\nb"},
		{"three", []string{"a", "b", "c"}, "a\nb\nc"},
		{"with empty in middle", []string{"a", "", "c"}, "a\n\nc"},
		{"with trailing empty", []string{"a", ""}, "a\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := joinLines(tt.lines)
			if got != tt.expected {
				t.Errorf("joinLines(%v) = %q, want %q", tt.lines, got, tt.expected)
			}
		})
	}
}

func TestFormatWriteSummary(t *testing.T) {
	t.Run("short file with full content", func(t *testing.T) {
		content := "line1\nline2\nline3"
		got := formatWriteSummary("test.txt", []byte(content), int64(len(content)))

		if !strings.Contains(got, "Successfully wrote test.txt") {
			t.Errorf("Should contain file path, got: %s", got)
		}
		if !strings.Contains(got, "3 lines") {
			t.Errorf("Should contain line count, got: %s", got)
		}
		if !strings.Contains(got, "line1") {
			t.Errorf("Should contain full content for short file, got: %s", got)
		}
		if !strings.Contains(got, "line3") {
			t.Errorf("Should contain full content for short file, got: %s", got)
		}
		// Should NOT contain First/Last markers
		if strings.Contains(got, "First 5 lines") {
			t.Errorf("Short file should not contain 'First 5 lines', got: %s", got)
		}
		if strings.Contains(got, "Last 5 lines") {
			t.Errorf("Short file should not contain 'Last 5 lines', got: %s", got)
		}
	})

	t.Run("long file with summary", func(t *testing.T) {
		// Build content with >10 lines
		var lines []string
		for i := 1; i <= 15; i++ {
			lines = append(lines, "line")
		}
		content := joinLines(lines)
		got := formatWriteSummary("bigfile.txt", []byte(content), int64(len(content)))

		if !strings.Contains(got, "Successfully wrote bigfile.txt") {
			t.Errorf("Should contain file path, got: %s", got)
		}
		if !strings.Contains(got, "15 lines") {
			t.Errorf("Should contain line count, got: %s", got)
		}
		if !strings.Contains(got, "First 5 lines") {
			t.Errorf("Should contain 'First 5 lines' for long file, got: %s", got)
		}
		if !strings.Contains(got, "Last 5 lines") {
			t.Errorf("Should contain 'Last 5 lines' for long file, got: %s", got)
		}
	})

	t.Run("exactly 10 lines shows full content", func(t *testing.T) {
		// 10 lines = 9 newlines + content
		var lines []string
		for i := 1; i <= 10; i++ {
			lines = append(lines, "line")
		}
		content := joinLines(lines)
		got := formatWriteSummary("tenlines.txt", []byte(content), int64(len(content)))

		if !strings.Contains(got, "10 lines") {
			t.Errorf("Should contain '10 lines', got: %s", got)
		}
		// 10 lines should show full content (<=10 threshold)
		if strings.Contains(got, "First 5 lines") {
			t.Errorf("10-line file should not use summary format, got: %s", got)
		}
	})

	t.Run("11 lines uses summary format", func(t *testing.T) {
		var lines []string
		for i := 1; i <= 11; i++ {
			lines = append(lines, "line")
		}
		content := joinLines(lines)
		got := formatWriteSummary("elevenlines.txt", []byte(content), int64(len(content)))

		if !strings.Contains(got, "11 lines") {
			t.Errorf("Should contain '11 lines', got: %s", got)
		}
		if !strings.Contains(got, "First 5 lines") {
			t.Errorf("11-line file should use summary format, got: %s", got)
		}
	})
}

// sliceEqual compares two string slices for equality.
func sliceEqual(a, b []string) bool {
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
