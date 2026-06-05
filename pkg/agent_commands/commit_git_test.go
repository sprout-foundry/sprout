package commands

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeNewlines(t *testing.T) {
	// In tests, stdout is NOT a terminal, so normalizeNewlines returns string unchanged
	tests := []struct {
		name string
		s    string
		want string
	}{
		{
			name: "string with LF newlines",
			s:    "line1\nline2\nline3",
			want: "line1\nline2\nline3",
		},
		{
			name: "string with CRLF newlines",
			s:    "line1\r\nline2\r\nline3",
			want: "line1\r\nline2\r\nline3",
		},
		{
			name: "empty string",
			s:    "",
			want: "",
		},
		{
			name: "single newline",
			s:    "\n",
			want: "\n",
		},
		{
			name: "single line no newline",
			s:    "line1",
			want: "line1",
		},
		{
			name: "mixed line endings",
			s:    "line1\nline2\r\nline3",
			want: "line1\nline2\r\nline3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeNewlines(tt.s)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseFilenameFromStatusLine(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantFile  string
		wantFound bool
	}{
		{
			name:      "modified file",
			line:      "M main.go",
			wantFile:  "main.go",
			wantFound: true,
		},
		{
			name:      "added file",
			line:      "A newfile.go",
			wantFile:  "newfile.go",
			wantFound: true,
		},
		{
			name:      "deleted file",
			line:      "D oldfile.go",
			wantFile:  "oldfile.go",
			wantFound: true,
		},
		{
			name:      "renamed file",
			line:      "R  oldfile.go -> newfile.go",
			wantFile:  "oldfile.go -> newfile.go",
			wantFound: true,
		},
		{
			name:      "copied file",
			line:      "C  source.go -> copy.go",
			wantFile:  "source.go -> copy.go",
			wantFound: true,
		},
		{
			name:      "untracked file",
			line:      "?? untracked.txt",
			wantFile:  "untracked.txt",
			wantFound: true,
		},
		{
			name:      "staged and modified",
			line:      "MM file.go",
			wantFile:  "file.go",
			wantFound: true,
		},
		{
			name:      "empty string",
			line:      "",
			wantFile:  "",
			wantFound: false,
		},
		{
			name:      "only status code",
			line:      "M",
			wantFile:  "",
			wantFound: false,
		},
		{
			name:      "file with spaces",
			line:      "M file with spaces.go",
			wantFile:  "file with spaces.go",
			wantFound: true,
		},
		{
			name:      "file with path",
			line:      "M pkg/utils/file.go",
			wantFile:  "pkg/utils/file.go",
			wantFound: true,
		},
		{
			name:      "whitespace only",
			line:      "   ",
			wantFile:  "",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFile, gotFound := parseFilenameFromStatusLine(tt.line)
			assert.Equal(t, tt.wantFile, gotFile)
			assert.Equal(t, tt.wantFound, gotFound)
		})
	}
}

func TestSelectAllModifiedFiles(t *testing.T) {
	tests := []struct {
		name               string
		validStatusLines   []string
		wantFiles          []string
	}{
		{
			name:               "empty list",
			validStatusLines:   []string{},
			wantFiles:          nil,
		},
		{
			name: "single file",
			validStatusLines:   []string{"M main.go"},
			wantFiles:          []string{"main.go"},
		},
		{
			name: "multiple files",
			validStatusLines:   []string{"M main.go", "A newfile.go", "D oldfile.go"},
			wantFiles:          []string{"main.go", "newfile.go", "oldfile.go"},
		},
		{
			name: "files with spaces in names",
			validStatusLines:   []string{"M file with spaces.go", "A another file.txt"},
			wantFiles:          []string{"file with spaces.go", "another file.txt"},
		},
		{
			name: "files with paths",
			validStatusLines:   []string{"M pkg/utils/file.go", "A cmd/main.go"},
			wantFiles:          []string{"pkg/utils/file.go", "cmd/main.go"},
		},
		{
			name: "mixed status codes",
			validStatusLines:   []string{"MM file.go", "A  new.txt", "D  old.txt", "?? untracked.go"},
			wantFiles:          []string{"file.go", "new.txt", "old.txt", "untracked.go"},
		},
		{
			name: "renamed files",
			validStatusLines:   []string{"R  old.go -> new.go"},
			wantFiles:          []string{"old.go -> new.go"},
		},
		{
			name: "many files",
			validStatusLines: []string{
				"M main.go",
				"A config.yaml",
				"D unused.go",
				"MM utils.go",
				"A  test.go",
				"?? new.txt",
			},
			wantFiles: []string{"main.go", "config.yaml", "unused.go", "utils.go", "test.go", "new.txt"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFiles := selectAllModifiedFiles(tt.validStatusLines)
			assert.Equal(t, tt.wantFiles, gotFiles)
		})
	}
}

func TestSetGitDirAndGitCommand(t *testing.T) {
	// Save original state
	originalDir := currentDir
	defer func() {
		currentDir = originalDir
	}()

	tests := []struct {
		name      string
		setDir    string
		wantEmpty bool
	}{
		{
			name:      "set empty directory",
			setDir:    "",
			wantEmpty: true,
		},
		{
			name:      "set directory with spaces",
			setDir:    "/path/to/repo",
			wantEmpty: false,
		},
		{
			name:      "set directory with trailing space",
			setDir:    "/path/to/repo   ",
			wantEmpty: false, // Should be trimmed
		},
		{
			name:      "set relative path",
			setDir:    "../other-repo",
			wantEmpty: false,
		},
		{
			name:      "set current directory",
			setDir:    ".",
			wantEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset to empty first
			currentDir = ""

			// Set the directory
			SetGitDir(tt.setDir)

			// Verify that currentDir was set (trimmed)
			if tt.wantEmpty {
				assert.Empty(t, currentDir, "currentDir should be empty")
			} else {
				assert.Equal(t, strings.TrimSpace(tt.setDir), currentDir, "currentDir should match (trimmed)")
			}

			// Verify that gitCommand respects the directory setting
			cmd := gitCommand("status")
			if tt.wantEmpty {
				assert.Empty(t, cmd.Dir, "git command Dir should be empty")
			} else {
				assert.Equal(t, strings.TrimSpace(tt.setDir), cmd.Dir, "git command Dir should match")
			}

			// Verify that command is properly structured
			assert.Contains(t, cmd.Path, "git", "command path should be git")
			assert.Equal(t, []string{"git", "status"}, cmd.Args, "command args should match")
		})
	}
}

func TestGitCommandBasic(t *testing.T) {
	// Save and reset currentDir
	originalDir := currentDir
	defer func() {
		currentDir = originalDir
	}()
	currentDir = ""

	tests := []struct {
		name string
		args []string
	}{
		{
			name: "status command",
			args: []string{"status"},
		},
		{
			name: "diff command with args",
			args: []string{"diff", "--staged"},
		},
		{
			name: "log command with args",
			args: []string{"log", "--oneline", "-10"},
		},
		{
			// Was {"commit", "-m", "test message"} — but the in-test
			// gitCommand defense (commit_git_safety_test.go) now
			// substitutes a blocked sentinel for mutating subcommands
			// when currentDir is empty. The test only verifies arg
			// wrapping, so any multi-arg read-only subcommand works.
			name: "multiple args",
			args: []string{"log", "-n", "3", "--oneline"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := gitCommand(tt.args...)

			// Verify command structure
			assert.Contains(t, cmd.Path, "git", "Path should contain git")
			assert.NotNil(t, cmd.Args)

			// Args should have "git" as first element
			expectedArgs := append([]string{"git"}, tt.args...)
			assert.Equal(t, expectedArgs, cmd.Args)

			// Dir should be empty since we reset currentDir
			assert.Empty(t, cmd.Dir)
		})
	}
}

func TestGitCommandRespectsCurrentDir(t *testing.T) {
	// Save and reset
	originalDir := currentDir
	defer func() {
		currentDir = originalDir
	}()

	tests := []struct {
		name   string
		setDir string
	}{
		{
			name:   "absolute path",
			setDir: "/tmp/test-repo",
		},
		{
			name:   "relative path",
			setDir: "../test",
		},
		{
			name:   "dot path",
			setDir: ".",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set the directory
			SetGitDir(tt.setDir)
			defer SetGitDir("") // Reset after each test

			// Create a git command
			cmd := gitCommand("status")

			// Verify the directory is set
			assert.Equal(t, tt.setDir, cmd.Dir)

			// Verify other properties
			assert.Contains(t, cmd.Path, "git", "Path should contain git")
			assert.Equal(t, []string{"git", "status"}, cmd.Args)
		})
	}
}

// Test helper: verify gitCommand creates a proper exec.Cmd
func TestGitCommandType(t *testing.T) {
	cmd := gitCommand("status")

	// Verify it's correct type
	assert.IsType(t, &exec.Cmd{}, cmd, "gitCommand should return *exec.Cmd")

	// Verify we can actually check if command exists (but don't run it)
	_, err := exec.LookPath("git")
	// git should be available in the test environment
	if err != nil {
		t.Logf("Warning: git not found in PATH: %v", err)
	} else {
		assert.Contains(t, cmd.Path, "git", "Path should contain git")
	}
}
