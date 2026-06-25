package commands

import "strings"

// stripQuotedContent replaces all single-quoted and double-quoted string
// content in a shell command with spaces, preserving quote boundaries.
// This prevents false-positive git command detection when words like
// "git commit" appear inside JSON payloads or other quoted arguments.
func stripQuotedContent(s string) string {
	var b strings.Builder
	inSingle := false
	inDouble := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			b.WriteByte(ch)
		} else if ch == '"' && !inSingle {
			inDouble = !inDouble
			b.WriteByte(ch)
		} else if inSingle || inDouble {
			if ch == '\n' {
				b.WriteByte('\n')
			} else {
				b.WriteByte(' ')
			}
		} else {
			b.WriteByte(ch)
		}
	}
	return b.String()
}

// GitCommandValidator provides utility functions for validating git commands
// to prevent dangerous operations from being executed without proper approval.
// These functions are shared across /exec and /shell slash commands.

// IsGitCheckoutSubcommand checks if a git command is a checkout or switch operation.
// These operations are blocked from direct execution to require explicit user approval.
// This function checks ALL git commands in a compound shell command (e.g., "cd x && git checkout").
func IsGitCheckoutSubcommand(command string) bool {
	// Strip quoted content to avoid false positives from JSON payloads etc.
	command = stripQuotedContent(command)
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
		
		// Skip leading flags (e.g., -c key=val, -C path, --no-pager)
		for i := 1; i < len(parts); i++ {
			part := parts[i]
			if strings.HasPrefix(part, "-") {
				// Skip flags that take an argument: -c, -C, --exec-path, etc.
				if part == "-c" || part == "-C" || part == "--exec-path" || part == "--git-dir" || part == "--work-tree" {
					i++ // skip the next argument (the value)
				}
				continue
			}
			// Clean up the subcommand by removing trailing punctuation (e.g., "checkout)" -> "checkout")
			sub := strings.TrimRight(strings.TrimPrefix(strings.TrimPrefix(part, "--"), "-"), ");\"'")
			if sub == "checkout" || sub == "switch" {
				return true
			}
			// If we found a non-flag, non-checkout subcommand, stop checking this git invocation
			break
		}
		
		// Move past this git invocation to check for more
		remaining = remaining[idx+1:]
	}
}

// IsGitDiscardCommand checks if a git command could discard changes
// (restore, reset --hard, checkout -- <file>). These are always blocked
// from direct execution regardless of orchestrator permissions.
// This function checks ALL git commands in a compound shell command (e.g., "cd x && git reset").
func IsGitDiscardCommand(command string) bool {
	// Strip quoted content to avoid false positives from JSON payloads etc.
	command = stripQuotedContent(command)
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
		
		// Find the subcommand (skip leading flags like -c, -C)
		subcommand := ""
		for i := 1; i < len(parts); i++ {
			part := parts[i]
			if strings.HasPrefix(part, "-") {
				// Skip flags that take arguments
				if part == "-c" || part == "-C" || part == "--exec-path" || part == "--git-dir" || part == "--work-tree" {
					i++
				}
				continue
			}
			// Clean up the subcommand by removing trailing punctuation (e.g., "reset)" -> "reset")
			subcommand = strings.TrimRight(part, ");\"'")
			break
		}
		
			if subcommand != "" {
		// git restore always discards (working tree or staged changes)
		if subcommand == "restore" {
			return true
		}
		// git reset can discard staged changes (even without --hard)
		if subcommand == "reset" {
			return true
		}
		// git stash: pop/apply/drop/clear are destructive. bare stash
		// and stash push revert the working tree. stash list/show are
		// read-only and NOT gated here.
		if subcommand == "stash" {
			// Check the sub-subcommand (the token after "stash")
			stashSub := ""
			if len(parts) > 2 {
				// Find the token after the "stash" subcommand position
				for i := 1; i < len(parts); i++ {
					part := parts[i]
					if strings.HasPrefix(part, "-") {
						if part == "-c" || part == "-C" || part == "--exec-path" || part == "--git-dir" || part == "--work-tree" {
							i++
						}
						continue
					}
					// Found the subcommand — now check if there's a sub-sub
					cleaned := strings.TrimRight(part, ");\"'")
					if cleaned == "stash" && i+1 < len(parts) {
						stashSub = strings.TrimRight(parts[i+1], ");\"'")
					}
					break
				}
			}
			if stashSub == "list" || stashSub == "show" {
				// Read-only — don't gate
			} else {
				return true
			}
		}
	}// Move past this git invocation to check for more
		remaining = remaining[idx+1:]
	}
}

// ExtractGitSubcommand extracts the first git subcommand from a command string for display purposes.
func ExtractGitSubcommand(command string) string {
	// Strip quoted content to avoid false positives from JSON payloads etc.
	command = stripQuotedContent(command)
	idx := strings.Index(command, "git ")
	if idx == -1 {
		return "unknown"
	}
	
	gitCmd := command[idx:]
	parts := strings.Fields(gitCmd)
	if len(parts) < 2 {
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
