package core

import (
	"strings"
	"testing"
)

func TestParse_ToolsBlock_CleanedContent(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{})
	// Use a valid tool_calls JSON body inside <tool> tags
	content := "A <tool>{\"tool_calls\":[{\"id\":\"1\",\"type\":\"function\",\"function\":{\"name\":\"x\",\"arguments\":\"{}\"}}]}</tool> B"
	result := fp.Parse(content)
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	// Cleaned content should NOT contain the closing ">" from </tool>
	if result.CleanedContent == "" {
		t.Fatal("expected non-empty cleaned content")
	}
	if !strings.Contains(result.CleanedContent, "A") {
		t.Errorf("expected 'A' in cleaned content, got %q", result.CleanedContent)
	}
	if !strings.Contains(result.CleanedContent, "B") {
		t.Errorf("expected 'B' in cleaned content, got %q", result.CleanedContent)
	}
	// The closing ">" from </tool> should NOT be in the output
	if strings.Contains(result.CleanedContent, ">") {
		t.Errorf("cleaned content should not contain stray '>' from </tool>, got %q", result.CleanedContent)
	}
	// Cleaned should be just "A B" (whitespace normalized)
	expected := "A B"
	if result.CleanedContent != expected {
		t.Errorf("cleaned content = %q, want %q", result.CleanedContent, expected)
	}
}

func TestParse_ToolsBlock_CleanedContent_Multiple(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{})
	content := "X <tool>{\"tool_calls\":[{\"id\":\"1\",\"type\":\"function\",\"function\":{\"name\":\"x\",\"arguments\":\"{}\"}}]}</tool>Y <tool>{\"tool_calls\":[{\"id\":\"2\",\"type\":\"function\",\"function\":{\"name\":\"y\",\"arguments\":\"{}\"}}]}</tool>Z"
	result := fp.Parse(content)
	if len(result.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(result.ToolCalls))
	}
	if !strings.Contains(result.CleanedContent, "X") ||
		!strings.Contains(result.CleanedContent, "Y") ||
		!strings.Contains(result.CleanedContent, "Z") {
		t.Errorf("expected X, Y, Z in cleaned content, got %q", result.CleanedContent)
	}
	if strings.Contains(result.CleanedContent, ">") {
		t.Errorf("cleaned content should not contain stray '>', got %q", result.CleanedContent)
	}
}

func TestParse_ToolUseBlock_CleanedContent(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{})
	content := "A <tool_use>{\"name\":\"parse\",\"input\":{\"key\":\"val\"}}</tool_use> B"
	result := fp.Parse(content)
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if result.CleanedContent == "" {
		t.Fatal("expected non-empty cleaned content")
	}
	if !strings.Contains(result.CleanedContent, "A") {
		t.Errorf("expected 'A' in cleaned content, got %q", result.CleanedContent)
	}
	if !strings.Contains(result.CleanedContent, "B") {
		t.Errorf("expected 'B' in cleaned content, got %q", result.CleanedContent)
	}
	// The closing ">" from </tool_use> should NOT be in the output
	if strings.Contains(result.CleanedContent, ">") {
		t.Errorf("cleaned content should not contain stray '>' from </tool_use>, got %q", result.CleanedContent)
	}
	// Cleaned should be just "A B" (whitespace normalized)
	expected := "A B"
	if result.CleanedContent != expected {
		t.Errorf("cleaned content = %q, want %q", result.CleanedContent, expected)
	}
}

func TestParse_ToolUseBlock_CleanedContent_Multiple(t *testing.T) {
	fp := NewFallbackParser(FallbackParserOptions{})
	content := "X <tool_use>{\"name\":\"x\",\"input\":{}}</tool_use>Y <tool_use>{\"name\":\"y\",\"input\":{}}</tool_use>Z"
	result := fp.Parse(content)
	if len(result.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(result.ToolCalls))
	}
	if !strings.Contains(result.CleanedContent, "X") ||
		!strings.Contains(result.CleanedContent, "Y") ||
		!strings.Contains(result.CleanedContent, "Z") {
		t.Errorf("expected X, Y, Z in cleaned content, got %q", result.CleanedContent)
	}
	if strings.Contains(result.CleanedContent, ">") {
		t.Errorf("cleaned content should not contain stray '>', got %q", result.CleanedContent)
	}
}
