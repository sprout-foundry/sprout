// Package console: code line syntax highlighting for Go, Python, Bash, JSON, YAML, JavaScript, TypeScript, and generic (split from markdown_formatter.go)
package console

import (
	"regexp"
	"strings"
)

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
