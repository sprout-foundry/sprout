package commands

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContains(t *testing.T) {
	tests := []struct {
		name  string
		slice []string
		item  string
		want  bool
	}{
		{
			name:  "item present",
			slice: []string{"apple", "banana", "cherry"},
			item:  "banana",
			want:  true,
		},
		{
			name:  "item absent",
			slice: []string{"apple", "banana", "cherry"},
			item:  "durian",
			want:  false,
		},
		{
			name:  "empty slice",
			slice: []string{},
			item:  "apple",
			want:  false,
		},
		{
			name:  "nil slice",
			slice: nil,
			item:  "apple",
			want:  false,
		},
		{
			name:  "item at start",
			slice: []string{"apple", "banana", "cherry"},
			item:  "apple",
			want:  true,
		},
		{
			name:  "item at end",
			slice: []string{"apple", "banana", "cherry"},
			item:  "cherry",
			want:  true,
		},
		{
			name:  "duplicate item",
			slice: []string{"apple", "apple", "banana"},
			item:  "apple",
			want:  true,
		},
		{
			name:  "empty string item",
			slice: []string{"apple", "", "banana"},
			item:  "",
			want:  true,
		},
		{
			name:  "case sensitive match",
			slice: []string{"Apple", "Banana"},
			item:  "apple",
			want:  false,
		},
		{
			name:  "special characters",
			slice: []string{"--flag", "-f", "output"},
			item:  "--flag",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contains(tt.slice, tt.item)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFilterArgs(t *testing.T) {
	tests := []struct {
		name  string
		slice []string
		item  string
		want  []string
	}{
		{
			name:  "filter one item",
			slice: []string{"apple", "banana", "cherry"},
			item:  "banana",
			want:  []string{"apple", "cherry"},
		},
		{
			name:  "filter item not present",
			slice: []string{"apple", "banana", "cherry"},
			item:  "durian",
			want:  []string{"apple", "banana", "cherry"},
		},
		{
			name:  "empty slice",
			slice: []string{},
			item:  "banana",
			want:  []string{},
		},
		{
			name:  "nil slice",
			slice: nil,
			item:  "banana",
			want:  []string{},
		},
		{
			name:  "filter all items",
			slice: []string{"banana", "banana", "banana"},
			item:  "banana",
			want:  []string{},
		},
		{
			name:  "filter duplicates",
			slice: []string{"apple", "banana", "apple", "cherry"},
			item:  "apple",
			want:  []string{"banana", "cherry"},
		},
		{
			name:  "filter empty string",
			slice: []string{"", "apple", "", "banana"},
			item:  "",
			want:  []string{"apple", "banana"},
		},
		{
			name:  "filter flag",
			slice: []string{"--json", "--verbose", "output"},
			item:  "--json",
			want:  []string{"--verbose", "output"},
		},
		{
			name:  "single item filtered",
			slice: []string{"banana"},
			item:  "banana",
			want:  []string{},
		},
		{
			name:  "single item kept",
			slice: []string{"apple"},
			item:  "banana",
			want:  []string{"apple"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterArgs(tt.slice, tt.item)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsLikelySlashCommandName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		// Valid names
		{
			name:  "simple lowercase",
			input: "help",
			want:  true,
		},
		{
			name:  "simple uppercase",
			input: "HELP",
			want:  true,
		},
		{
			name:  "mixed case",
			input: "HeLp",
			want:  true,
		},
		{
			name:  "with numbers",
			input: "model3",
			want:  true,
		},
		{
			name:  "with hyphen",
			input: "git-commit",
			want:  true,
		},
		{
			name:  "with underscore",
			input: "git_commit",
			want:  true,
		},
		{
			name:  "with all three",
			input: "git_commit-v2",
			want:  true,
		},
		{
			name:  "single letter",
			input: "a",
			want:  true,
		},
		{
			name:  "single number",
			input: "1",
			want:  true,
		},
		// Invalid names
		{
			name:  "with space",
			input: "git commit",
			want:  false,
		},
		{
			name:  "with slash",
			input: "git/commit",
			want:  false,
		},
		{
			name:  "with backslash",
			input: "git\\commit",
			want:  false,
		},
		{
			name:  "with pipe",
			input: "git|commit",
			want:  false,
		},
		{
			name:  "with semicolon",
			input: "git;commit",
			want:  false,
		},
		{
			name:  "with comma",
			input: "git,commit",
			want:  false,
		},
		{
			name:  "with period",
			input: "git.commit",
			want:  false,
		},
		{
			name:  "with exclamation",
			input: "git!",
			want:  false,
		},
		{
			name:  "with question",
			input: "git?",
			want:  false,
		},
		{
			name:  "with at",
			input: "git@",
			want:  false,
		},
		{
			name:  "with hash",
			input: "git#",
			want:  false,
		},
		{
			name:  "with dollar",
			input: "git$",
			want:  false,
		},
		{
			name:  "with percent",
			input: "git%",
			want:  false,
		},
		{
			name:  "with ampersand",
			input: "git&",
			want:  false,
		},
		{
			name:  "with asterisk",
			input: "git*",
			want:  false,
		},
		{
			name:  "with parentheses",
			input: "git()",
			want:  false,
		},
		{
			name:  "with brackets",
			input: "git[]",
			want:  false,
		},
		{
			name:  "with braces",
			input: "git{}",
			want:  false,
		},
		{
			name:  "with angle brackets",
			input: "git<>",
			want:  false,
		},
		// Edge cases
		{
			name:  "empty string",
			input: "",
			want:  true, // Loop doesn't execute, returns true by default
		},
		{
			name:  "only hyphen",
			input: "-",
			want:  true, // Single hyphen is valid
		},
		{
			name:  "only underscore",
			input: "_",
			want:  true, // Single underscore is valid
		},
		{
			name:  "starts with number",
			input: "3dmodel",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isLikelySlashCommandName(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestOutputWriter(t *testing.T) {
	tests := []struct {
		name    string
		writes  []string
		wantStr string
	}{
		{
			name:    "single write",
			writes:  []string{"hello"},
			wantStr: "hello",
		},
		{
			name:    "multiple writes",
			writes:  []string{"hello", " ", "world"},
			wantStr: "hello world",
		},
		{
			name:    "writes with newlines",
			writes:  []string{"line1\n", "line2\n"},
			wantStr: "line1\nline2\n",
		},
		{
			name:    "empty write",
			writes:  []string{""},
			wantStr: "",
		},
		{
			name:    "write bytes",
			writes:  []string{"hello", "\n", "world"},
			wantStr: "hello\nworld",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ow := &OutputWriter{}

			// Perform writes
			for _, w := range tt.writes {
				n, err := ow.Write([]byte(w))
				assert.NoError(t, err)
				assert.Equal(t, len(w), n)
			}

			// Check the string representation
			got := ow.String()
			assert.Equal(t, tt.wantStr, got)
		})
	}
}

func TestOutputWriterWrite(t *testing.T) {
	t.Run("Write returns byte count", func(t *testing.T) {
		ow := &OutputWriter{}
		testStr := "hello, world!"

		n, err := ow.Write([]byte(testStr))

		assert.NoError(t, err)
		assert.Equal(t, len(testStr), n)
	})

	t.Run("Write appends to buffer", func(t *testing.T) {
		ow := &OutputWriter{}

		ow.Write([]byte("first"))
		ow.Write([]byte(" "))
		ow.Write([]byte("second"))

		assert.Equal(t, "first second", ow.String())
	})

	t.Run("Write empty byte slice", func(t *testing.T) {
		ow := &OutputWriter{}

		n, err := ow.Write([]byte{})

		assert.NoError(t, err)
		assert.Equal(t, 0, n)
		assert.Equal(t, "", ow.String())
	})

	t.Run("Write to already initialized buffer", func(t *testing.T) {
		ow := &OutputWriter{Buffer: bytes.Buffer{}}
		ow.Buffer.WriteString("existing")

		ow.Write([]byte(" appended"))

		assert.Equal(t, "existing appended", ow.String())
	})
}

func TestOutputWriterString(t *testing.T) {
	t.Run("String returns buffer content", func(t *testing.T) {
		ow := &OutputWriter{}
		ow.Buffer.WriteString("test content")

		got := ow.String()
		assert.Equal(t, "test content", got)
	})

	t.Run("String on empty buffer", func(t *testing.T) {
		ow := &OutputWriter{}

		got := ow.String()
		assert.Equal(t, "", got)
	})

	t.Run("String returns copy, doesn't affect buffer", func(t *testing.T) {
		ow := &OutputWriter{}
		ow.Buffer.WriteString("original")

		str := ow.String()
		assert.Equal(t, "original", str)

		// Modify the returned string shouldn't affect buffer
		ow.Buffer.WriteString(" added")
		assert.Equal(t, "original", str)
		assert.Equal(t, "original added", ow.String())
	})
}

func TestCommandRegistryGetCommand(t *testing.T) {
	registry := NewCommandRegistry()

	tests := []struct {
		name        string
		cmdName     string
		wantCmdName string
		wantFound   bool
	}{
		{
			name:        "existing command help",
			cmdName:     "help",
			wantCmdName: "help",
			wantFound:   true,
		},
		{
			name:        "existing command model",
			cmdName:     "model",
			wantCmdName: "model",
			wantFound:   true,
		},
		{
			name:        "existing command provider",
			cmdName:     "provider",
			wantCmdName: "provider",
			wantFound:   true,
		},
		{
			name:        "existing command exec",
			cmdName:     "exec",
			wantCmdName: "exec",
			wantFound:   true,
		},
		{
			name:        "existing command commit",
			cmdName:     "commit",
			wantCmdName: "commit",
			wantFound:   true,
		},
		{
			name:        "non-existing command",
			cmdName:     "notreal",
			wantCmdName: "",
			wantFound:   false,
		},
		{
			name:        "empty command name",
			cmdName:     "",
			wantCmdName: "",
			wantFound:   false,
		},
		{
			name:        "command with invalid chars",
			cmdName:     "invalid/command",
			wantCmdName: "",
			wantFound:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, found := registry.GetCommand(tt.cmdName)
			if tt.wantFound {
				assert.True(t, found)
				assert.NotNil(t, cmd)
				assert.Equal(t, tt.wantCmdName, cmd.Name())
			} else {
				assert.False(t, found)
				assert.Nil(t, cmd)
			}
		})
	}
}

func TestCommandRegistryListCommands(t *testing.T) {
	registry := NewCommandRegistry()

	// Get all commands
	commands := registry.ListCommands()

	// Verify we got commands
	assert.NotNil(t, commands)
	assert.Greater(t, len(commands), 0)

	// Verify all commands are valid
	for _, cmd := range commands {
		assert.NotNil(t, cmd)
		name := cmd.Name()
		assert.NotEmpty(t, name)
		assert.NotEmpty(t, cmd.Description())
	}

	// Check for some expected commands
	commandNames := make(map[string]bool)
	for _, cmd := range commands {
		commandNames[cmd.Name()] = true
	}

	// Verify some expected commands exist
	expectedCommands := []string{
		"help", "model", "provider", "sessions", "clear",
		"exec", "shell", "commit", "changes", "status",
	}

	for _, expected := range expectedCommands {
		assert.True(t, commandNames[expected], "expected command %q to be in list", expected)
	}
}

func TestCommandRegistryListCommandsConsistency(t *testing.T) {
	registry := NewCommandRegistry()

	// List commands multiple times to ensure consistency
	commands1 := registry.ListCommands()
	commands2 := registry.ListCommands()

	assert.Equal(t, len(commands1), len(commands2), "command count should be consistent")

	// Create maps for comparison
	cmdMap1 := make(map[string]Command)
	for _, cmd := range commands1 {
		cmdMap1[cmd.Name()] = cmd
	}

	cmdMap2 := make(map[string]Command)
	for _, cmd := range commands2 {
		cmdMap2[cmd.Name()] = cmd
	}

	// Check that the same commands are present
	for name := range cmdMap1 {
		_, exists := cmdMap2[name]
		assert.True(t, exists, "command %q should be present in both lists", name)
	}

	for name := range cmdMap2 {
		_, exists := cmdMap1[name]
		assert.True(t, exists, "command %q should be present in both lists", name)
	}
}
