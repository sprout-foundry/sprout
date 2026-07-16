// Package console: per-line markdown formatting, table detection/flush, and separator-row helpers (split from markdown_formatter.go)
package console

import (
	"fmt"
	"regexp"
	"strings"
)

// formatMarkdownLine formats a single markdown line
func (f *MarkdownFormatter) formatMarkdownLine(line string) string {
	// Headers
	if strings.HasPrefix(line, "# ") {
		return fmt.Sprintf("%s%s%s%s", ColorBold+ColorBrightBlue, strings.Repeat("█", 3), line[2:], ColorReset)
	}
	if strings.HasPrefix(line, "## ") {
		return fmt.Sprintf("%s%s%s%s", ColorBold+ColorCyan, strings.Repeat("▪ ", 2), line[3:], ColorReset)
	}
	if strings.HasPrefix(line, "### ") {
		return fmt.Sprintf("%s%s%s%s", ColorBold+ColorBlue, "▸ ", line[4:], ColorReset)
	}
	if strings.HasPrefix(line, "#### ") {
		return fmt.Sprintf("%s%s%s%s", ColorBold, "• ", line[5:], ColorReset)
	}

	// If it starts with "- " or "* " or "+ " with optional leading whitespace
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "+ ") ||
		regexp.MustCompile(`^\s*[-*+]\s`).MatchString(line) {
		// Normalize leading whitespace into structured indent levels.
		// CommonMark allows 2-4 spaces per nesting level; we normalize
		// to 2 visible spaces per level for compact readability.
		bulletPattern := `^(\s*)([-*+])(\s+)(.*)$`
		re := regexp.MustCompile(bulletPattern)
		if matches := re.FindStringSubmatch(line); len(matches) > 0 {
			leadingSpaces := len(matches[1])
			level := leadingSpaces / 2
			indent := strings.Repeat("  ", level)
			return fmt.Sprintf("%s%s%s%s%s", indent, ColorGreen+matches[2], ColorReset+matches[3], matches[4], ColorReset)
		}
	}

	// Horizontal rule
	if strings.TrimSpace(line) == "---" || strings.TrimSpace(line) == "***" {
		return fmt.Sprintf("%s%s%s", ColorDim, strings.Repeat("─", f.hrWidth()), ColorReset)
	}

	// Blockquotes
	if strings.HasPrefix(line, "> ") {
		quoted := f.formatMarkdownLine(line[2:])
		return fmt.Sprintf("%s│ %s%s", ColorDim, quoted, ColorReset)
	}

	// Inline formatting
	if f.enableInline {
		line = f.formatInlineElements(line)
	}

	return line
}

// flushTable processes the buffered table lines and returns rendered output.
// If the buffer doesn't form a valid table (needs ≥2 rows with a separator),
// fall back to rendering each line as plain markdown.
func (f *MarkdownFormatter) flushTable(buffer []string) string {
	if len(buffer) < 2 {
		// Not enough rows for a valid table — render as plain text.
		var sb strings.Builder
		for i, line := range buffer {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(f.formatMarkdownLine(line))
		}
		return sb.String() + "\n"
	}

	// Check if there's a separator row (second row should be |---|---| pattern).
	sepRow := -1
	for i, line := range buffer {
		if isSeparatorRow(line) {
			sepRow = i
			break
		}
	}

	if sepRow < 0 || sepRow == 0 {
		// No separator or separator is first row — not a valid table.
		var sb strings.Builder
		for i, line := range buffer {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(f.formatMarkdownLine(line))
		}
		return sb.String() + "\n"
	}

	// Valid table: parse and render.
	return f.renderTable(buffer, sepRow)
}

// isSeparatorRow returns true if the line matches the GFM separator pattern
// like |---|---| or |:--|--:|:--:|
func isSeparatorRow(line string) bool {
	// Strip leading/trailing whitespace and pipes, then check each cell.
	cells := parseTableRow(line)
	if len(cells) < 2 {
		return false
	}
	for _, cell := range cells {
		trimmed := strings.TrimSpace(cell)
		// Each cell should be a sequence of hyphens, optionally with colons.
		if trimmed == "" {
			return false
		}
		for _, r := range trimmed {
			if r != '-' && r != ':' {
				return false
			}
		}
	}
	return true
}

// parseTableRow splits a pipe-delimited row into cells, trimming whitespace.
func parseTableRow(line string) []string {
	// Strip leading/trailing pipe if present.
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "|") {
		line = line[1:]
	}
	if strings.HasSuffix(line, "|") {
		line = strings.TrimSuffix(line, "|")
	}

	// Split on pipes.
	parts := strings.Split(line, "|")
	cells := make([]string, 0, len(parts))
	for _, p := range parts {
		cells = append(cells, strings.TrimSpace(p))
	}
	return cells
}
