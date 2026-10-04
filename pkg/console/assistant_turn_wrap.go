package console

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

// minWrapColumns is the narrowest text column worth word-wrapping into;
// below it the terminal's own hard wrap reads no worse.
const minWrapColumns = 20

var hangingIndentPattern = regexp.MustCompile(`^(\s*(?:[-*+•]\s+|\d+[.)]\s+|│ )?)`)

// writeWrappedLine prints one formatted line under the renderer's indent,
// word-wrapped to the terminal width. Left to the terminal, a long line
// breaks mid-word and its continuation rows start at column 0, outside the
// indent; here continuation rows keep the indent plus the line's own
// hanging indent (list marker, code gutter). Returns the physical rows
// written. Caller must hold outputMu.
func (r *AssistantTurnRenderer) writeWrappedLine(line string) int {
	body, newline := strings.CutSuffix(line, "\n")
	indentWidth := displayWidth(r.indent)
	if r.terminalWidth <= 0 || ansi.StringWidth(body) <= r.terminalWidth-indentWidth {
		fmt.Print(r.indent, line)
		return 1
	}
	hang := hangingIndent(body)
	limit := r.terminalWidth - indentWidth - len(hang)
	if limit < minWrapColumns {
		fmt.Print(r.indent, line)
		return physicalRows(indentWidth+ansi.StringWidth(body), r.terminalWidth)
	}
	rows := strings.Split(ansi.Wrap(body, limit, ""), "\n")
	for i, row := range rows {
		if i == 0 {
			fmt.Print(r.indent, row)
			continue
		}
		fmt.Print("\n", r.indent, hang, row)
	}
	if newline {
		fmt.Print("\n")
	}
	return len(rows)
}

// hangingIndent is the blank prefix that lines up a wrapped line's
// continuation rows with the text after its list marker or code gutter.
func hangingIndent(line string) string {
	m := hangingIndentPattern.FindString(stripANSIEscapeCodes(line))
	return strings.Repeat(" ", displayWidth(m))
}

// WrapIndented word-wraps each line of text to cols columns, prefixing
// every resulting row with indent.
func WrapIndented(text, indent string, cols int) string {
	limit := cols - displayWidth(indent)
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		rows := []string{line}
		if limit >= minWrapColumns {
			rows = strings.Split(ansi.Wrap(line, limit, ""), "\n")
		}
		for _, row := range rows {
			b.WriteString(indent + row + "\n")
		}
	}
	return b.String()
}

// WrapHanging word-wraps text to cols columns behind prefix, indenting the
// continuation rows to line up under the text rather than the prefix. Rows
// are joined with "\n" and carry no trailing newline.
func WrapHanging(prefix, text string, cols int) string {
	width := displayWidth(stripANSIEscapeCodes(prefix))
	limit := cols - width
	if limit < minWrapColumns {
		return prefix + text
	}
	rows := strings.Split(ansi.Wrap(text, limit, ""), "\n")
	return prefix + strings.Join(rows, "\n"+strings.Repeat(" ", width))
}

// StdoutColumns is the terminal width for laying out output (80 when
// stdout is not a terminal).
func StdoutColumns() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		return w
	}
	return 80
}
