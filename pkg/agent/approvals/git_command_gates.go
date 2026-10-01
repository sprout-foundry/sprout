package approvals

import (
	"strings"

	"github.com/sprout-foundry/sprout/pkg/shelltext"
)

// IsGitWriteCommand reports whether `command` contains a git invocation
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
func IsGitWriteCommand(command string) bool {
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

// IsGitStashCommand checks whether `command` contains a `git stash`
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
func IsGitStashCommand(command string) bool {
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
