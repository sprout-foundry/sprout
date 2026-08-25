package console

import (
	"fmt"
	"strings"
)

// StreamingMarkdownFormatter applies markdown formatting line-by-line as
// text arrives, maintaining cross-line state (code blocks, tables) so that
// multi-line constructs render correctly without any cursor manipulation.
//
// This replaces the old clear-and-reprint approach in FinalizeAtTurnEnd,
// which was disabled because the formatter's output row count never matched
// the streamed row count, causing cursor-clobbering. By formatting each
// line before it reaches the terminal, clobbering is structurally impossible.
//
// Usage: feed complete lines (without trailing \n) via ProcessLine. Each
// call returns the formatted output for that line (may be empty for
// consumed-only lines like code-fence boundaries, or multiple lines for
// a table flush). Call Flush at segment/turn end to emit any pending
// buffered table rows.
type StreamingMarkdownFormatter struct {
	formatter *MarkdownFormatter

	inCodeBlock   bool
	codeBlockLang string

	tableBuffer []string
}

// NewStreamingMarkdownFormatter wraps an existing MarkdownFormatter with
// streaming state.
func NewStreamingMarkdownFormatter(f *MarkdownFormatter) *StreamingMarkdownFormatter {
	return &StreamingMarkdownFormatter{formatter: f}
}

// ProcessLine formats one complete line (without trailing newline).
// Returns formatted output ending with \n. May return:
//   - "" — line was consumed without output (code fence open/close, table row buffered)
//   - single line + \n — regular markdown line or code-block content
//   - multiple lines — table flush triggered by a non-table line after buffered rows
func (s *StreamingMarkdownFormatter) ProcessLine(line string) string {
	// Code block fence handling.
	if strings.HasPrefix(line, "```") {
		if !s.inCodeBlock {
			s.inCodeBlock = true
			s.codeBlockLang = strings.TrimSpace(line[3:])
			if s.codeBlockLang != "" {
				return fmt.Sprintf("%s──── %s ────%s\n", ColorDim, s.codeBlockLang, ColorReset)
			}
			return ""
		}
		s.inCodeBlock = false
		s.codeBlockLang = ""
		return ""
	}

	// Inside a code block — emit with the dim gutter and syntax highlighting.
	if s.inCodeBlock {
		return fmt.Sprintf("%s│ %s%s\n", ColorDim, s.formatter.formatCodeLine(line, s.codeBlockLang), ColorReset)
	}

	// Table detection: buffer pipe-delimited lines until the table ends.
	if strings.HasPrefix(line, "|") {
		s.tableBuffer = append(s.tableBuffer, line)
		return ""
	}

	// A non-table line after buffered table rows — flush the table first.
	var result string
	if len(s.tableBuffer) > 0 {
		result = s.formatter.flushTable(s.tableBuffer)
		s.tableBuffer = nil
	}

	result += s.formatter.formatMarkdownLine(line) + "\n"
	return result
}

// Flush emits any pending buffered state (incomplete table rows). Call
// at segment/turn end so buffered table content is not lost.
func (s *StreamingMarkdownFormatter) Flush() string {
	if len(s.tableBuffer) > 0 {
		out := s.formatter.flushTable(s.tableBuffer)
		s.tableBuffer = nil
		return out
	}
	return ""
}

// Reset clears all streaming state for a new segment.
func (s *StreamingMarkdownFormatter) Reset() {
	s.inCodeBlock = false
	s.codeBlockLang = ""
	s.tableBuffer = nil
}

// InTable reports whether table rows are currently buffered (used by
// callers to decide whether to emit a partial line raw vs. through the
// formatter on segment boundaries).
func (s *StreamingMarkdownFormatter) InTable() bool {
	return len(s.tableBuffer) > 0
}
