// Package console: inline markdown element formatting (bold, italic, code, links) and underscore italic boundary detection (split from markdown_formatter.go)
package console

import (
	"fmt"
	"regexp"
	"strings"
)

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
