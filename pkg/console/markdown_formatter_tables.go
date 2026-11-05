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
	// The full-width floor: a column may not shrink below the width it
	// needs to render every cell, so clamping never truncates a value.
	// A table that cannot fit at those widths stacks instead. SP-161c.
	if !clampColumnWidths(colWidths, colWidths, available) {
		// Even at the legibility floor the columns don't fit. Degrade to
		// the stacked layout — one record per block — so every value stays
		// readable instead of cells truncating into noise. SP-161c.
		return f.renderStackedTable(allCells, sepIndex, colCount)
	}

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

// minColumnWidth is the narrowest a column may be clamped to before the
// table is considered unfittable. A column below this renders as a
// character or two plus an ellipsis — the reader cannot tell what the
// value is, which is worse than a layout change. SP-161c.
const minColumnWidth = 4

// clampColumnWidths shrinks the widest columns until their total fits
// within maxTotal. A column is never clamped below minColumnWidth, and
// never below the width it needs to render every cell in full — going
// lower would truncate a value. Returns true when the widths now fit,
// false when even those floors overflow; the caller then degrades to a
// layout that stays readable.
//
// fullWidths[i] is the display width column i needs so that no cell in
// it is truncated (the widest cell, header included). A column of 8-char
// values may not shrink below 8: clamping it to 4 would render "aaaa…"
// and lose the value.
func clampColumnWidths(widths, fullWidths []int, maxTotal int) bool {
	sum := 0
	for _, w := range widths {
		sum += w
	}
	if sum <= maxTotal {
		return true
	}

	// Snapshot the floors before mutating widths — callers pass the same
	// slice for both, and the loop below shrinks widths in place.
	floors := make([]int, len(widths))
	copy(floors, fullWidths)

	floor := func(i int) int {
		f := minColumnWidth
		if i < len(floors) && floors[i] > f {
			f = floors[i]
		}
		return f
	}

	// Iterative clamping: reduce the widest column first. A column at its
	// floor is no longer a candidate, so the loop terminates when every
	// column is at its floor rather than draining to 1.
	for sum > maxTotal {
		maxIdx := -1
		maxW := 0
		for i, w := range widths {
			if w > floor(i) && w > maxW {
				maxIdx = i
				maxW = w
			}
		}
		if maxIdx < 0 {
			return false // every column at its floor; still too wide
		}
		widths[maxIdx]--
		sum--
	}
	return true
}

// renderStackedTable renders a table as one block per record when the
// columns cannot fit at the legibility floor. Each record shows every
// header/value pair on its own line, so no value is truncated and the
// reader can still tell what each field is. SP-161c.
func (f *MarkdownFormatter) renderStackedTable(allCells [][]string, sepIndex, colCount int) string {
	headers := make([]string, colCount)
	if len(allCells) > 0 {
		for i := 0; i < colCount; i++ {
			if i < len(allCells[0]) {
				headers[i] = strings.TrimSpace(allCells[0][i])
			}
		}
	}

	labelWidth := 0
	for _, h := range headers {
		if w := displayWidth(h); w > labelWidth {
			labelWidth = w
		}
	}

	var sb strings.Builder
	first := true
	for ri, row := range allCells {
		if ri == sepIndex || ri == 0 {
			continue
		}
		if !first {
			sb.WriteString("\n")
		}
		first = false
		for i := 0; i < colCount; i++ {
			cell := ""
			if i < len(row) {
				cell = strings.TrimSpace(row[i])
			}
			if cell == "" {
				continue
			}
			label := headers[i]
			pad := strings.Repeat(" ", max(labelWidth-displayWidth(label), 0))
			sb.WriteString("  ")
			sb.WriteString(ColorDim)
			sb.WriteString(label)
			sb.WriteString(pad)
			sb.WriteString(ColorReset)
			sb.WriteString("  ")
			sb.WriteString(f.formatInlineElements(cell))
			sb.WriteString("\n")
		}
	}

	return sb.String()
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
