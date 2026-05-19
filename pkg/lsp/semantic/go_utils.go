package semantic

// isIdentRune returns true if r is a valid Go identifier rune.
func isIdentRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

// goLineColToOffset converts a 1-based line:col to a 0-based byte offset in content.
func goLineColToOffset(content string, line, col int) int {
	if line <= 0 {
		line = 1
	}
	if col <= 0 {
		col = 1
	}
	currentLine := 1
	lineStart := 0
	for i, ch := range content {
		if currentLine == line {
			offset := lineStart + col - 1
			if offset > len(content) {
				return len(content)
			}
			return offset
		}
		if ch == '\n' {
			currentLine++
			lineStart = i + 1
		}
	}
	return len(content)
}
