package console

import (
	"bufio"
	"fmt"
	"regexp"
	"strings"
)

// ANSI color codes
const (
	ColorReset  = "\033[0m"
	ColorBold   = "\033[1m"
	ColorDim    = "\033[2m"
	ColorItalic = "\033[3m"
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
}

// NewMarkdownFormatter creates a new markdown formatter
func NewMarkdownFormatter(enableColors, enableInline bool) *MarkdownFormatter {
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

	// Process line by line for better formatting
	var result strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(text))
	
	inCodeBlock := false
	inCodeBlockLang := ""
	
	for scanner.Scan() {
		line := scanner.Text()
		
		// Handle code blocks
		if strings.HasPrefix(line, "```") {
			if !inCodeBlock {
				// Start code block
				inCodeBlock = true
				lang := strings.TrimSpace(line[3:])
				inCodeBlockLang = lang
				result.WriteString(fmt.Sprintf("%s%s%s\n", ColorDim, ColorBold, "┌─ Code Block"))
				if lang != "" {
					result.WriteString(fmt.Sprintf("%s│ Language: %s%s\n", ColorDim, lang, ColorReset))
				}
				result.WriteString(fmt.Sprintf("%s│%s\n", ColorDim, ColorReset))
			} else {
				// End code block
				inCodeBlock = false
				result.WriteString(fmt.Sprintf("%s└─ End Code Block%s\n", ColorDim, ColorReset))
			}
			continue
		}
		
		if inCodeBlock {
			// Inside code block - format with syntax highlighting hints
			result.WriteString(fmt.Sprintf("%s│ %s%s\n", ColorDim, f.formatCodeLine(line, inCodeBlockLang), ColorReset))
			continue
		}
		
		// Process regular markdown line
		formattedLine := f.formatMarkdownLine(line)
		result.WriteString(formattedLine + "\n")
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
		// Simple list style: color the bullet
		bulletPattern := `^(\s*)([-*+])(\s+)(.*)$`
		re := regexp.MustCompile(bulletPattern)
		if matches := re.FindStringSubmatch(line); len(matches) > 0 {
			return fmt.Sprintf("%s%s%s%s%s", matches[1], ColorGreen+matches[2], ColorReset+matches[3], matches[4], ColorReset)
		}
	}
	
	// Horizontal rule
	if strings.TrimSpace(line) == "---" || strings.TrimSpace(line) == "***" {
		return fmt.Sprintf("%s%s%s", ColorDim, strings.Repeat("─", 40), ColorReset)
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
	
	// Italic text (*text* or _text_)
	italicRegex := regexp.MustCompile(`\*(.*?)\*|_(.*?)_`)
	text = italicRegex.ReplaceAllStringFunc(text, func(match string) string {
		// Avoid matching bold text
		if strings.HasPrefix(match, "**") || strings.HasPrefix(match, "__") {
			return match
		}
		var content string
		content = match[1 : len(match)-1]
		return ColorItalic + content + ColorReset
	})
	
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

// IsLikelyMarkdown checks if text contains markdown patterns
func IsLikelyMarkdown(text string) bool {
	// Check for headers (# ## ### etc.)
	if strings.Contains(text, "#") {
		// Look for lines that start with #
		lines := strings.Split(text, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") {
				return true
			}
		}
	}
	
	markdownPatterns := []string{
		"`",        // Inline code
		"```",      // Code blocks
		"**",       // Bold
		"[", "](",  // Links
		"- ",       // Lists
		"> ",       // Blockquotes
	}
	
	for _, pattern := range markdownPatterns {
		if strings.Contains(text, pattern) {
			return true
		}
	}
	
	return false
}