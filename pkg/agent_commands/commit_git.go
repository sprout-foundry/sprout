package commands

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/console"
	"golang.org/x/term"
)

// currentDir is the working directory override for git commands.
// When non-empty, all gitCommand calls use this directory.
var currentDir string

// SetGitDir sets the working directory for subsequent gitCommand calls.
func SetGitDir(dir string) {
	currentDir = strings.TrimSpace(dir)
}

// gitCommand creates an exec.Cmd for a git command with the correct working directory.
func gitCommand(args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	if currentDir != "" {
		cmd.Dir = currentDir
	}
	return cmd
}

// normalizeNewlines converts newlines for terminal compatibility
func normalizeNewlines(s string) string {
	if term.IsTerminal(int(os.Stdout.Fd())) {
		return strings.ReplaceAll(s, "\n", "\r\n")
	}
	return s
}

// getStagedFiles returns the list of staged file paths.
func getStagedFiles() ([]string, error) {
	out, err := gitCommand("diff", "--staged", "--name-only").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to get staged files: %w", err)
	}
	raw := strings.Split(strings.TrimSpace(string(out)), "\n")
	var files []string
	for _, f := range raw {
		if t := strings.TrimSpace(f); t != "" {
			files = append(files, t)
		}
	}
	return files, nil
}

// getPorcelainStatusLines returns non-empty lines from `git status --porcelain`.
func getPorcelainStatusLines() ([]string, error) {
	out, err := gitCommand("status", "--porcelain").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to get git status: %w", err)
	}
	if len(out) == 0 {
		return nil, nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var valid []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			valid = append(valid, l)
		}
	}
	return valid, nil
}

// parseFilenameFromStatusLine extracts the filename from a porcelain status line.
func parseFilenameFromStatusLine(line string) (string, bool) {
	parts := strings.Fields(line)
	if len(parts) >= 2 {
		return strings.Join(parts[1:], " "), true
	}
	return "", false
}

// stageFiles stages a list of files and reports results.
func stageFiles(c printlnPrintfHelper, files []string) {
	c.println("\n" + console.GlyphAction.Prefix() + "Staging files...")
	for _, file := range files {
		cmd := gitCommand("add", file)
		output, err := cmd.CombinedOutput()
		if err != nil {
			c.printf("%sFailed to stage %s: %v\n", console.GlyphError.Prefix(), file, err)
			if len(output) > 0 {
				c.printf("Output: %s\n", string(output))
			}
		} else {
			c.printf("%sStaged: %s\n", console.GlyphSuccess.Prefix(), file)
		}
	}
}

// selectAllModifiedFiles converts porcelain lines to filenames.
func selectAllModifiedFiles(validStatusLines []string) []string {
	var files []string
	for _, line := range validStatusLines {
		if name, ok := parseFilenameFromStatusLine(line); ok {
			files = append(files, name)
		}
	}
	return files
}

// getStagedFiles returns the list of staged file paths.
func getGitCommitHash() (string, error) {
	output, err := gitCommand("rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get commit hash: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// getGitBranchName retrieves the current git branch name
func getGitBranchName() (string, error) {
	output, err := gitCommand("rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get branch name: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// printlnPrintfHelper is an interface for console output helpers
type printlnPrintfHelper interface {
	printf(format string, args ...interface{})
	println(text string)
}
