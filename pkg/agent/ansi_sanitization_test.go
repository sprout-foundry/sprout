package agent

import (
	"strings"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

func TestSanitizeContent(t *testing.T) {
	// Create a minimal agent to avoid complex initialization
	agent := &Agent{
		debug: false,
	}
	ch := &ConversationHandler{
		agent: agent,
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "No ANSI codes",
			input:    "Hello world",
			expected: "Hello world",
		},
		{
			name:     "Simple color code",
			input:    "\x1b[31mRed text\x1b[0m",
			expected: "Red text",
		},
		{
			name:     "Multiple ANSI codes",
			input:    "\x1b[1m\x1b[32mBold green\x1b[0m",
			expected: "Bold green",
		},
		{
			name:     "Mixed content",
			input:    "Normal \x1b[33myellow\x1b[0m text",
			expected: "Normal yellow text",
		},
		{
			name:     "Think tag with ANSI",
			input:    "\x1b[36m💭\x1b[0m \x1b[2mthinking...\x1b[0m",
			expected: "💭 thinking...",
		},
		{
			name:     "Complex ANSI sequences",
			input:    "\x1b[38;5;208mOrange text\x1b[0m",
			expected: "Orange text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ch.sanitizeContent(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeContent() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

func TestStreamingContentSanitization(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Clean content",
			input:    "Hello world",
			expected: "Hello world",
		},
		{
			name:     "Content with ANSI",
			input:    "\x1b[31mError:\x1b[0m Something went wrong",
			expected: "Error: Something went wrong",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeStreamingContent(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeStreamingContent() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

func TestZAIConversationHistory(t *testing.T) {
	// Simulate a scenario where ANSI codes leak into conversation history
	agent := &Agent{
		debug: false,
		messages: []api.Message{
			{Role: "user", Content: "Help me debug this issue"},
		},
	}

	ch := &ConversationHandler{
		agent: agent,
	}

	// Simulate streaming content with ANSI contamination
	ansiContent := "\x1b[36m✨\x1b[0m \x1b[1mHere's the solution:\x1b[0m\n\x1b[33m1.\x1b[0m Check the logs\n\x1b[33m2.\x1b[0m Fix the bug"

	// Test that sanitization removes ANSI codes
	cleanContent := ch.sanitizeContent(ansiContent)

	// Verify no ANSI codes remain
	if strings.Contains(cleanContent, "\x1b") {
		t.Errorf("Content still contains ANSI codes after sanitization: %q", cleanContent)
	}

	// Verify content structure is preserved
	expectedClean := "✨ Here's the solution:\n1. Check the logs\n2. Fix the bug"
	if cleanContent != expectedClean {
		t.Errorf("Clean content mismatch. Got: %q, Expected: %q", cleanContent, expectedClean)
	}
}
