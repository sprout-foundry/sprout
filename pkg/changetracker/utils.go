package changetracker

import (
	"strings"
)

// wrapAndIndent wraps the input text at the specified width and indents each line with indentSpaces spaces.
func wrapAndIndent(text string, width int, indentSpaces int) string {
	indent := strings.Repeat(" ", indentSpaces)
	var result strings.Builder
	words := strings.Fields(text)
	lineLen := 0
	result.WriteString(indent)
	for i, word := range words {
		if lineLen+len(word)+1 > width {
			result.WriteString("\n" + indent)
			lineLen = 0
		} else if i != 0 {
			result.WriteString(" ")
			lineLen++
		}
		result.WriteString(word)
		lineLen += len(word)
	}
	return result.String()
}
