// Package configuration: heredoc / quoted-string stripping for risk classifiers.
// (split from config_risk_subagent.go)
package configuration

import (
	"regexp"
	"strings"
)

// heredocPattern matches heredoc syntax: `<<DELIM`, `<<-DELIM`, or `<<'DELIM'`.
// We capture the delimiter so we can find the closing line.
var heredocStartPattern = regexp.MustCompile(`<<-?['"]?(\w+)['"]?`)

// stripHeredocAndQuotes replaces heredoc bodies and quoted string content
// with spaces so the risk pattern matcher doesn't scan DATA content as if
// it were a command. Without this, a heredoc writing a file whose source
// code mentions "git checkout" (or "rm -rf") would falsely match risk
// patterns.
//
// Heredoc: `cat > file <<'EOF' ... git checkout ... EOF` — everything
// between the opening `<<DELIM` and the closing delimiter line is data.
// Quoted strings: content inside '...' or "..." is replaced with spaces
// (same approach as pkg/agent_tools.stripQuotedSections).
func stripHeredocAndQuotes(cmd string) string {
	// 1. Strip heredoc bodies first (they may contain quotes that would
	//    confuse the quote-stripping pass below).
	result := stripHeredocBodies(cmd)

	// 2. Strip quoted string content.
	return stripQuotedContent(result)
}

// stripHeredocBodies removes the content between heredoc delimiters,
// replacing it with spaces (preserving newlines so line-based structure
// is maintained for any downstream processing).
func stripHeredocBodies(cmd string) string {
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
		b.WriteString(cmd[prevEnd:match[1]])
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

// stripQuotedContent replaces the content of quoted strings (single and
// double quotes) with spaces, preserving the quote characters themselves.
// This prevents risk patterns from matching command-like text inside
// string literals (e.g. echo "git checkout main").
func stripQuotedContent(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inQuote := false
	var quoteChar byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !inQuote && (c == '\'' || c == '"') {
			inQuote = true
			quoteChar = c
			b.WriteByte(c)
			continue
		}
		if inQuote {
			if c == quoteChar {
				inQuote = false
				b.WriteByte(c)
			} else {
				b.WriteByte(' ')
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
