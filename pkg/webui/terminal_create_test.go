//go:build !js

package webui

import (
	"strings"
	"testing"
)

func TestValidateSessionID_Create(t *testing.T) {
	// Valid IDs
	validIDs := []string{
		"abc",
		"ABC",
		"123",
		"abc-123",
		"abc_123",
		"abc.123",
		"a",
		"my-session_id.test",
	}
	for _, id := range validIDs {
		if err := validateSessionID(id); err != nil {
			t.Errorf("validateSessionID(%q) should be valid, got error: %v", id, err)
		}
	}

	// Empty error
	err := validateSessionID("")
	if err == nil {
		t.Error("validateSessionID(\"\") should return error")
	} else if !strings.Contains(err.Error(), "required") {
		t.Errorf("validateSessionID(\"\") error = %q; should mention required", err.Error())
	}

	// >128 characters error
	longID := strings.Repeat("a", 129)
	err = validateSessionID(longID)
	if err == nil {
		t.Error("validateSessionID(129 chars) should return error")
	} else if !strings.Contains(err.Error(), "128") {
		t.Errorf("validateSessionID(129 chars) error = %q; should mention max 128", err.Error())
	}

	// Invalid characters error
	invalidIDs := []string{
		"abc def", // space
		"abc/def", // slash
		"abc:def", // colon
		"abc@def", // at
		"abc#def", // hash
		"abc!def", // exclamation
	}
	for _, id := range invalidIDs {
		err = validateSessionID(id)
		if err == nil {
			t.Errorf("validateSessionID(%q) should return error", id)
		}
	}

	// Boundary: exactly 128 characters should be valid
	boundaryID := strings.Repeat("a", 128)
	err = validateSessionID(boundaryID)
	if err != nil {
		t.Errorf("validateSessionID(128 chars) should be valid, got error: %v", err)
	}
}

func TestResolveShellArgs(t *testing.T) {
	tests := []struct {
		name     string
		shell    string
		expected []string
	}{
		{"bash", "bash", []string{"--login"}},
		{"zsh", "zsh", []string{"--login"}},
		{"sh", "sh", nil},
		{"fish", "fish", nil},
		{"/bin/bash", "/bin/bash", []string{"--login"}},
		{"/usr/bin/zsh", "/usr/bin/zsh", []string{"--login"}},
		{"unknown shell", "unknown-shell", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveShellArgs(tt.shell)
			if len(got) != len(tt.expected) {
				t.Errorf("resolveShellArgs(%q) = %v; want %v", tt.shell, got, tt.expected)
			} else if len(got) > 0 && got[0] != tt.expected[0] {
				t.Errorf("resolveShellArgs(%q) = %v; want %v", tt.shell, got, tt.expected)
			}
		})
	}
}

func TestWithName(t *testing.T) {
	s := &TerminalSession{}
	opt := WithName("my terminal")
	opt(s)
	if s.Name != "my terminal" {
		t.Errorf("WithName: name = %q; want %q", s.Name, "my terminal")
	}
}

func TestWithName_TrimsWhitespace(t *testing.T) {
	s := &TerminalSession{}
	opt := WithName("  my terminal  ")
	opt(s)
	if s.Name != "my terminal" {
		t.Errorf("WithName: name = %q; want %q", s.Name, "my terminal")
	}
}

func TestWithAutoClose(t *testing.T) {
	s := &TerminalSession{}
	opt := WithAutoClose(true)
	opt(s)
	if !s.AutoClose {
		t.Error("WithAutoClose(true): AutoClose should be true")
	}

	s2 := &TerminalSession{AutoClose: true}
	opt2 := WithAutoClose(false)
	opt2(s2)
	if s2.AutoClose {
		t.Error("WithAutoClose(false): AutoClose should be false")
	}
}
