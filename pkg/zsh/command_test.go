package zsh

import (
	"fmt"
	"os"
	"testing"
)

func TestExtractFirstWord(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"git status", "git"},
		{"ls -la", "ls"},
		{"cd /home/user", "cd"},
		{"echo hello world", "echo"},
		{"git", "git"},
		{"", ""},
		{"   git status", "git"}, // leading spaces
		{"git", "git"},
		{"'git' status", "git"}, // single quotes
		{"\"git\" status", "git"}, // double quotes
		{"npm install", "npm"},
		{"docker build -t test .", "docker"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := extractFirstWord(tt.input)
			if result != tt.expected {
				t.Errorf("extractFirstWord(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsZshShell(t *testing.T) {
	// Save original SHELL
	originalShell := os.Getenv("SHELL")
	defer os.Setenv("SHELL", originalShell)

	// Test with zsh
	os.Setenv("SHELL", "/bin/zsh")
	if !isZshShell() {
		t.Error("isZshShell() returned false for /bin/zsh")
	}

	// Test with bash
	os.Setenv("SHELL", "/bin/bash")
	if isZshShell() {
		t.Error("isZshShell() returned true for /bin/bash")
	}

	// Test with empty
	os.Unsetenv("SHELL")
	if isZshShell() {
		t.Error("isZshShell() returned true for empty SHELL")
	}
}

func TestQueryZshCommand(t *testing.T) {
	if !isZshShell() {
		t.Skip("Skipping test: not running in zsh")
	}

	tests := []struct {
		name          string
		cmdName       string
		shouldBeFound bool
		expectedType  CommandType
	}{
		{"external command", "git", true, CommandTypeExternal},
		{"external command ls", "ls", true, CommandTypeExternal},
		{"builtin", "echo", true, CommandTypeBuiltin},
		{"builtin cd", "cd", true, CommandTypeBuiltin},
		{"nonexistent", "nonexistentcommand12345", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := queryZshCommand(tt.cmdName)
			if err != nil {
				t.Fatalf("queryZshCommand(%q) error: %v", tt.cmdName, err)
			}

			if tt.shouldBeFound {
				if info == nil {
					t.Errorf("queryZshCommand(%q) returned nil, expected command info", tt.cmdName)
					return
				}
				// Note: Some commands (echo) can be both builtin and external
				// We accept either type if the expected type is builtin
				if tt.expectedType == CommandTypeBuiltin && info.Type == CommandTypeExternal {
					// This is acceptable - both exist
				} else if info.Type != tt.expectedType {
					t.Errorf("queryZshCommand(%q).Type = %q, want %q", tt.cmdName, info.Type, tt.expectedType)
				}
			} else {
				if info != nil {
					t.Errorf("queryZshCommand(%q) returned info, expected nil", tt.cmdName)
				}
			}
		})
	}
}

func TestIsCommand(t *testing.T) {
	if !isZshShell() {
		t.Skip("Skipping test: not running in zsh")
	}

	tests := []struct {
		input          string
		shouldBeFound  bool
		expectedName   string
		expectedType   CommandType
	}{
		{"git status", true, "git", CommandTypeExternal},
		{"ls -la", true, "ls", CommandTypeExternal},
		{"cd /home", true, "cd", CommandTypeBuiltin},
		{"echo hello", true, "echo", CommandTypeBuiltin},
		{"pwd", true, "pwd", CommandTypeBuiltin},
		{"nonexistentcommand xyz", false, "", ""},
		{"", false, "", ""},
		{"   git status", true, "git", CommandTypeExternal},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			found, info, err := IsCommand(tt.input)
			if err != nil {
				t.Fatalf("IsCommand(%q) error: %v", tt.input, err)
			}

			if tt.shouldBeFound {
				if !found {
					t.Errorf("IsCommand(%q) returned false, expected true", tt.input)
					return
				}
				if info == nil {
					t.Errorf("IsCommand(%q) returned nil info, expected non-nil", tt.input)
					return
				}
				if info.Name != tt.expectedName {
					t.Errorf("IsCommand(%q).Name = %q, want %q", tt.input, info.Name, tt.expectedName)
				}
				// Note: Some commands (echo, pwd) can be both builtin and external
				// We accept either type if the expected type is builtin
				if tt.expectedType == CommandTypeBuiltin && info.Type == CommandTypeExternal {
					// This is acceptable - both exist
				} else if info.Type != tt.expectedType {
					t.Errorf("IsCommand(%q).Type = %q, want %q", tt.input, info.Type, tt.expectedType)
				}
			} else {
				if found {
					t.Errorf("IsCommand(%q) returned true, expected false", tt.input)
				}
				if info != nil {
					t.Errorf("IsCommand(%q) returned non-nil info, expected nil", tt.input)
				}
			}
		})
	}
}

// Manual test helper
func TestMain(m *testing.M) {
	// Print test info
	fmt.Printf("SHELL=%s\n", os.Getenv("SHELL"))
	fmt.Printf("isZshShell=%v\n", isZshShell())

	// Run tests
	os.Exit(m.Run())
}
