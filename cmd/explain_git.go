//go:build !js

package cmd

// explain_git.go — git-command classification for the /explain
// command: isGitWriteCommand (destructive git ops the explainer must
// flag) and isGitRebaseCommand. Split out of explain.go.
import (
	"strings"

	"github.com/sprout-foundry/sprout/pkg/shelltext"
)

// isGitWriteCommand reports whether `command` contains a git invocation
// whose intent requires the orchestrator git-write flow. Replicated from
// pkg/agent to avoid a circular import.
func isGitWriteCommand(command string) bool {
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
		subcommand := ""
		subcommandIdx := 2
		for i := 1; i < len(parts); i++ {
			part := parts[i]
			if strings.HasPrefix(part, "-") {
				if part == "-c" || part == "-C" || part == "--exec-path" || part == "--git-dir" || part == "--work-tree" {
					i++
				}
				continue
			}
			subcommand = strings.TrimRight(part, ");\"'")
			subcommandIdx = i
			break
		}
		if subcommand != "" {
			subcommand = strings.TrimPrefix(subcommand, "--")
			subcommand = strings.TrimPrefix(subcommand, "-")
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
					remaining = remaining[idx+1:]
					continue
				}
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
			intentGated := []string{"commit", "push", "merge", "clone", "init", "worktree"}
			for _, writeCmd := range intentGated {
				if subcommand == writeCmd {
					return true
				}
			}
		}
		remaining = remaining[idx+1:]
	}
}

// isGitRebaseCommand reports whether `command` contains a `git rebase`
// invocation that rewrites history (i.e. NOT `git rebase --abort`).
// AGENTS.md bans rebase unconditionally; the only permitted rebase is
// `--abort` (recovery from a prior session's interrupted rebase).
func isGitRebaseCommand(command string) bool {
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
		subcommand := ""
		subIdx := 0
		for i := 1; i < len(parts); i++ {
			part := parts[i]
			if strings.HasPrefix(part, "-") {
				if part == "-c" || part == "-C" || part == "--exec-path" || part == "--git-dir" || part == "--work-tree" {
					i++
				}
				continue
			}
			subcommand = strings.TrimRight(part, ");\"'")
			subIdx = i
			break
		}
		if subcommand == "rebase" {
			rest := parts[subIdx+1:]
			// Pure `git rebase --abort` is the only permitted rebase
			// invocation (recovery from a prior session's interrupted
			// rebase). Any additional token — even something as benign
			// looking as `--no-verify` — makes the abort intent ambiguous
			// and is treated as a rewrite attempt.
			if len(rest) == 1 && rest[0] == "--abort" {
				return false
			}
			return true
		}
		if subcommand == "pull" {
			// AGENTS.md also bans `git pull --rebase` (and `-r`).
			// Use whole-token matching so `--no-rebase` and
			// `--recurse-submodules -r` don't false-positive.
			// `--rebase-preserve` is a real git flag (rebases + preserves
			// locally committed merges) — also a rebase, also banned.
			for _, a := range parts[subIdx+1:] {
				if a == "--rebase" || a == "-r" || a == "--rebase-preserve" {
					return true
				}
			}
		}
		remaining = remaining[idx+1:]
	}
}

// ---------------------------------------------------------------------------
// Contributing-check model
// ---------------------------------------------------------------------------
