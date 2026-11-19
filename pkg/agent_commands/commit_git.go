package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/git"
	"golang.org/x/term"
)

// currentDir is the working directory override for git commands.
// When non-empty, all gitCommand calls use this directory.
var currentDir string

// SetGitDir sets the working directory for subsequent gitCommand calls.
func SetGitDir(dir string) {
	currentDir = strings.TrimSpace(dir)
}

// currentGitDir reports the working directory gitCommand calls run in
// (the last value passed to SetGitDir). Empty means the process CWD.
func currentGitDir() string {
	return currentDir
}

// gitCommand creates an exec.Cmd for a git command with the correct
// working directory. Delegates to pkg/git.SafeGitCmd so the test-mode
// mutation guard is shared with every other git invocation in the
// codebase — there is exactly one allowlist of mutating subcommands.
func gitCommand(args ...string) *exec.Cmd {
	return git.SafeGitCmd(currentDir, args...)
}

// mutatingGitSubcommands is exposed for the safety test in this package.
// Production code should call gitCommand (which routes through
// pkg/git.SafeGitCmd) rather than reading this map directly.
var mutatingGitSubcommands = map[string]bool{
	"commit":       true,
	"add":          true,
	"push":         true,
	"reset":        true,
	"restore":      true,
	"checkout":     true,
	"switch":       true,
	"rm":           true,
	"mv":           true,
	"merge":        true,
	"rebase":       true,
	"am":           true,
	"cherry-pick":  true,
	"revert":       true,
	"pull":         true,
	"stash":        true,
	"tag":          true,
	"branch":       true,
	"update-ref":   true,
	"update-index": true,
	"gc":           true,
	"prune":        true,
	"repack":       true,
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

// mergeCommitMessage is the message of a merge in progress: git's own
// MERGE_MSG ("Merge branch 'x' into y"), with the files whose conflicts were
// resolved listed in its body. A merge's staged diff is everything the merged
// branch brings in, so a message written from it would describe that branch's
// work instead of the merge. ok is false when no merge is in progress.
func mergeCommitMessage() (message string, ok bool) {
	if err := gitCommand("rev-parse", "-q", "--verify", "MERGE_HEAD").Run(); err != nil {
		return "", false
	}
	pathOut, err := gitCommand("rev-parse", "--git-path", "MERGE_MSG").Output()
	if err != nil {
		return "", false
	}
	path := strings.TrimSpace(string(pathOut))
	if !filepath.IsAbs(path) {
		if dir := currentGitDir(); dir != "" {
			path = filepath.Join(dir, path)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		head, herr := gitCommand("rev-parse", "--short", "MERGE_HEAD").Output()
		if herr != nil {
			return "", false
		}
		return "Merge " + strings.TrimSpace(string(head)), true
	}
	return formatMergeMessage(normalizeNewlines(string(raw))), true
}

// formatMergeMessage turns git's MERGE_MSG into a commit message: its own
// lines as they are, and the files its "# Conflicts:" comment names as a
// "Resolved conflicts in:" list.
func formatMergeMessage(raw string) string {
	var body, conflicts []string
	inConflicts := false
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "#") {
			text := strings.TrimSpace(strings.TrimPrefix(line, "#"))
			switch {
			case text == "Conflicts:":
				inConflicts = true
			case inConflicts && text != "":
				conflicts = append(conflicts, text)
			default:
				inConflicts = false
			}
			continue
		}
		body = append(body, line)
	}
	message := strings.TrimSpace(strings.Join(body, "\n"))
	if len(conflicts) > 0 {
		message += "\n\nResolved conflicts in:\n- " + strings.Join(conflicts, "\n- ")
	}
	return message
}
