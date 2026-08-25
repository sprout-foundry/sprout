// Package console: plain-text markdown stripping (no ANSI colors) and table stripping without ANSI (split from markdown_formatter.go)
package console

import (
	"regexp"
	"strings"
)

// stripMarkdown removes markdown formatting when colors are disabled
func (f *MarkdownFormatter) stripMarkdown(text string) string {
	// Handle tables first — strip pipe delimiters and align columns.
	text = f.stripTables(text)

	// Remove code blocks
	codeBlockRegex := regexp.MustCompile("```[\\s\\S]*?```")
	text = codeBlockRegex.ReplaceAllString(text, "[CODE BLOCK]")

	// Remove headers
	text = regexp.MustCompile("^#{1,6}\\s").ReplaceAllString(text, "")

	// Remove bold/italic
	text = regexp.MustCompile("\\*\\*(.*?)\\*\\*").ReplaceAllString(text, "$1")
	text = regexp.MustCompile("__(.*?)__").ReplaceAllString(text, "$1")
	text = regexp.MustCompile("\\*(.*?)\\*").ReplaceAllString(text, "$1")
	text = regexp.MustCompile("_(.*?)_").ReplaceAllString(text, "$1")

	// Remove inline code
	text = regexp.MustCompile("`(.*?)`").ReplaceAllString(text, "$1")

	// Remove links but keep text
	text = regexp.MustCompile("\\[(.*?)\\]\\(.*?\\)").ReplaceAllString(text, "$1")

	// Remove list markers
	text = regexp.MustCompile("^\\s*[-*+]\\s").ReplaceAllString(text, "• ")

	// Remove blockquotes
	text = regexp.MustCompile("^>\\s").ReplaceAllString(text, "")

	// Remove horizontal rules
	text = regexp.MustCompile("^---$|^---$").ReplaceAllString(text, "")

	return text
}

// stripTables detects table blocks in the text and strips pipe delimiters
// while preserving column alignment.
func (f *MarkdownFormatter) stripTables(text string) string {
	lines := strings.Split(text, "\n")
	result := make([]string, 0, len(lines))

	i := 0
	for i < len(lines) {
		line := lines[i]

		// Check if this line starts a table.
		if strings.HasPrefix(strings.TrimSpace(line), "|") {
			// Buffer table lines.
			var tableLines []string
			for i < len(lines) && strings.TrimSpace(lines[i]) != "" && strings.HasPrefix(strings.TrimSpace(lines[i]), "|") {
				tableLines = append(tableLines, lines[i])
				i++
			}
			// Process the table.
			result = append(result, strings.Split(f.stripTableBlock(tableLines), "\n")...)
			continue
		}

		result = append(result, line)
		i++
	}

	return strings.Join(result, "\n")
}

// stripTableBlock processes a table block and returns stripped text with
// aligned columns (no pipes).
func (f *MarkdownFormatter) stripTableBlock(rows []string) string {
	if len(rows) < 2 {
		// Not a valid table — just strip pipes.
		var sb strings.Builder
		for i, row := range rows {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(stripPipes(row))
		}
		return sb.String()
	}

	// Check for separator row.
	sepIdx := -1
	for i, row := range rows {
		if isSeparatorRow(row) {
			sepIdx = i
			break
		}
	}

	if sepIdx < 0 || sepIdx == 0 {
		// No valid separator — just strip pipes.
		var sb strings.Builder
		for i, row := range rows {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(stripPipes(row))
		}
		return sb.String()
	}

	// Parse cells.
	allCells := make([][]string, len(rows))
	for i, row := range rows {
		allCells[i] = parseTableRow(row)
	}

	colCount := len(allCells[sepIdx])
	for _, row := range allCells {
		if len(row) > colCount {
			colCount = len(row)
		}
	}

	// Calculate column widths (no ANSI, so just rune count).
	colWidths := make([]int, colCount)
	for ri, row := range allCells {
		if ri == sepIdx {
			continue
		}
		for i, cell := range row {
			if i >= colCount {
				break
			}
			w := len([]rune(cell))
			if w > colWidths[i] {
				colWidths[i] = w
			}
		}
	}

	// Clamp.
	maxWidth := f.width
	if maxWidth <= 0 {
		maxWidth = 80
	}
	available := maxWidth - 2 - (colCount - 1)
	if available < colCount {
		available = colCount
	}
	clampColumnWidths(colWidths, available)

	// Build output.
	var sb strings.Builder
	sb.WriteString("  ")

	// Header row.
	for i := 0; i < colCount; i++ {
		if i > 0 {
			sb.WriteString(" ")
		}
		cell := ""
		if i < len(allCells[0]) {
			cell = allCells[0][i]
		}
		sb.WriteString(padCellPlain(cell, colWidths[i]))
	}
	sb.WriteString("\n")

	// Separator rule.
	totalWidth := 0
	for i := 0; i < colCount; i++ {
		if i > 0 {
			totalWidth++
		}
		totalWidth += colWidths[i]
	}
	sb.WriteString("  ")
	sb.WriteString(strings.Repeat("─", totalWidth))
	sb.WriteString("\n")

	// Data rows.
	for ri, row := range allCells {
		if ri == sepIdx || ri == 0 {
			continue
		}
		sb.WriteString("  ")
		for i := 0; i < colCount; i++ {
			if i > 0 {
				sb.WriteString(" ")
			}
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			sb.WriteString(padCellPlain(cell, colWidths[i]))
		}
		sb.WriteString("\n")
	}

	return strings.TrimSuffix(sb.String(), "\n")
}

// stripPipes removes pipe delimiters from a table row.
func stripPipes(line string) string {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "|") {
		line = line[1:]
	}
	if strings.HasSuffix(line, "|") {
		line = strings.TrimSuffix(line, "|")
	}
	parts := strings.Split(line, "|")
	var cells []string
	for _, p := range parts {
		cells = append(cells, strings.TrimSpace(p))
	}
	return strings.Join(cells, "  ")
}

// padCellPlain pads a plain text cell to the given width (no ANSI codes).
func padCellPlain(cell string, width int) string {
	runeCount := len([]rune(cell))
	if runeCount >= width {
		runes := []rune(cell)
		if len(runes) > width {
			runes = runes[:width]
		}
		return string(runes)
	}
	padding := width - runeCount
	return cell + strings.Repeat(" ", padding)
}
