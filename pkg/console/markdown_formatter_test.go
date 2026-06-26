package console

import (
	"strings"
	"testing"
)

func TestMarkdownFormatter_BasicFormatting(t *testing.T) {
	formatter := NewMarkdownFormatter(true, true)

	tests := []struct {
		name     string
		input    string
		contains []string
	}{
		{
			name:  "Headers",
			input: "# Main Title\n## Subtitle\n### Section",
			contains: []string{
				ColorBold + ColorBrightBlue,
				"█",
				ColorBold + ColorCyan,
				"▪",
				ColorBold + ColorBlue,
				"▸",
			},
		},
		{
			name:  "Inline formatting",
			input: "This has **bold** and *italic* and `code` text",
			contains: []string{
				ColorBold,
				ColorItalic,
				BgGray,
			},
		},
		{
			name:  "Lists",
			input: "- First item\n- Second item\n* Third item",
			contains: []string{
				ColorGreen + "-",
				ColorGreen + "*",
			},
		},
		{
			// Code-block decoration was lightened: one optional
			// "──── lang ────" header line and a dim "│ " gutter per
			// code row, instead of the previous four rows of chrome
			// (┌─/│ Language/│/└─). The language label and the
			// per-line gutter are still present, just without the
			// surrounding box.
			name:  "Code block",
			input: "```go\nfunc main() {\n  println(\"hello\")\n}\n```",
			contains: []string{
				"──── go ────",
				"│ ",
				ColorBlue + "func",
				ColorGreen + "hello",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.Format(tt.input)
			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected result to contain %q, but got:\n%s", expected, result)
				}
			}
		})
	}
}

// TestMarkdownFormatter_LongLineNotTruncated pins the Scanner buffer
// bump: a single line over 64 KiB (the default Scanner cap) must round-
// trip through Format without being silently dropped. Before the fix
// large generated diffs or one-line minified code in assistant output
// would simply vanish from the rendered turn.
func TestMarkdownFormatter_LongLineNotTruncated(t *testing.T) {
	formatter := NewMarkdownFormatter(true, true)
	// 100 KiB single-line payload — well past the 64 KiB Scanner default.
	long := strings.Repeat("abcdefghij", 100*1024/10)
	if len(long) < 64*1024 {
		t.Fatalf("test setup: expected >64KiB, got %d", len(long))
	}
	input := "before\n" + long + "\nafter"
	result := formatter.Format(input)
	if !strings.Contains(result, "before") {
		t.Errorf("missing prefix line in output")
	}
	if !strings.Contains(result, "after") {
		t.Errorf("missing suffix line — long line was silently dropped by the Scanner")
	}
	// The long line itself should also survive (sample a chunk of it).
	if !strings.Contains(result, strings.Repeat("abcdefghij", 100)) {
		t.Errorf("long line content missing from output")
	}
}

func TestMarkdownFormatter_DisabledColors(t *testing.T) {
	formatter := NewMarkdownFormatter(false, true)

	input := "# Title\n**bold** and `code`"
	result := formatter.Format(input)

	// Should contain no ANSI codes
	if strings.Contains(result, "\033[") {
		t.Errorf("Expected no ANSI codes when colors disabled, got: %s", result)
	}

	// Should contain stripped content
	if !strings.Contains(result, "Title") || !strings.Contains(result, "bold") {
		t.Errorf("Expected stripped content to remain, got: %s", result)
	}
}

func TestIsLikelyMarkdown(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"plain text", false},
		{"# header", true},
		{"**bold**", true},
		{"`code`", true},
		{"```\ncode\n```", true},
		{"- list item", true},
		{"[link](url)", true},
		{"> quote", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := IsLikelyMarkdown(tt.input)
			if result != tt.expected {
				t.Errorf("IsLikelyMarkdown(%q) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCIOutputHandler_Integration(t *testing.T) {
	handler := NewCIOutputHandler(&strings.Builder{})

	// Test markdown is passed through without processing
	markdown := "# Test\n**bold** text"
	_, err := handler.Write([]byte(markdown))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Markdown should be passed through unchanged (no processing)
}

func TestMarkdownFormatter_NO_COLOR_SuppressesANSI(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "")
	formatter := NewMarkdownFormatter(true, true)
	result := formatter.Format("# Title\n**bold** text")
	if strings.Contains(result, "\033[") {
		t.Errorf("NO_COLOR=1 should suppress all ANSI, but got: %q", result)
	}
}

func TestMarkdownFormatter_FORCE_COLOR_EnablesANSI(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")
	formatter := NewMarkdownFormatter(false, true)
	result := formatter.Format("# Title\n**bold** text")
	if !strings.Contains(result, "\033[") {
		t.Errorf("FORCE_COLOR=1 should enable ANSI even when formatter created with false, but got: %q", result)
	}
}

func TestMarkdownFormatter_NO_COLOR_beats_FORCE_COLOR(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "1")
	formatter := NewMarkdownFormatter(true, true)
	result := formatter.Format("# Title\n**bold** text")
	if strings.Contains(result, "\033[") {
		t.Errorf("NO_COLOR should win over FORCE_COLOR, but got ANSI in: %q", result)
	}
}

func TestMarkdownFormatter_UnderscoreItalicCommonMarkBoundaries(t *testing.T) {
	formatter := NewMarkdownFormatter(true, true)

	tests := []struct {
		name      string
		input     string
		hasItalic bool
	}{
		{
			name:      "standalone italic underscore",
			input:     "hello _world_ goodbye",
			hasItalic: true,
		},
		{
			name:      "identifier with underscores is NOT italic",
			input:     "handle_read_file",
			hasItalic: false,
		},
		{
			name:      "multiple underscores in identifier",
			input:     "some_long_function_name",
			hasItalic: false,
		},
		{
			name:      "underscore at end of identifier followed by space-underscore",
			input:     "func_name _is italic_",
			hasItalic: true,
		},
		{
			name:      "underscore at start then identifier",
			input:     "_italic_ then some_func",
			hasItalic: true,
		},
		{
			name:      "single underscore not followed by closing underscore",
			input:     "it's a contraction test_here",
			hasItalic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.Format(tt.input)
			if tt.hasItalic {
				if !strings.Contains(result, ColorItalic) {
					t.Errorf("Expected italic in %q but got none; output: %q", tt.input, result)
				}
			} else {
				if strings.Contains(result, ColorItalic) {
					t.Errorf("Did not expect italic in %q but got it; output: %q", tt.input, result)
				}
			}
		})
	}
}
