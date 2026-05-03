package tools

import (
	"testing"
)

func TestIsFileDeletionCommand(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		expected bool
	}{
		// rm commands
		{"rm -rf /", "rm -rf /", true},
		{"rm file.txt", "rm file.txt", true},
		{"rm -rf file", "rm -rf file", true},
		{"rm -r file", "rm -r file", true},
		{"rm with leading whitespace", "  rm -rf file  ", true},
		{"rm with trailing whitespace", "rm file  ", true},
		{"rm case insensitive", "RM -rf /", true},
		{"RM upper", "RM file.txt", true},
		{"Rm mixed", "Rm file.txt", true},

		// git clean
		{"git clean -f", "git clean -f", true},
		{"git clean -fd", "git clean -fd", true},
		{"git clean -df (no -f substring match)", "git clean -df", false},
		{"git clean no flag", "git clean", false},
		{"git clean -n (dry run, no -f)", "git clean -n", false},
		{"GIT CLEAN -F upper", "GIT CLEAN -F", true},
		{"git clean -f with leading space", "  git clean -f", true},

		// rmdir
		{"rmdir dir", "rmdir dir", true},
		{"rmdir -p nested/dir", "rmdir -p nested/dir", true},
		{"RMDIR uppercase", "RMDIR dir", true},
		{"RmDir mixed case", "RmDir dir", true},
		{"RDIR not a command (not rmdir prefix)", "RDIR dir", false},
		{"rmdir with leading space", "  rmdir dir  ", true},

		// non-deletion commands (false negatives to avoid)
		{"echo rm file", "echo rm file", false},
		{"ls -la", "ls -la", false},
		{"cat file.txt", "cat file.txt", false},
		{"cp src dst", "cp src dst", false},
		{"mv src dst", "mv src dst", false},
		{"mkdir dir", "mkdir dir", false},
		{"echo hello", "echo hello", false},
		{"git status", "git status", false},
		{"git add .", "git add .", false},
		{"empty string", "", false},
		{"whitespace only", "   ", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsFileDeletionCommand(tt.command)
			if got != tt.expected {
				t.Errorf("IsFileDeletionCommand(%q) = %v, want %v",
					tt.command, got, tt.expected)
			}
		})
	}
}
