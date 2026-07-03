package console

import (
	"bufio"
	"fmt"
	"regexp"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// ANSI color codes
const (
	ColorReset     = "\033[0m"
	ColorBold      = "\033[1m"
	ColorDim       = "\033[2m"
	ColorItalic    = "\033[3m"
	ColorUnderline = "\033[4m"

	// Colors
	ColorRed     = "\033[31m"
	ColorGreen   = "\033[32m"
	ColorYellow  = "\033[33m"
	ColorBlue    = "\033[34m"
	ColorMagenta = "\033[35m"
	ColorCyan    = "\033[36m"
	ColorWhite   = "\033[37m"
	ColorGray    = "\033[90m"

	// Bright colors
	ColorBrightRed     = "\033[91m"
	ColorBrightGreen   = "\033[92m"
	ColorBrightYellow  = "\033[93m"
	ColorBrightBlue    = "\033[94m"
	ColorBrightMagenta = "\033[95m"
	ColorBrightCyan    = "\033[96m"
	ColorBrightWhite   = "\033[97m"

	// Background colors
	BgRed     = "\033[41m"
	BgGreen   = "\033[42m"
	BgYellow  = "\033[43m"
	BgBlue    = "\033[44m"
	BgMagenta = "\033[45m"
	BgCyan    = "\033[46m"
	BgGray    = "\033[100m"
)

// MarkdownFormatter converts markdown to ANSI-colored terminal output
type MarkdownFormatter struct {
	enableColors bool
	enableInline bool
	// width is the content width used for width-dependent elements like the
	// horizontal rule. 0 means "unknown" — a sensible default is used.
	width int
}

// SetWidth sets the content width used for width-aware rendering (e.g. the
// horizontal rule spans this many columns). Returns the receiver for chaining.
func (f *MarkdownFormatter) SetWidth(w int) *MarkdownFormatter {
	f.width = w
	return f
}

// hrWidth returns the column count for a horizontal rule: the configured width
// clamped to a readable range, or 40 when width is unknown.
func (f *MarkdownFormatter) hrWidth() int {
	if f.width <= 0 {
		return 40
	}
	w := f.width
	if w < 10 {
		w = 10
	}
	if w > 120 {
		w = 120
	}
	return w
}

// NewMarkdownFormatter creates a new markdown formatter.
//
// The caller's enableColors preference is overridden by the environment
// per the no-color.org convention (SP-048-4a):
//   - NO_COLOR set to any non-empty value → colors OFF (always wins)
//   - FORCE_COLOR set to any non-empty value → colors ON (unless NO_COLOR)
//
// This lets users opt out of ANSI escapes globally (`NO_COLOR=1 sprout`)
// and CI pipelines opt in (`FORCE_COLOR=1 sprout > log.txt`) without
// individual call sites needing to know. The resolver lives in
// pkg/envutil (a zero-dep leaf) to avoid the import cycle pkg/utils →
// pkg/console.
func NewMarkdownFormatter(enableColors, enableInline bool) *MarkdownFormatter {
	enableColors = envutil.ResolveColorPreference(enableColors)
	return &MarkdownFormatter{
		enableColors: enableColors,
		enableInline: enableInline,
	}
}

// Format formats markdown text to colored terminal output
func (f *MarkdownFormatter) Format(text string) string {
	if !f.enableColors {
		return f.stripMarkdown(text)
	}

	// Process line by line for better formatting.
	//
	// bufio.Scanner's default buffer is 64 KiB and Scan() silently
	// returns false on tokens larger than that — for the assistant
	// output that meant a single ~64KiB code block (generated docs,
	// large diffs) would be truncated without warning. Bump the cap
	// to 1 MiB which comfortably covers any single line a model would
	// produce while still bounding worst-case memory.
	var result strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(text))
	const maxLineBytes = 1 << 20
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	inCodeBlock := false
	inCodeBlockLang := ""
	// Table buffering state
	var tableBuffer []string

	for scanner.Scan() {
		line := scanner.Text()

		// Handle code blocks. The decoration here is intentionally
		// lightweight: one optional header line for the language and a
		// dim `│` gutter on each code row. The previous form added four
		// rows of chrome per block (`┌─ Code Block`, `│ Language: X`,
		// `│`, `└─ End Code Block`), which more than doubled the size
		// of short snippets and crowded the scroll buffer on responses
		// with several blocks.
		if strings.HasPrefix(line, "```") {
			if !inCodeBlock {
				inCodeBlock = true
				lang := strings.TrimSpace(line[3:])
				inCodeBlockLang = lang
				if lang != "" {
					result.WriteString(fmt.Sprintf("%s──── %s ────%s\n", ColorDim, lang, ColorReset))
				}
			} else {
				inCodeBlock = false
				// No closing line — the next non-code row reads as the
				// natural boundary. Saves a row per block.
			}
			continue
		}

		if inCodeBlock {
			result.WriteString(fmt.Sprintf("%s│ %s%s\n", ColorDim, f.formatCodeLine(line, inCodeBlockLang), ColorReset))
			continue
		}

		// Table detection: lines starting with "|" are buffered until the
		// table ends (a line not starting with "|" or a blank line).
		if strings.HasPrefix(line, "|") {
			tableBuffer = append(tableBuffer, line)
			continue
		}

		// If we were buffering a table, flush it now.
		if len(tableBuffer) > 0 {
			result.WriteString(f.flushTable(tableBuffer))
			tableBuffer = nil
		}

		// Process regular markdown line
		formattedLine := f.formatMarkdownLine(line)
		result.WriteString(formattedLine + "\n")
	}

	// Flush any remaining table buffer at end of input.
	if len(tableBuffer) > 0 {
		result.WriteString(f.flushTable(tableBuffer))
		tableBuffer = nil
	}

	return strings.TrimSuffix(result.String(), "\n") // Remove trailing newline
}

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

// tableAlignment represents the text alignment for a column.
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

// formatInlineElements formats inline markdown elements
func (f *MarkdownFormatter) formatInlineElements(text string) string {
	// Bold text (**text** or __text__)
	boldRegex := regexp.MustCompile(`\*\*(.*?)\*\*|__(.*?)__`)
	text = boldRegex.ReplaceAllStringFunc(text, func(match string) string {
		var content string
		if strings.HasPrefix(match, "**") {
			content = match[2 : len(match)-2]
		} else {
			content = match[2 : len(match)-2]
		}
		return ColorBold + content + ColorReset
	})

	// Italic text — *text* is always safe (asterisks never appear in identifiers)
	italicAsteriskRegex := regexp.MustCompile(`\*(.*?)\*`)
	text = italicAsteriskRegex.ReplaceAllStringFunc(text, func(match string) string {
		if strings.HasPrefix(match, "**") {
			return match
		}
		content := match[1 : len(match)-1]
		return ColorItalic + content + ColorReset
	})

	// Italic via underscore _text_ — CommonMark requires underscores NOT
	// adjacent to alphanumeric chars (so handle_read_file stays intact).
	text = f.formatUnderscoreItalic(text)

	// Inline code (`code`)
	codeRegex := regexp.MustCompile("`(.*?)`")
	text = codeRegex.ReplaceAllStringFunc(text, func(match string) string {
		content := match[1 : len(match)-1]
		return fmt.Sprintf("%s%s%s", BgGray, content, ColorReset)
	})

	// Links [text](url) - just highlight the text part
	linkRegex := regexp.MustCompile(`\[(.*?)\]\((.*?)\)`)
	text = linkRegex.ReplaceAllStringFunc(text, func(match string) string {
		re := regexp.MustCompile(`\[(.*?)\]\((.*?)\)`)
		matches := re.FindStringSubmatch(match)
		if len(matches) >= 3 {
			return ColorUnderline + ColorCyan + matches[1] + ColorReset + ColorDim + "(" + matches[2] + ")" + ColorReset
		}
		return match
	})

	return text
}

// formatUnderscoreItalic handles `_text_` italic markers with CommonMark-style
// boundary checks: the underscore must NOT be adjacent to alphanumeric characters
// or other underscores. This prevents `handle_read_file` from being mangled.
func (f *MarkdownFormatter) formatUnderscoreItalic(text string) string {
	out := make([]byte, 0, len(text))
	for len(text) > 0 {
		i := strings.Index(text, "_")
		if i < 0 {
			out = append(out, text...)
			break
		}
		// Copy everything before this underscore
		out = append(out, text[:i]...)
		text = text[i:] // text now starts with "_"

		// Check left boundary: previous byte must NOT be alphanumeric or underscore
		if len(out) > 0 {
			prev := out[len(out)-1]
			if isIdentChar(prev) {
				// Underscore is part of an identifier — keep literal
				out = append(out, '_')
				text = text[1:]
				continue
			}
		}

		// Find the next underscore (the closing candidate)
		j := strings.Index(text[1:], "_")
		if j < 0 {
			// No closing underscore — keep literal
			out = append(out, '_')
			text = text[1:]
			continue
		}
		closingPos := j + 1 // position relative to text start

		// Check right boundary: byte after closing _ must NOT be alphanumeric or underscore
		if closingPos+1 < len(text) {
			next := text[closingPos+1]
			if isIdentChar(next) {
				// Underscore is part of an identifier — keep literal opening _
				out = append(out, '_')
				text = text[1:]
				continue
			}
		}

		// Italic: apply formatting to content between the two underscores
		content := text[1:closingPos]
		out = append(out, []byte(ColorItalic+content+ColorReset)...)
		text = text[closingPos+1:]
	}
	return string(out)
}

// isIdentChar returns true if b is a character that can appear in identifiers
// (letters, digits, underscore). Used to enforce CommonMark boundary rules for
// underscore-based formatting.
func isIdentChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') || b == '_'
}

// formatCodeLine provides basic syntax highlighting for code lines
func (f *MarkdownFormatter) formatCodeLine(line, lang string) string {
	lang = strings.ToLower(lang)

	switch lang {
	case "go", "golang":
		return f.highlightGo(line)
	case "python", "py":
		return f.highlightPython(line)
	case "bash", "sh", "shell":
		return f.highlightBash(line)
	case "json":
		return f.highlightJSON(line)
	case "yaml", "yml":
		return f.highlightYAML(line)
	case "javascript", "js":
		return f.highlightJavaScript(line)
	case "typescript", "ts":
		return f.highlightTypeScript(line)
	default:
		// Generic highlighting
		return f.highlightGeneric(line)
	}
}

// Language-specific highlighters
func (f *MarkdownFormatter) highlightGo(line string) string {
	// Comments
	if strings.Contains(line, "//") {
		parts := strings.SplitN(line, "//", 2)
		return ColorGreen + parts[0] + ColorDim + "//" + parts[1] + ColorReset
	}

	// Keywords
	keywords := []string{"func", "var", "const", "type", "struct", "interface", "if", "else", "for", "range", "return", "import", "package"}
	for _, kw := range keywords {
		re := regexp.MustCompile(`\b` + kw + `\b`)
		line = re.ReplaceAllString(line, ColorBlue+kw+ColorReset)
	}

	// Strings
	stringRegex := regexp.MustCompile(`"(.*?)"`)
	line = stringRegex.ReplaceAllString(line, ColorGreen+"$1"+ColorReset)

	return line
}

func (f *MarkdownFormatter) highlightPython(line string) string {
	// Comments
	if strings.Contains(line, "#") && !strings.Contains(line, "\"#") && !strings.Contains(line, "'#") {
		parts := strings.SplitN(line, "#", 2)
		return ColorGreen + parts[0] + ColorDim + "#" + parts[1] + ColorReset
	}

	// Keywords
	keywords := []string{"def", "class", "if", "elif", "else", "for", "in", "return", "import", "from", "as", "try", "except", "with"}
	for _, kw := range keywords {
		re := regexp.MustCompile(`\b` + kw + `\b`)
		line = re.ReplaceAllString(line, ColorBlue+kw+ColorReset)
	}

	// Strings
	stringRegex := regexp.MustCompile(`"(.*?)"|'(.*?)'`)
	line = stringRegex.ReplaceAllStringFunc(line, func(match string) string {
		if strings.HasPrefix(match, `"`) {
			return ColorGreen + match + ColorReset
		}
		return ColorGreen + match + ColorReset
	})

	return line
}

func (f *MarkdownFormatter) highlightBash(line string) string {
	// Comments
	if strings.HasPrefix(strings.TrimSpace(line), "#") {
		return ColorDim + line + ColorReset
	}

	// Commands
	commands := []string{"cd", "ls", "pwd", "echo", "cat", "grep", "sed", "awk", "find", "mkdir", "rm", "cp", "mv", "chmod"}
	for _, cmd := range commands {
		re := regexp.MustCompile(`\b` + cmd + `\b`)
		line = re.ReplaceAllString(line, ColorCyan+cmd+ColorReset)
	}

	// Options
	optionRegex := regexp.MustCompile(`(-\w+|--\w+)`)
	line = optionRegex.ReplaceAllString(line, ColorYellow+"$1"+ColorReset)

	return line
}

func (f *MarkdownFormatter) highlightJSON(line string) string {
	// Strings (keys and values)
	stringRegex := regexp.MustCompile(`"(.*?)"`)
	line = stringRegex.ReplaceAllString(line, ColorGreen+"\"$1\""+ColorReset)

	// Brackets and braces
	line = strings.ReplaceAll(line, "{", ColorBold+"{"+ColorReset)
	line = strings.ReplaceAll(line, "}", ColorBold+"}"+ColorReset)
	line = strings.ReplaceAll(line, "[", ColorBold+"["+ColorReset)
	line = strings.ReplaceAll(line, "]", ColorBold+"]"+ColorReset)

	return line
}

func (f *MarkdownFormatter) highlightYAML(line string) string {
	// Keys (before colon)
	if strings.Contains(line, ":") {
		parts := strings.SplitN(line, ":", 2)
		return ColorCyan + parts[0] + ColorReset + ":" + ColorGreen + parts[1] + ColorReset
	}

	// Comments
	if strings.Contains(line, "#") {
		parts := strings.SplitN(line, "#", 2)
		return ColorGreen + parts[0] + ColorDim + "#" + parts[1] + ColorReset
	}

	return line
}

func (f *MarkdownFormatter) highlightJavaScript(line string) string {
	// Comments
	if strings.Contains(line, "//") {
		parts := strings.SplitN(line, "//", 2)
		return ColorGreen + parts[0] + ColorDim + "//" + parts[1] + ColorReset
	}
	if strings.Contains(line, "/*") {
		return ColorDim + line + ColorReset
	}

	// Keywords
	keywords := []string{"function", "const", "let", "var", "if", "else", "for", "while", "return", "class", "import", "export"}
	for _, kw := range keywords {
		re := regexp.MustCompile(`\b` + kw + `\b`)
		line = re.ReplaceAllString(line, ColorBlue+kw+ColorReset)
	}

	// Strings
	stringRegex := regexp.MustCompile("(\".*?\")|('.*?')|(`.*?`)")
	line = stringRegex.ReplaceAllString(line, ColorGreen+"$1"+ColorReset)

	return line
}

func (f *MarkdownFormatter) highlightTypeScript(line string) string {
	// Similar to JavaScript but with TypeScript specifics
	result := f.highlightJavaScript(line)

	// TypeScript keywords
	tsKeywords := []string{"interface", "type", "enum", "implements", "extends", "public", "private", "protected"}
	for _, kw := range tsKeywords {
		re := regexp.MustCompile(`\b` + kw + `\b`)
		result = re.ReplaceAllString(result, ColorMagenta+kw+ColorReset)
	}

	return result
}

func (f *MarkdownFormatter) highlightGeneric(line string) string {
	// Generic syntax highlighting
	line = strings.ReplaceAll(line, "true", ColorGreen+"true"+ColorReset)
	line = strings.ReplaceAll(line, "false", ColorRed+"false"+ColorReset)
	line = strings.ReplaceAll(line, "null", ColorDim+"null"+ColorReset)

	// Strings
	stringRegex := regexp.MustCompile(`"(.*?)"|'(.*?)'`)
	line = stringRegex.ReplaceAllString(line, ColorGreen+"$1"+ColorReset)

	return line
}

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
