// Package console: ANSI color constants, MarkdownFormatter struct, and the top-level Format entry point (split from markdown_formatter.go)
package console

import (
	"bufio"
	"fmt"
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
	// codeBlockIndent records the leading-whitespace depth of the opening
	// fence line. Fences indented inside a list item (e.g. "  ```go") are
	// valid CommonMark: their content and the closing fence are indented
	// to the same depth. We dedent content lines by this amount so the
	// gutter isn't double-indented. 0 means a column-0 fence, which keeps
	// byte-identical output with the original behavior.
	codeBlockIndent := 0
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
		//
		// Fence detection uses the left-trimmed line so that fences
		// indented inside a list item (e.g. "  ```go") are still
		// recognized as fences rather than leaking raw backticks into
		// the rendered output.
		trimmedLine := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmedLine, "```") {
			indent := len(line) - len(trimmedLine)
			if !inCodeBlock {
				// Opening fence.
				inCodeBlock = true
				lang := strings.TrimSpace(trimmedLine[3:])
				inCodeBlockLang = lang
				codeBlockIndent = indent
				if lang != "" {
					result.WriteString(fmt.Sprintf("%s──── %s ────%s\n", ColorDim, lang, ColorReset))
				}
				continue
			} else if indent <= 3 {
				// Closing fence. CommonMark: a closing fence may be
				// preceded by up to three spaces of indentation,
				// independent of the opening fence's indentation.
				inCodeBlock = false
				codeBlockIndent = 0
				continue
			}
			// else: inside a code block but this fence-like line is
			// indented more than three spaces → treat it as regular code
			// content (the inCodeBlock branch below renders it).
		}

		if inCodeBlock {
			// Dedent content lines by the opening fence's indentation so
			// nested code blocks don't appear double-indented behind the
			// gutter. For column-0 fences (codeBlockIndent == 0) this is
			// a no-op, preserving byte-identical output.
			codeLine := dedentLine(line, codeBlockIndent)
			result.WriteString(fmt.Sprintf("%s│ %s%s\n", ColorDim, f.formatCodeLine(codeLine, inCodeBlockLang), ColorReset))
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

// dedentLine removes up to n leading space characters from line. If the line
// has fewer than n leading spaces, they are all removed. Tabs are treated as
// a single character and are not counted as spaces. Used to strip the
// indentation of an opening code fence from its content lines so nested code
// blocks (indented inside a list item) render behind the gutter without being
// double-indented.
func dedentLine(line string, n int) string {
	if n <= 0 {
		return line
	}
	removed := 0
	for i := 0; i < len(line) && removed < n; i++ {
		if line[i] == ' ' {
			removed++
		} else {
			// First non-space byte (or tab) ends the dedent region.
			break
		}
	}
	return line[removed:]
}
