//go:build !js

package webui

import (
	"testing"
)

func TestIsControlOnlyCommand(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		expected bool
	}{
		// Empty string returns false
		{"empty string", "", false},
		// Whitespace only returns false
		{"whitespace only", "   ", false},
		{"tab only", "\t\t", false},
		// Regular printable text returns false
		{"regular text", "echo hello", false},
		{"single char", "a", false},
		{"mixed printable and control", "a\x03b", false},
		// Pure control characters return true
		{"ctrl-C", "\x03", true},
		{"ctrl-D", "\x04", true},
		{"ctrl-A", "\x01", true},
		{"ctrl-E", "\x05", true},
		{"multiple control chars", "\x03\x04\x01", true},
		// Mixed control + printable returns false
		{"mixed ctrl-C and text", "\x03echo", false},
		{"text then ctrl-C", "echo\x03", false},
		{"text ctrl text", "a\x03b", false},
		// Whitespace around control chars (trimmed) returns true
		{"ctrl-C with surrounding spaces", "  \x03  ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isControlOnlyCommand(tt.command)
			if got != tt.expected {
				t.Errorf("isControlOnlyCommand(%q) = %v; want %v", tt.command, got, tt.expected)
			}
		})
	}
}
