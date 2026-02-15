package commands

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/term"
)

// normalizeNewlines converts newlines for terminal compatibility
func normalizeNewlines(s string) string {
	if term.IsTerminal(int(os.Stdout.Fd())) {
		return strings.ReplaceAll(s, "\n", "\r\n")
	}
	return s
}

// getStagedFiles returns the list of staged file paths.
func getStagedFiles() ([]string, error) {
	out, err := exec.Command("git", "diff", "--staged", "--name-only").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to get staged files: %v", err)
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
	out, err := exec.Command("git", "status", "--porcelain").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to get git status: %v", err)
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
	c.println("\n📦 Staging files...")
	for _, file := range files {
		cmd := exec.Command("git", "add", file)
		output, err := cmd.CombinedOutput()
		if err != nil {
			c.printf("❌ Failed to stage %s: %v\n", file, err)
			if len(output) > 0 {
				c.printf("Output: %s\n", string(output))
			}
		} else {
			c.printf("✅ Staged: %s\n", file)
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
	output, err := exec.Command("git", "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get commit hash: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// getGitBranchName retrieves the current git branch name
func getGitBranchName() (string, error) {
	output, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()
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
