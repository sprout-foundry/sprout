// Package console: markdown detection heuristics for deciding whether to format text as markdown (split from markdown_formatter.go)
package console

import (
	"regexp"
	"strings"
)

// IsLikelyMarkdown checks if text contains markdown patterns
// More selective to avoid formatting code blocks, shell output, or other non-summary text
func IsLikelyMarkdown(text string) bool {
	// Skip if text looks like command output or code
	// Tool calls already have their own formatting via ToolLog()
	if looksLikeCommandOrCodeOutput(text) {
		return false
	}

	// Check for headers (# ## ### etc.)
	if strings.Contains(text, "#") {
		// Look for lines that start with #
		lines := strings.Split(text, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") {
				// Also ensure this isn't a comment in code
				if !strings.Contains(line, "//") && !strings.Contains(line, "#include") && !strings.Contains(line, "#define") && !strings.HasPrefix(line, "#include") && !strings.HasPrefix(line, "##") && len(line) > 2 {
					return true
				}
			}
		}
	}

	// Check for markdown patterns that are likely for summary text
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Header patterns - must be at start of line or early in text
		if strings.HasPrefix(trimmed, "# ") || strings.HasPrefix(trimmed, "## ") ||
			strings.HasPrefix(trimmed, "### ") || strings.HasPrefix(trimmed, "#### ") {
			// Ensure not a comment
			if !strings.Contains(trimmed, "//") && !strings.Contains(trimmed, "#include") && !strings.Contains(trimmed, "#define") {
				return true
			}
		}

		// Bold for emphasis (e.g., **Key points**)
		if strings.Count(trimmed, "**") >= 2 {
			return true
		}

		// Bullet lists with descriptive text after
		if strings.HasPrefix(trimmed, "- ") && len(trimmed) > 3 {
			// Accept if it looks like a meaningful list item (not just flags)
			// Good: "- Completed the setup"
			// Bad: "-v" or just a flag
			if len(trimmed) > 10 && !regexp.MustCompile(`^-\s+[a-z]$`).MatchString(trimmed) {
				return true
			}
		}

		// Blockquotes are markdown
		if strings.HasPrefix(trimmed, "> ") {
			return true
		}

		// Inline code backticks are markdown
		if strings.Count(trimmed, "`") >= 2 {
			return true
		}

		// Links are markdown
		if strings.Contains(trimmed, "](") && strings.Contains(trimmed, "[") {
			return true
		}

		// Code block delimiters
		if trimmed == "```" || strings.HasPrefix(trimmed, "```") {
			return true
		}
	}

	return false
}

// looksLikeCommandOrCodeOutput returns true if text appears to be
// command output, code, or other non-summary content that shouldn't be markdown-formatted
func looksLikeCommandOrCodeOutput(text string) bool {
	// Tool call patterns - things that look like tool logs
	// Format: [1 - 0%] read file filename.go
	if regexp.MustCompile(`^\[\d+\s*-\s*\d+%\s*\]\s+\w+\s+\w+`).MatchString(text) {
		return true
	}

	// Lines starting with file paths or similar
	if regexp.MustCompile(`^[\w\/\-\_\.]+\.\w+:\d+`).MatchString(text) {
		return true
	}

	// Check if majority of lines look like code
	lines := strings.Split(text, "\n")
	if len(lines) > 1 {
		codeLineCount := 0
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			// Skip empty lines
			if trimmed == "" {
				continue
			}

			// Lines with common code patterns
			if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '}' ||
				strings.Contains(trimmed, "func ") || strings.Contains(trimmed, "var ") ||
				strings.Contains(trimmed, "const ") || strings.Contains(trimmed, "type ") ||
				strings.Contains(trimmed, "import") || strings.Contains(trimmed, "package") ||
				strings.Contains(trimmed, "}") || strings.Contains(trimmed, "{") ||
				strings.Contains(trimmed, "// ")) {
				codeLineCount++
			}

			// Lines ending with semicolons or parentheses (code-like)
			if strings.HasSuffix(trimmed, ";") || strings.Contains(trimmed, "(){") {
				codeLineCount++
			}
		}
		// If more than 50% of lines look like code, skip markdown formatting
		if codeLineCount > 0 && codeLineCount > len(lines)/2 {
			return true
		}
	}

	return false
}
