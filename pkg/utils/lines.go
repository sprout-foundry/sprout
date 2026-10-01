package utils

import "strings"

// lines.go — the two line-splitting semantics the codebase needs, named for
// what they do with a trailing newline. They had four private `splitLines`
// copies across packages with silently divergent edge behavior before this
// consolidation; the names below make the difference visible at call sites.

// SplitLinesKeepEmpty splits s on "\n" and keeps every element, including
// the empty one a trailing newline produces: "a\n" → ["a", ""] and "" → [""].
// Use when line positions matter (line-numbered diffs, approval hunks).
func SplitLinesKeepEmpty(s string) []string {
	if s == "" {
		return []string{""}
	}
	return strings.Split(s, "\n")
}

// SplitLines splits s on "\n" and drops the single empty element a trailing
// newline produces: "a\n" → ["a"] and "" → nil. Use when the lines are
// processed as a set or list and a phantom final line would mislead
// (diffing, field extraction, text tools).
func SplitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
