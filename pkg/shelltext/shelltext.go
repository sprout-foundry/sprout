// Package shelltext provides shared helpers for scanning shell command text:
// quote stripping, heredoc stripping, and git history-rewrite detection.
// It exists to deduplicate the per-package copies of these helpers and must
// stay import-cycle free (strings + regexp only).
package shelltext

import (
	"regexp"
	"strings"
)

// StripQuotedContent replaces all single-quoted and double-quoted string
// content in a shell command with spaces, preserving quote boundaries so
// token positions stay stable. This prevents false-positive git command
// detection when words like "git commit" appear inside JSON payloads or
// other quoted arguments.
func StripQuotedContent(s string) string {
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
			// Inside quotes: replace content with spaces (keep structural positions)
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

// IsGitHistoryRewriteCommand checks whether `command` contains a git
// invocation that can lose commit history (a ref moves backward, a
// branch/tag pointer disappears, a rebase rewrites commits). The change
// tracker can recover working-tree changes but cannot recover lost
// commits — only the reflog can — so these ops stay gated by default.
//
// Specifically matches:
//
//   - `git reset --hard <commit-ish>`  (backward ref-move)
//   - `git rebase` (any form — rewrites or drops commits)
//   - `git branch -d`/`-D`/`--delete` (deletes a branch ref)
//   - `git tag -d`/`--delete` (deletes a tag ref)
//
// `git reset --hard` *without* an explicit commit-ish argument is
// equivalent to `reset --hard HEAD` — it only reverts the working tree
// and is fully recoverable. We err toward "gated" when the argument
// shape is ambiguous (cheap false positive, expensive false negative).
func IsGitHistoryRewriteCommand(command string) bool {
	command = StripQuotedContent(command)
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
		if subcommand == "" {
			remaining = remaining[idx+1:]
			continue
		}
		rest := parts[subIdx+1:]

		switch subcommand {
		case "rebase":
			// `git rebase --abort` is a recovery op (reverts the in-progress
			// rebase state), not a history rewrite. Any other rebase form
			// (including `--abort` with other flags or arguments) is a rewrite.
			// The only permitted rebase invocation is pure `--abort`.
			if len(rest) == 1 && rest[0] == "--abort" {
				return false
			}
			return true
		case "reset":
			// `reset --hard` followed by an explicit commit-ish other than
			// HEAD (or a positional path filter) is a backward ref move.
			// Bare `reset --hard` or `reset --hard HEAD` only mutates the
			// working tree and is handled by the change tracker.
			hard := false
			for _, a := range rest {
				if a == "--hard" {
					hard = true
				}
			}
			if !hard {
				remaining = remaining[idx+1:]
				continue
			}
			// `--hard` with no further args, or with `HEAD` as the only
			// other token, is working-tree-only. Anything else (`HEAD~1`,
			// `abc123`, `origin/main`) abandons commits.
			hasCommitIsh := false
			for _, a := range rest {
				if a == "--hard" || strings.HasPrefix(a, "-") {
					continue
				}
				if a == "HEAD" {
					continue
				}
				hasCommitIsh = true
				break
			}
			if hasCommitIsh {
				return true
			}
		case "branch":
			for _, a := range rest {
				if a == "-d" || a == "-D" || a == "--delete" {
					return true
				}
			}
		case "tag":
			for _, a := range rest {
				if a == "-d" || a == "--delete" {
					return true
				}
			}
		}
		remaining = remaining[idx+1:]
	}
}

// heredocPattern matches heredoc syntax: `<<DELIM`, `<<-DELIM`, or `<<'DELIM'`.
// We capture the delimiter so we can find the closing line.
var heredocStartPattern = regexp.MustCompile(`<<-?['"]?(\w+)['"]?`)

// StripHeredocAndQuotes replaces heredoc bodies and quoted string content
// with spaces so risk pattern matchers don't scan DATA content as if it
// were a command. Without this, a heredoc writing a file whose source
// code mentions "git checkout" (or "rm -rf") would falsely match risk
// patterns.
//
// Heredoc: `cat > file <<'EOF' ... git checkout ... EOF` — everything
// between the opening `<<DELIM` and the closing delimiter line is data.
// Quoted strings: content inside '...' or "..." is replaced with spaces.
func StripHeredocAndQuotes(cmd string) string {
	// 1. Strip heredoc bodies first (they may contain quotes that would
	//    confuse the quote-stripping pass below).
	result := StripHeredocBodies(cmd)

	// 2. Strip quoted string content.
	return StripQuotedContent(result)
}

// StripHeredocBodies removes the content between heredoc delimiters,
// replacing it with spaces (preserving newlines so line-based structure
// is maintained for any downstream processing).
func StripHeredocBodies(cmd string) string {
	indices := heredocStartPattern.FindAllStringSubmatchIndex(cmd, -1)
	if len(indices) == 0 {
		return cmd
	}

	var b strings.Builder
	prevEnd := 0
	for _, match := range indices {
		// match: [fullStart, fullEnd, group1Start, group1End]
		delimStart := match[2]
		delimEnd := match[3]
		delim := cmd[delimStart:delimEnd]

		// Write everything before this heredoc start marker.
		b.WriteString(cmd[prevEnd:match[0]])
		// Write the heredoc start marker itself (e.g. `<<'EOF'`).
		b.WriteString(cmd[match[0]:match[1]])

		// Find the closing delimiter on its own line. It must appear at
		// the start of a line (after a newline or at the beginning).
		bodyStart := match[1]
		closeIdx := findHeredocClose(cmd[bodyStart:], delim)
		if closeIdx == -1 {
			// No closing delimiter found — treat rest as data, but we
			// can't safely strip it. Leave as-is (best effort).
			b.WriteString(cmd[bodyStart:])
			prevEnd = len(cmd)
			break
		}
		// Replace the heredoc body with spaces (preserving newlines).
		body := cmd[bodyStart : bodyStart+closeIdx+len(delim)]
		b.WriteString(replaceNonNewlinesWithSpaces(body))
		prevEnd = bodyStart + closeIdx + len(delim)
	}
	if prevEnd < len(cmd) {
		b.WriteString(cmd[prevEnd:])
	}
	return b.String()
}

// findHeredocClose finds the index of the closing delimiter line relative
// to the start of s. The delimiter must be at the start of a line. Returns
// the index of the delimiter start, or -1 if not found.
func findHeredocClose(s string, delim string) int {
	delimLine := "\n" + delim
	// Check if delimiter is at the very start (heredoc on same line).
	if strings.HasPrefix(s, delim) {
		return 0
	}
	idx := strings.Index(s, delimLine)
	if idx == -1 {
		return -1
	}
	// Ensure the delimiter is followed by a newline or end of string.
	afterDelim := idx + len(delimLine)
	if afterDelim >= len(s) || s[afterDelim] == '\n' || s[afterDelim] == '\r' {
		return idx + 1 // +1 to skip the newline we used for matching
	}
	// Partial match (e.g. delimiter is a prefix of another word) — search again.
	next := findHeredocClose(s[afterDelim:], delim)
	if next == -1 {
		return -1
	}
	return afterDelim + next
}

// replaceNonNewlinesWithSpaces replaces every character that is not a
// newline with a space. Used to blank out heredoc bodies while keeping
// line structure.
func replaceNonNewlinesWithSpaces(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			b[i] = '\n'
		} else {
			b[i] = ' '
		}
	}
	return string(b)
}
