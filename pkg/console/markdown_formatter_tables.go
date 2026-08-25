// Package console: table rendering, alignment types, and cell padding/truncation helpers (split from markdown_formatter.go)
package console

import (
	"strings"
)

type tableAlignment int

const (
	alignLeft tableAlignment = iota
	alignCenter
	alignRight
)

// renderTable parses the table rows and renders aligned columns without pipe borders.
func (f *MarkdownFormatter) renderTable(rows []string, sepIndex int) string {
	// Parse all rows into cells.
	allCells := make([][]string, len(rows))
	for i, row := range rows {
		allCells[i] = parseTableRow(row)
	}

	// Determine column count from the separator row (use the widest row).
	colCount := len(allCells[sepIndex])
	for _, row := range allCells {
		if len(row) > colCount {
			colCount = len(row)
		}
	}

	// Determine alignment from separator row.
	alignments := make([]tableAlignment, colCount)
	sepCells := allCells[sepIndex]
	for i := 0; i < colCount; i++ {
		if i < len(sepCells) {
			cell := strings.TrimSpace(sepCells[i])
			if strings.HasPrefix(cell, ":") && strings.HasSuffix(cell, ":") {
				alignments[i] = alignCenter
			} else if strings.HasPrefix(cell, ":") {
				alignments[i] = alignLeft
			} else if strings.HasSuffix(cell, ":") {
				alignments[i] = alignRight
			} else {
				alignments[i] = alignLeft
			}
		} else {
			alignments[i] = alignLeft
		}
	}

	// Calculate column widths: max display width (rune count) of any non-separator cell.
	colWidths := make([]int, colCount)
	for ri, row := range allCells {
		if ri == sepIndex {
			continue
		}
		for i, cell := range row {
			if i >= colCount {
				break
			}
			// Apply inline formatting to get the rendered version for width measurement.
			rendered := f.formatInlineElements(cell)
			// Measure display width (rune count, stripping ANSI codes).
			w := displayWidth(rendered)
			if w > colWidths[i] {
				colWidths[i] = w
			}
		}
	}

	// Clamp column widths to fit within the formatter's width.
	maxWidth := f.width
	if maxWidth <= 0 {
		maxWidth = 80
	}
	// Account for 2-space left margin + (colCount-1) single-space gaps.
	available := maxWidth - 2 - (colCount - 1)
	if available < colCount {
		available = colCount // minimum: 1 char per column
	}
	clampColumnWidths(colWidths, available)

	// Build output.
	var sb strings.Builder
	sb.WriteString("  ") // 2-space left margin

	// Header row (bold).
	sb.WriteString(ColorBold)
	for i := 0; i < colCount; i++ {
		if i > 0 {
			sb.WriteString(" ")
		}
		cell := ""
		if i < len(allCells[0]) {
			cell = f.formatInlineElements(allCells[0][i])
		}
		sb.WriteString(padCell(cell, colWidths[i], alignments[i]))
	}
	sb.WriteString(ColorReset)
	sb.WriteString("\n")

	// Separator row (dim rule line).
	sb.WriteString("  ")
	sb.WriteString(ColorDim)
	totalWidth := 0
	for i := 0; i < colCount; i++ {
		if i > 0 {
			totalWidth++ // space gap
		}
		totalWidth += colWidths[i]
	}
	sb.WriteString(strings.Repeat("─", totalWidth))
	sb.WriteString(ColorReset)
	sb.WriteString("\n")

	// Data rows (skip separator).
	for ri, row := range allCells {
		if ri == sepIndex {
			continue
		}
		if ri == 0 {
			continue // header already rendered
		}
		sb.WriteString("  ")
		for i := 0; i < colCount; i++ {
			if i > 0 {
				sb.WriteString(" ")
			}
			cell := ""
			if i < len(row) {
				cell = f.formatInlineElements(row[i])
			}
			sb.WriteString(padCell(cell, colWidths[i], alignments[i]))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// clampColumnWidths ensures the total width of all columns fits within maxTotal.
// If the sum exceeds maxTotal, widths are reduced proportionally, with a
// minimum of 1 character per column.
func clampColumnWidths(widths []int, maxTotal int) {
	sum := 0
	for _, w := range widths {
		sum += w
	}
	if sum <= maxTotal {
		return
	}

	// Iterative clamping: reduce the widest columns first.
	for sum > maxTotal && len(widths) > 0 {
		// Find the widest column that's above minimum (1).
		maxIdx := -1
		maxW := 1
		for i, w := range widths {
			if w > maxW {
				maxIdx = i
				maxW = w
			}
		}
		if maxIdx < 0 {
			break // all columns at minimum
		}
		widths[maxIdx]--
		sum--
	}
}

// padCell pads (or truncates) a cell string to the given width with the
// specified alignment. The width is in display characters (rune count minus
// ANSI escape sequences).
func padCell(cell string, width int, align tableAlignment) string {
	dw := displayWidth(cell)
	if dw >= width {
		// Truncate if needed (by display width).
		return truncateDisplay(cell, width)
	}

	padding := width - dw
	var leftPad, rightPad int
	switch align {
	case alignLeft:
		leftPad = 0
		rightPad = padding
	case alignCenter:
		leftPad = padding / 2
		rightPad = padding - leftPad
	case alignRight:
		leftPad = padding
		rightPad = 0
	default:
		rightPad = padding
	}

	var sb strings.Builder
	sb.WriteString(strings.Repeat(" ", leftPad))
	sb.WriteString(cell)
	sb.WriteString(strings.Repeat(" ", rightPad))
	return sb.String()
}

// truncateDisplay truncates s to the given display width, cutting off ANSI
// sequences safely. Uses the existing displayWidth and truncateToWidth helpers.
func truncateDisplay(s string, maxWidth int) string {
	// Strip ANSI, truncate, then re-apply ANSI from original.
	plain := stripANSIEscapeCodes(s)
	truncated := truncateToWidth(plain, maxWidth, "…")
	// If the plain truncated text is shorter than the original, we need to
	// rebuild with ANSI codes. Simple approach: just return the truncated
	// plain text — the ANSI codes are formatting-only and the truncation
	// is on the content.
	return truncated
}
