package tools

import "strings"

// stripQuotedSections replaces the content of quoted strings (single and double
// quotes) with spaces, preserving string length. This is used before pattern
// matching to avoid false positives from | or other shell metacharacters that
// appear inside quoted argument values (e.g., grep regex alternation like
// "rgba|gradient|shadow|image").
func stripQuotedSections(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\'' || c == '"' {
			inQuote = !inQuote
			b.WriteByte(c)
			continue
		}
		if inQuote {
			b.WriteByte(' ')
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// maxRisk returns the maximum risk level from a slice
func maxRisk(risks []SecurityRisk) SecurityRisk {
	max := SecuritySafe
	for _, r := range risks {
		if r > max {
			max = r
		}
	}
	return max
}

func extractCommandSubstitutions(cmd string) []string {
	var subs []string
	for i := 0; i < len(cmd); i++ {
		if cmd[i] != '$' || i+1 >= len(cmd) || cmd[i+1] != '(' {
			continue
		}
		start := i + 2
		depth := 1
		j := start
		for ; j < len(cmd); j++ {
			switch cmd[j] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					sub := strings.TrimSpace(cmd[start:j])
					if sub != "" {
						subs = append(subs, sub)
					}
					i = j
					break
				}
			}
			if depth == 0 {
				break
			}
		}
	}
	return subs
}

// containsRedirection returns true if the command contains output redirection
// operators (>, >>) that could write to arbitrary paths
func containsRedirection(cmd string) bool {
	for i := 0; i < len(cmd); i++ {
		r := cmd[i]
		if r == '\'' {
			for i++; i < len(cmd) && cmd[i] != '\''; i++ {
			}
			continue
		}
		if r == '"' {
			for i++; i < len(cmd) && cmd[i] != '"'; i++ {
			}
			continue
		}
		// File descriptor duplication (2>&1, 1>&2, etc.) is not file output redirection
		if r == '>' && i+1 < len(cmd) && cmd[i+1] == '&' {
			continue
		}
		if r == '>' && i+1 < len(cmd) && cmd[i+1] == '>' {
			return true
		}
		if r == '>' && (i+1 >= len(cmd) || cmd[i+1] != '=') {
			if i == 0 || cmd[i-1] != '>' {
				return true
			}
		}
	}
	return false
}

// isBenignRedirection returns true if output redirection targets known harmless sinks.
func isBenignRedirection(cmd string) bool {
	lower := strings.ToLower(cmd)
	return strings.Contains(lower, "> /tmp") || strings.Contains(lower, ">> /tmp") ||
		strings.Contains(lower, ">/tmp") ||
		strings.Contains(lower, "> /dev/null") || strings.Contains(lower, ">> /dev/null") ||
		strings.Contains(lower, ">/dev/null") ||
		strings.Contains(lower, "> /dev/stdout") || strings.Contains(lower, ">> /dev/stdout") ||
		strings.Contains(lower, "> /dev/stderr") || strings.Contains(lower, ">> /dev/stderr")
}
