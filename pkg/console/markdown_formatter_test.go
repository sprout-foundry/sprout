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
			name:  "Code block",
			input: "```go\nfunc main() {\n  println(\"hello\")\n}\n```",
			contains: []string{
				ColorDim + ColorBold,
				"┌─ Code Block",
				"│ Language: go",
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
	
	// Test markdown is processed
	markdown := "# Test\n**bold** text"
	_, err := handler.Write([]byte(markdown))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	
	// Should have a markdown formatter
	if handler.markdownFormatter == nil {
		t.Error("Expected markdown formatter to be initialized")
	}
}