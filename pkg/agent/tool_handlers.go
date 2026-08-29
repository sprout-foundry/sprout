package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/shelltext"
)

// Shared utility functions for tool handlers

// isGitWriteCommand reports whether `command` contains a git invocation
// whose intent (not safety) requires the orchestrator git-write flow:
// `commit`, `push`, branch/tag CREATE operations (not delete — those go
// through the history-rewrite gate), and pushes via `clone`/`init` of
// existing-repo destinations.
//
// Tier A working-tree-mutating ops (`checkout`, `switch`, `restore`,
// `reset` without history-loss, `clean`, `rm`, `mv`, `am`, `apply`,
// `cherry-pick`, `revert`, `fetch`, `pull`, `stash` mutations) used to
// be gated here; they're now allowed because the change tracker captures
// before-content and recover_file / recover_bulk are the recovery path.
// Tier B history-loss ops (`rebase`, `reset --hard <commit-ish>`,
// `branch -D`, `tag -d`) are routed through isGitHistoryRewriteCommand
// instead — that gate has its own opt-in flag (AllowGitHistoryRewrite)
// and prompts the user (auto-approved when AllowGitHistoryRewrite=true).
func isGitWriteCommand(command string) bool {
	// Strip quoted content to avoid false positives from JSON payloads etc.
	command = shelltext.StripQuotedContent(command)
	// Find all occurrences of "git " in the command and check each subcommand
	remaining := command
	for {
		idx := strings.Index(remaining, "git ")
		if idx == -1 {
			return false
		}

		gitCmd := remaining[idx:]
		parts := strings.Fields(gitCmd)
		if len(parts) < 2 {
			remaining = remaining[idx+1:]
			continue
		}

		// Find the actual subcommand (skip "git" and any leading flags like -c, -C, etc.)
		subcommand := ""
		subcommandIdx := 2
		for i := 1; i < len(parts); i++ {
			part := parts[i]
			if strings.HasPrefix(part, "-") {
				if part == "-c" || part == "-C" || part == "--exec-path" || part == "--git-dir" || part == "--work-tree" {
					i++ // skip the flag value
				}
				continue
			}
			// Clean up the subcommand by removing trailing punctuation (e.g., "commit)" -> "commit")
			subcommand = strings.TrimRight(part, ");\"'")
			subcommandIdx = i
			break
		}

		if subcommand != "" {
			subcommand = strings.TrimPrefix(subcommand, "--")
			subcommand = strings.TrimPrefix(subcommand, "-")

			// branch / tag — only CREATE/UPDATE operations land here.
			// Deletes (`-d`/`-D`/`--delete`) are caught upstream by
			// isGitHistoryRewriteCommand; if we see them here it's
			// because the caller hasn't routed through that gate, but
			// we still need to skip them so the positional `<name>`
			// argument doesn't get misread as "create new branch X".
			rest := parts[subcommandIdx+1:]
			switch subcommand {
			case "branch":
				hasDelete := false
				for _, arg := range rest {
					if arg == "-d" || arg == "-D" || arg == "--delete" {
						hasDelete = true
						break
					}
				}
				if hasDelete {
					// Delete = history-rewrite, not the intent gate's job.
					remaining = remaining[idx+1:]
					continue
				}
				// `git branch` / `-a` / `--list` etc. are read-only.
				// A positional (non-flag) argument means create.
				createFlags := map[string]struct{}{
					"-m": {}, "-M": {}, "--move": {},
					"-c": {}, "-C": {}, "--copy": {}, "-f": {}, "--force": {},
					"-u": {}, "--set-upstream-to": {}, "--unset-upstream": {}, "--edit-description": {},
				}
				for _, arg := range rest {
					if _, ok := createFlags[arg]; ok {
						return true
					}
					if !strings.HasPrefix(arg, "-") {
						return true
					}
				}
				remaining = remaining[idx+1:]
				continue
			case "tag":
				hasDelete := false
				for _, arg := range rest {
					if arg == "-d" || arg == "--delete" {
						hasDelete = true
						break
					}
				}
				if hasDelete {
					remaining = remaining[idx+1:]
					continue
				}
				createFlags := map[string]struct{}{
					"-a": {}, "-s": {}, "-u": {}, "-f": {}, "--force": {},
				}
				for _, arg := range rest {
					if _, ok := createFlags[arg]; ok {
						return true
					}
					if !strings.HasPrefix(arg, "-") {
						return true
					}
				}
				remaining = remaining[idx+1:]
				continue
			}

			// Intent gates: commit & push always require the orchestrator
			// (or the commit-tool redirect for commit). clone/init create
			// new repos — gated to keep the agent from making side
			// repositories without the orchestrator opting in. merge
			// stays gated because a merge commit is a commit.
			intentGated := []string{"commit", "push", "merge", "clone", "init", "worktree"}
			for _, writeCmd := range intentGated {
				if subcommand == writeCmd {
					return true
				}
			}
		}

		// Move past this git invocation to check for more
		remaining = remaining[idx+1:]
	}
}

// isGitStashCommand checks whether `command` contains a `git stash`
// invocation that is not purely read-only. `git stash` (bare or with
// `push`) saves the working tree and reverts it to HEAD — the save
// itself isn't destructive, but the typical pattern is stash + build +
// pop, and the pop is where merge conflicts silently revert files.
// `stash pop`, `stash apply`, `stash drop`, and `stash clear` are all
// destructive. `stash list` and `stash show` are read-only and handled
// by shellLooksReadOnly, but we include them here for completeness —
// the gate blocks all stash subcommands except list/show.
//
// The gate is intentionally broad: any `git stash` that isn't `list`
// or `show` is blocked, because even `git stash push` sets up the
// pop-that-can-corrupt that we want to prevent.
func isGitStashCommand(command string) bool {
	command = shelltext.StripQuotedContent(command)
	remaining := command
	for {
		idx := strings.Index(remaining, "git ")
		if idx == -1 {
			return false
		}
		gitCmd := remaining[idx:]
		parts := strings.Fields(gitCmd)
		if len(parts) < 2 {
			remaining = remaining[idx+1:]
			continue
		}
		// Find the subcommand, skipping leading git global flags.
		subcommand := ""
		for i := 1; i < len(parts); i++ {
			part := parts[i]
			if strings.HasPrefix(part, "-") {
				if part == "-c" || part == "-C" || part == "--exec-path" || part == "--git-dir" || part == "--work-tree" {
					i++
				}
				continue
			}
			subcommand = strings.TrimRight(part, ");\"'")
			break
		}
		if subcommand == "stash" {
			// `git stash list` and `git stash show` are read-only.
			// Everything else (bare stash, push, pop, apply, drop, clear)
			// is gated.
			if len(parts) > 2 {
				rest := parts[2]
				rest = strings.TrimRight(rest, ");\"'")
				if rest == "list" || rest == "show" {
					remaining = remaining[idx+1:]
					continue
				}
			}
			// Bare `git stash` or any non-list/show subcommand.
			return true
		}
		remaining = remaining[idx+1:]
	}
}

// extractGitSubcommand extracts the subcommand from a git command string for display purposes.
func extractGitSubcommand(command string) string {
	parts := strings.Fields(strings.TrimSpace(command))
	if len(parts) < 2 || parts[0] != "git" {
		return "unknown"
	}
	for i := 1; i < len(parts); i++ {
		part := parts[i]
		if strings.HasPrefix(part, "-") {
			if part == "-c" || part == "-C" || part == "--exec-path" || part == "--git-dir" || part == "--work-tree" {
				i++ // skip the flag value
			}
			continue
		}
		return part
	}
	return "unknown"
}

// isGitCommitSubcommand checks if a git command is specifically a commit operation
// (as opposed to other write operations like push, merge, etc.)
func isGitCommitSubcommand(command string) bool {
	parts := shellSplit(strings.TrimSpace(command))
	if len(parts) < 2 || parts[0] != "git" {
		return false
	}
	// Skip leading flags and -c key=value config options to find the actual subcommand
	for i := 1; i < len(parts); i++ {
		part := parts[i]
		if part == "-c" {
			// -c takes the next argument as key=value, skip it too
			i++
			continue
		}
		if strings.HasPrefix(part, "-") {
			continue
		}
		subcommand := strings.TrimPrefix(strings.TrimPrefix(part, "--"), "-")
		return subcommand == "commit"
	}
	return false
}

// convertToString safely converts a parameter to string with proper error handling
func convertToString(param interface{}, paramName string) (string, error) {
	switch v := param.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	case int, int32, int64, float32, float64:
		return fmt.Sprintf("%v", v), nil
	case bool:
		return fmt.Sprintf("%t", v), nil
	case map[string]interface{}:
		// If it's a map, try to convert to JSON string
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			return "", agenterrors.Wrapf(err, "parameter '%s' is an object that cannot be converted to string", paramName)
		}
		return string(jsonBytes), nil
	case nil:
		return "", agenterrors.NewValidation("parameter '"+paramName+"' is missing or null", nil)
	default:
		return "", agenterrors.NewValidation(fmt.Sprintf("parameter '%s' has invalid type %T, expected string", paramName, param), nil)
	}
}

// extractGitCommitArgs parses a `git commit ...` command line and extracts
// the message from -m or --message flags. The command comes from an LLM tool
// argument, which may include shell-style quoting (single or double quotes).
// We support basic shell quoting so that `git commit -m "fix: typo"` correctly
// extracts `fix: typo`.
//
// Returns the extracted message (may be empty if no -m/--message flag found).
func extractGitCommitArgs(command string) string {
	tokens := shellSplit(command)
	message := ""

	for i := 0; i < len(tokens)-1; i++ {
		switch tokens[i] {
		case "-m", "--message":
			// Git supports multiple -m flags to build multi-paragraph messages.
			// Each -m becomes a separate paragraph in the commit message.
			if message != "" {
				message += "\n\n"
			}
			message += tokens[i+1]
			i++ // skip the next token (it's the message value)
		}
	}

	return message
}

// shellSplit performs basic shell-style word splitting that respects
// single and double quotes. This is intentionally minimal — it handles
// the common patterns LLMs use when constructing git commit commands.
// It does NOT handle escape sequences, backticks, or variable expansion.
func shellSplit(s string) []string {
	var tokens []string
	var current strings.Builder
	var inQuote rune // 0 = not in quote, '"' or '\'' == in quote
	justClosedQuote := false

	for _, r := range s {
		switch {
		case inQuote != 0:
			if r == inQuote {
				inQuote = 0
				justClosedQuote = true
			} else {
				current.WriteRune(r)
			}
		case r == '"' || r == '\'':
			inQuote = r
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if current.Len() > 0 || justClosedQuote {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			justClosedQuote = false
		default:
			current.WriteRune(r)
			justClosedQuote = false
		}
	}

	if current.Len() > 0 || justClosedQuote {
		tokens = append(tokens, current.String())
	}

	return tokens
}
