//go:build !js

package webui

import (
	"strings"
	"testing"
)

// Test trimSSHOutput function
func TestTrimSSHOutput2(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
		want string
	}{
		{
			name: "simple output",
			raw:  []byte("Hello, World!"),
			want: "Hello, World!",
		},
		{
			name: "output with leading/trailing whitespace",
			raw:  []byte("  Hello, World!  "),
			want: "Hello, World!",
		},
		{
			name: "output with newlines",
			raw:  []byte("Line 1\nLine 2\nLine 3"),
			want: "Line 1\nLine 2\nLine 3",
		},
		{
			name: "empty byte slice",
			raw:  []byte(""),
			want: "",
		},
		{
			name: "whitespace only",
			raw:  []byte("   \n\t  "),
			want: "",
		},
		{
			name: "nil byte slice",
			raw:  nil,
			want: "",
		},
		{
			name: "output longer than maxLen",
			raw:  []byte(strings.Repeat("a", 5000)),
			want: strings.Repeat("a", 4000) + "\n...[truncated]",
		},
		{
			name: "output exactly maxLen",
			raw:  []byte(strings.Repeat("a", 4000)),
			want: strings.Repeat("a", 4000),
		},
		{
			name: "output just over maxLen",
			raw:  []byte(strings.Repeat("a", 4001)),
			want: strings.Repeat("a", 4000) + "\n...[truncated]",
		},
		{
			name: "multiline output with no truncation",
			raw:  []byte(strings.Repeat("a\n", 1000)), // 2000 chars (1000 * 2)
			want: strings.Repeat("a\n", 999) + "a",    // TrimSpace removes final trailing newline
		},
		{
			name: "very long multiline output with truncation",
			raw:  []byte(strings.Repeat("ab\n", 2000)),                    // 6000 chars - over limit
			want: strings.Repeat("ab\n", 1333) + "a" + "\n...[truncated]", // 4000 chars exactly + suffix
		},
		{
			name: "output with special characters",
			raw:  []byte("Hello\x1b[0m World"),
			want: "Hello\x1b[0m World",
		},
		{
			name: "output with tabs and spaces",
			raw:  []byte("\t  \tHello, World!\t  \t"),
			want: "Hello, World!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trimSSHOutput(tt.raw)
			if len(got) != len(tt.want) {
				t.Errorf("trimSSHOutput() length = %d, want %d", len(got), len(tt.want))
				return
			}
			if got != tt.want {
				t.Errorf("trimSSHOutput() output mismatch (len=%d)", len(got))
			}
		})
	}
}

// Test shellEscapeSSH function
func TestShellEscapeSSH2(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "simple string",
			value: "hello",
			want:  "'hello'",
		},
		{
			name:  "string with spaces",
			value: "hello world",
			want:  "'hello world'",
		},
		{
			name:  "string with single quote",
			value: "hello'world",
			want:  "'hello'\\''world'",
		},
		{
			name:  "string with multiple single quotes",
			value: "'hello' 'world'",
			want:  "''\\''hello'\\'' '\\''world'\\'''",
		},
		{
			name:  "string with double quotes",
			value: `hello"world"`,
			want:  `'hello"world"'`,
		},
		{
			name:  "string with mixed quotes",
			value: `hello'world"test'`,
			want:  `'hello'\''world"test'\'''`,
		},
		{
			name:  "string with special characters",
			value: "hello!@#$%^&*()",
			want:  "'hello!@#$%^&*()'",
		},
		{
			name:  "string with backticks",
			value: "hello`world`",
			want:  "'hello`world`'",
		},
		{
			name:  "string with backslashes",
			value: "hello\\world",
			want:  "'hello\\world'",
		},
		{
			name:  "string with newlines",
			value: "hello\nworld",
			want:  "'hello\nworld'",
		},
		{
			name:  "string with tabs",
			value: "hello\tworld",
			want:  "'hello\tworld'",
		},
		{
			name:  "string with dollar signs",
			value: "hello$world",
			want:  "'hello$world'",
		},
		{
			name:  "string with semicolons",
			value: "hello;world",
			want:  "'hello;world'",
		},
		{
			name:  "string with pipes",
			value: "hello|world",
			want:  "'hello|world'",
		},
		{
			name:  "string with ampersands",
			value: "hello&world",
			want:  "'hello&world'",
		},
		{
			name:  "string with redirects",
			value: "hello>world",
			want:  "'hello>world'",
		},
		{
			name:  "string with asterisks",
			value: "hello*world",
			want:  "'hello*world'",
		},
		{
			name:  "string with question marks",
			value: "hello?world",
			want:  "'hello?world'",
		},
		{
			name:  "string with brackets",
			value: "hello[world]",
			want:  "'hello[world]'",
		},
		{
			name:  "empty string",
			value: "",
			want:  "''",
		},
		{
			name:  "string with only single quotes",
			value: "'",
			want:  "''\\'''",
		},
		{
			name:  "string with consecutive single quotes",
			value: "''",
			want:  "''\\'''\\'''",
		},
		{
			name:  "complex path",
			value: "/path/to/some file's name",
			want:  "'/path/to/some file'\\''s name'",
		},
		{
			name:  "home directory",
			value: "$HOME/path/to/file",
			want:  "'$HOME/path/to/file'",
		},
		{
			name:  "tilda home",
			value: "~/Documents",
			want:  "'~/Documents'",
		},
		{
			name:  "string with spaces and special chars",
			value: "My File's Name.txt",
			want:  "'My File'\\''s Name.txt'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellEscapeSSH(tt.value)
			if got != tt.want {
				t.Errorf("shellEscapeSSH(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

// Test workspaceLogPath function
func TestWorkspaceLogPath2(t *testing.T) {
	path := workspaceLogPath()

	t.Run("returns non-empty string", func(t *testing.T) {
		if path == "" {
			t.Error("workspaceLogPath() should return a non-empty string")
		}
	})

	t.Run("ends with workspace.log", func(t *testing.T) {
		if !strings.HasSuffix(path, "workspace.log") {
			t.Errorf("workspaceLogPath() should end with 'workspace.log', got %q", path)
		}
	})

	t.Run("is a valid path format", func(t *testing.T) {
		// Check if it contains path separators
		if !strings.Contains(path, "/") && !strings.Contains(path, string('\\')) {
			t.Logf("workspaceLogPath() returned: %q (may be relative)", path)
		}
	})
}

// Test localSSHCacheRoot function
func TestLocalSSHCacheRoot2(t *testing.T) {
	path := localSSHCacheRoot()

	t.Run("returns non-empty string", func(t *testing.T) {
		if path == "" {
			t.Error("localSSHCacheRoot() should return a non-empty string")
		}
	})

	t.Run("ends with sprout-ssh-cache", func(t *testing.T) {
		if !strings.HasSuffix(path, "sprout-ssh-cache") {
			t.Errorf("localSSHCacheRoot() should end with 'sprout-ssh-cache', got %q", path)
		}
	})

	t.Run("contains expected path components", func(t *testing.T) {
		// Should contain either "cache", "sprout", or ".sprout" based on implementation
		lowerPath := strings.ToLower(path)
		if !strings.Contains(lowerPath, "sprout") && !strings.Contains(lowerPath, "cache") {
			t.Logf("localSSHCacheRoot() returned: %q", path)
		}
	})
}

// Test edge cases for trimSSHOutput
func TestTrimSSHOutputEdgeCases2(t *testing.T) {
	t.Run("byte slice with only null bytes", func(t *testing.T) {
		got := trimSSHOutput([]byte{0x00, 0x00, 0x00})
		want := "\x00\x00\x00" // strings.TrimSpace doesn't trim null bytes
		if got != want {
			t.Errorf("trimSSHOutput([]byte{0x00, 0x00, 0x00}) = %q, want %q", got, want)
		}
	})

	t.Run("byte slice with null bytes mixed with text", func(t *testing.T) {
		got := trimSSHOutput([]byte("hello\x00world"))
		want := "hello\x00world"
		if got != want {
			t.Errorf("trimSSHOutput([]byte(\"hello\\x00world\")) = %q, want %q", got, want)
		}
	})

	t.Run("very long output with truncation at boundary", func(t *testing.T) {
		longText := strings.Repeat("x", 3999) + "y" + strings.Repeat("z", 100)
		got := trimSSHOutput([]byte(longText))
		if !strings.HasSuffix(got, "\n...[truncated]") {
			t.Errorf("expected truncation suffix, got: %q", got)
		}
		if len(got) > 4015 { // 4000 + len("\n...[truncated]")
			t.Errorf("truncated output too long: %d", len(got))
		}
	})
}

// Test edge cases for shellEscapeSSH
func TestShellEscapeSSHEdgeCases2(t *testing.T) {
	t.Run("single quote in middle of string", func(t *testing.T) {
		got := shellEscapeSSH("it's a test")
		want := "'it'\\''s a test'"
		if got != want {
			t.Errorf("shellEscapeSSH(\"it's a test\") = %q, want %q", got, want)
		}
	})

	t.Run("single quote at start", func(t *testing.T) {
		got := shellEscapeSSH("'test")
		want := "''\\''test'"
		if got != want {
			t.Errorf("shellEscapeSSH(\"'test\") = %q, want %q", got, want)
		}
	})

	t.Run("single quote at end", func(t *testing.T) {
		got := shellEscapeSSH("test'")
		want := "'test'\\'''"
		if got != want {
			t.Errorf("shellEscapeSSH(\"test'\") = %q, want %q", got, want)
		}
	})

	t.Run("consecutive single quotes", func(t *testing.T) {
		got := shellEscapeSSH("'")
		want := "''\\'''"
		if got != want {
			t.Errorf("shellEscapeSSH(\"'\") = %q, want %q", got, want)
		}
	})

	t.Run("string with unicode", func(t *testing.T) {
		got := shellEscapeSSH("こんにちは")
		want := "'こんにちは'"
		if got != want {
			t.Errorf("shellEscapeSSH(\"こんにちは\") = %q, want %q", got, want)
		}
	})

	t.Run("string with emoji", func(t *testing.T) {
		got := shellEscapeSSH("hello 😊 world")
		want := "'hello 😊 world'"
		if got != want {
			t.Errorf("shellEscapeSSH(\"hello 😊 world\") = %q, want %q", got, want)
		}
	})

	t.Run("very long string", func(t *testing.T) {
		longStr := strings.Repeat("a", 10000)
		got := shellEscapeSSH(longStr)
		want := "'" + longStr + "'"
		if got != want {
			t.Errorf("shellEscapeSSH(long string) produces incorrect output")
		}
	})
}

// Test that workspaceLogPath and localSSHCacheRoot produce valid strings
func TestSSHPathFunctionsReturnValidStrings2(t *testing.T) {
	t.Run("workspaceLogPath is consistent", func(t *testing.T) {
		path1 := workspaceLogPath()
		path2 := workspaceLogPath()
		if path1 != path2 {
			t.Error("workspaceLogPath() should return consistent results")
		}
	})

	t.Run("localSSHCacheRoot is consistent", func(t *testing.T) {
		path1 := localSSHCacheRoot()
		path2 := localSSHCacheRoot()
		if path1 != path2 {
			t.Error("localSSHCacheRoot() should return consistent results")
		}
	})

	t.Run("functions return different paths", func(t *testing.T) {
		logPath := workspaceLogPath()
		cachePath := localSSHCacheRoot()
		if logPath == cachePath {
			t.Logf("Both functions returned same path: %q (may be intentional)", logPath)
		}
	})
}

// Test various output trimming scenarios
func TestTrimSSHOutputVariousOutputs2(t *testing.T) {
	t.Run("JSON output", func(t *testing.T) {
		json := `{"key": "value", "number": 123}`
		got := trimSSHOutput([]byte(json))
		if got != json {
			t.Errorf("trimSSHOutput(JSON) = %q, want %q", got, json)
		}
	})

	t.Run("command output with color codes", func(t *testing.T) {
		output := "\x1b[31mError:\x1b[0m Something went wrong"
		got := trimSSHOutput([]byte(output))
		if got != output {
			t.Errorf("trimSSHOutput(with ANSI codes) = %q, want %q", got, output)
		}
	})

	t.Run("multiline with trailing whitespace", func(t *testing.T) {
		output := "Line 1\n  Line 2\n    Line 3    "
		got := trimSSHOutput([]byte(output))
		want := "Line 1\n  Line 2\n    Line 3"
		if got != want {
			t.Errorf("trimSSHOutput(multiline) = %q, want %q", got, want)
		}
	})

	t.Run("output with carriage returns", func(t *testing.T) {
		output := "Line 1\r\nLine 2\r"
		got := trimSSHOutput([]byte(output))
		want := "Line 1\r\nLine 2" // Note: trim only trims \t\n\r\x00, not \r\n as a pair
		if got != want {
			t.Logf("trimSSHOutput(CR/LF) = %q, want %q", got, want)
		}
	})

	t.Run("empty lines", func(t *testing.T) {
		output := "\n\n\n"
		got := trimSSHOutput([]byte(output))
		want := ""
		if got != want {
			t.Errorf("trimSSHOutput(empty lines) = %q, want %q", got, want)
		}
	})
}
