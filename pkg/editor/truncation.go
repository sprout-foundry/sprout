package editor

import (
	"path/filepath"
	"strings"
)

// isIncompleteTruncatedResponse checks if a response is genuinely incomplete/truncated
// This is much more conservative than the old IsPartialResponse to avoid false positives
func isIncompleteTruncatedResponse(code, filename string) bool {
	// Check for obvious truncation markers that indicate the LLM gave up
	truncationMarkers := []string{
		"... (rest of file unchanged)",
		"... rest of the file ...",
		"... content truncated ...",
		"... full file content ...",
		"[TRUNCATED]",
		"[INCOMPLETE]",
		"// ... (truncated)",
	}

	// Only flag markers that appear outside of string literals to avoid
	// false positives when editing code that defines these markers.
	for _, marker := range truncationMarkers {
		if containsMarkerOutsideQuotes(code, marker) {
			return true
		}
	}

	// Check if the file appears to end abruptly (no proper closing braces for Go files)
	if strings.HasSuffix(filename, ".go") {
		// Count opening and closing braces - if severely unbalanced, likely truncated
		openBraces := strings.Count(code, "{")
		closeBraces := strings.Count(code, "}")

		// Allow some imbalance for partial code, but large imbalances suggest truncation
		if openBraces > closeBraces+5 {
			return true
		}

		// Check if it looks like it ends mid-function (very short and ends with incomplete syntax)
		lines := strings.Split(strings.TrimSpace(code), "\n")
		if len(lines) < 10 { // Very short response
			lastLine := strings.TrimSpace(lines[len(lines)-1])
			// Ends with incomplete syntax patterns
			incompleteSyntax := []string{"{", "if ", "for ", "func ", "var ", "const "}
			for _, syntax := range incompleteSyntax {
				if strings.HasSuffix(lastLine, syntax) {
					return true
				}
			}
		}
	}

	// Default to accepting the response - better to process partial code than loop forever
	return false
}

// containsMarkerOutsideQuotes returns true if marker appears outside of
// simple string literals (double-quoted or backtick raw strings). This is a
// heuristic to avoid flagging markers that are present as string constants
// inside source code.
func containsMarkerOutsideQuotes(text, marker string) bool {
	// Fast-path reject
	if !strings.Contains(strings.ToLower(text), strings.ToLower(marker)) {
		return false
	}
	// Scan line by line and track quote state per line
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		// Track whether we're inside a double-quoted or backtick raw string
		inDouble := false
		inBacktick := false
		for i := 0; i < len(line); i++ {
			ch := line[i]
			// Toggle backtick raw string state
			if ch == '`' && !inDouble {
				inBacktick = !inBacktick
			}
			// Toggle double quote state (ignore escaped quotes)
			if ch == '"' && !inBacktick {
				// check escape
				if i == 0 || line[i-1] != '\\' {
					inDouble = !inDouble
				}
			}
			// Check for marker at this position when not in a string
			if !inDouble && !inBacktick {
				if i+len(marker) <= len(line) && strings.EqualFold(line[i:i+len(marker)], marker) {
					return true
				}
			}
		}
	}
	return false
}

// isIntentionalPartialCode determines if partial code is intentional vs truncated
func isIntentionalPartialCode(code, instructions string) bool {
	instructionsLower := strings.ToLower(instructions)

	// If instructions specifically ask for partial/targeted changes, accept partial code
	partialIntentKeywords := []string{
		"add function", "add method", "add import", "add constant",
		"modify function", "update function", "change function",
		"add to", "insert", "create function", "new function",
		"add the following", "implement the following",
	}

	for _, keyword := range partialIntentKeywords {
		if strings.Contains(instructionsLower, keyword) {
			return true
		}
	}

	// If the code looks structurally complete for what was asked
	lines := strings.Split(strings.TrimSpace(code), "\n")
	if len(lines) == 0 {
		return false
	}

	// Check if it's a complete function/method/struct
	firstLine := strings.TrimSpace(lines[0])
	lastLine := strings.TrimSpace(lines[len(lines)-1])

	// For Go code, check if it looks like a complete structure
	if strings.HasPrefix(firstLine, "func ") {
		// Should end with } or similar
		return strings.HasSuffix(lastLine, "}") || strings.Contains(lastLine, "return")
	}

	if strings.HasPrefix(firstLine, "type ") {
		// Should end with } for structs/interfaces
		return strings.HasSuffix(lastLine, "}")
	}

	if strings.HasPrefix(firstLine, "import ") || strings.Contains(firstLine, `"`) {
		// Import statements are naturally short and partial
		return true
	}

	if strings.HasPrefix(firstLine, "const ") || strings.HasPrefix(firstLine, "var ") {
		// Variable/constant declarations are naturally short
		return true
	}

	// If none of the above, be conservative and assume it's intentional
	// (better to handle partial code than loop forever)
	return true
}

// getLanguageFromExtension infers the programming language from the file extension.
func getLanguageFromExtension(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js", ".ts":
		return "javascript"
	case ".java":
		return "java"
	case ".c", ".cpp", ".h":
		return "c"
	case ".sh":
		return "bash"
	case ".md":
		return "markdown"
	case ".json":
		return "json"
	case ".xml":
		return "xml"
	case ".html":
		return "html"
	case ".css":
		return "css"
	case ".yaml", ".yml":
		return "yaml"
	case ".sql":
		return "sql"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".rs":
		return "rust"
	case ".swift":
		return "swift"
	case ".kt":
		return "kotlin"
	case ".cs":
		return "csharp"
	default:
		return "text"
	}
}
