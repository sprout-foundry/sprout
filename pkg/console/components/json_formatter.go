package components

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// JSONFormatter formats JSON output with One Dark theme styling
type JSONFormatter struct {
	// One Dark color scheme (ANSI codes)
	colors struct {
		reset      string // Reset to default
		background string // Dark background
		foreground string // Light text
		comment    string // Gray for comments/null
		keyword    string // Purple for keys
		string     string // Green for strings
		number     string // Orange for numbers
		boolean    string // Cyan for booleans
		null       string // Gray for null
		bracket    string // White for brackets/braces
		colon      string // Light gray for colons/commas
	}

	indentSize int
	maxDepth   int
}

// NewJSONFormatter creates a new JSON formatter with One Dark styling
func NewJSONFormatter() *JSONFormatter {
	jf := &JSONFormatter{
		indentSize: 2,
		maxDepth:   10,
	}

	// One Dark color scheme
	jf.colors.reset = "\033[0m"
	jf.colors.background = "\033[48;2;40;44;52m"    // Dark background
	jf.colors.foreground = "\033[38;2;171;178;191m" // Light foreground
	jf.colors.comment = "\033[38;2;92;99;112m"      // Gray
	jf.colors.keyword = "\033[38;2;198;120;221m"    // Purple (for keys)
	jf.colors.string = "\033[38;2;152;195;121m"     // Green
	jf.colors.number = "\033[38;2;209;154;102m"     // Orange
	jf.colors.boolean = "\033[38;2;86;182;194m"     // Cyan
	jf.colors.null = "\033[38;2;92;99;112m"         // Gray
	jf.colors.bracket = "\033[38;2;224;227;236m"    // Light white
	jf.colors.colon = "\033[38;2;130;137;151m"      // Light gray

	return jf
}

// FormatJSON takes JSON string or data and returns a beautifully formatted string
func (jf *JSONFormatter) FormatJSON(data interface{}) (string, error) {
	// Handle different input types
	var jsonData interface{}
	var err error

	switch v := data.(type) {
	case string:
		// Try to parse as JSON
		err = json.Unmarshal([]byte(v), &jsonData)
		if err != nil {
			// Not valid JSON, return as-is but with string styling
			return jf.formatPlainString(v), nil
		}
	case []byte:
		err = json.Unmarshal(v, &jsonData)
		if err != nil {
			return "", fmt.Errorf("invalid JSON: %w", err)
		}
	default:
		jsonData = data
	}

	// Format the JSON with syntax highlighting
	return jf.formatValue(jsonData, 0), nil
}

// formatValue recursively formats JSON values with appropriate styling
func (jf *JSONFormatter) formatValue(value interface{}, depth int) string {
	if depth > jf.maxDepth {
		return jf.colors.comment + "..." + jf.colors.reset
	}

	switch v := value.(type) {
	case nil:
		return jf.colors.null + "null" + jf.colors.reset

	case bool:
		return jf.colors.boolean + fmt.Sprintf("%t", v) + jf.colors.reset

	case float64:
		// JSON numbers are always float64 when unmarshaled
		if v == float64(int64(v)) {
			return jf.colors.number + fmt.Sprintf("%.0f", v) + jf.colors.reset
		}
		return jf.colors.number + fmt.Sprintf("%g", v) + jf.colors.reset

	case string:
		return jf.colors.string + `"` + jf.escapeString(v) + `"` + jf.colors.reset

	case []interface{}:
		return jf.formatArray(v, depth)

	case map[string]interface{}:
		return jf.formatObject(v, depth)

	default:
		// Fallback for unknown types
		return jf.colors.foreground + fmt.Sprintf("%v", v) + jf.colors.reset
	}
}

// formatArray formats JSON arrays with proper indentation and styling
func (jf *JSONFormatter) formatArray(arr []interface{}, depth int) string {
	if len(arr) == 0 {
		return jf.colors.bracket + "[]" + jf.colors.reset
	}

	indent := strings.Repeat(" ", depth*jf.indentSize)
	nextIndent := strings.Repeat(" ", (depth+1)*jf.indentSize)

	var parts []string
	parts = append(parts, jf.colors.bracket+"["+jf.colors.reset)

	for i, item := range arr {
		line := nextIndent + jf.formatValue(item, depth+1)
		if i < len(arr)-1 {
			line += jf.colors.colon + "," + jf.colors.reset
		}
		parts = append(parts, line)
	}

	parts = append(parts, indent+jf.colors.bracket+"]"+jf.colors.reset)
	return strings.Join(parts, "\n")
}

// formatObject formats JSON objects with proper indentation and styling
func (jf *JSONFormatter) formatObject(obj map[string]interface{}, depth int) string {
	if len(obj) == 0 {
		return jf.colors.bracket + "{}" + jf.colors.reset
	}

	indent := strings.Repeat(" ", depth*jf.indentSize)
	nextIndent := strings.Repeat(" ", (depth+1)*jf.indentSize)

	var parts []string
	parts = append(parts, jf.colors.bracket+"{"+jf.colors.reset)

	// Sort keys for consistent output
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}

	for i, key := range keys {
		value := obj[key]
		keyStr := jf.colors.keyword + `"` + jf.escapeString(key) + `"` + jf.colors.reset
		valueStr := jf.formatValue(value, depth+1)

		line := nextIndent + keyStr + jf.colors.colon + ": " + jf.colors.reset + valueStr
		if i < len(keys)-1 {
			line += jf.colors.colon + "," + jf.colors.reset
		}
		parts = append(parts, line)
	}

	parts = append(parts, indent+jf.colors.bracket+"}"+jf.colors.reset)
	return strings.Join(parts, "\n")
}

// formatPlainString formats a non-JSON string with basic styling
func (jf *JSONFormatter) formatPlainString(s string) string {
	return jf.colors.foreground + s + jf.colors.reset
}

// escapeString properly escapes strings for JSON display
func (jf *JSONFormatter) escapeString(s string) string {
	// Basic JSON string escaping
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

// DetectAndFormatJSON detects JSON in text and formats it with syntax highlighting
func (jf *JSONFormatter) DetectAndFormatJSON(text string) string {
	// Regex to find JSON-like structures
	jsonRegex := regexp.MustCompile(`(?s)\{[^{}]*(?:\{[^{}]*\}[^{}]*)*\}|\[[^\[\]]*(?:\[[^\[\]]*\][^\[\]]*)*\]`)

	return jsonRegex.ReplaceAllStringFunc(text, func(match string) string {
		formatted, err := jf.FormatJSON(match)
		if err != nil {
			// If formatting fails, return original with basic styling
			return jf.formatPlainString(match)
		}
		return formatted
	})
}

// FormatModelResponse formats common model response patterns with JSON highlighting
func (jf *JSONFormatter) FormatModelResponse(response string) string {
	// Handle common model response patterns
	lines := strings.Split(response, "\n")
	var formattedLines []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			formattedLines = append(formattedLines, "")
			continue
		}

		// Check if line contains JSON
		if strings.Contains(line, "{") || strings.Contains(line, "[") {
			formatted := jf.DetectAndFormatJSON(line)
			formattedLines = append(formattedLines, formatted)
		} else {
			// Regular text with basic styling
			formattedLines = append(formattedLines, jf.colors.foreground+line+jf.colors.reset)
		}
	}

	return strings.Join(formattedLines, "\n")
}

// SetIndentSize sets the indentation size for formatting
func (jf *JSONFormatter) SetIndentSize(size int) *JSONFormatter {
	jf.indentSize = size
	return jf
}

// SetMaxDepth sets the maximum depth for nested objects/arrays
func (jf *JSONFormatter) SetMaxDepth(depth int) *JSONFormatter {
	jf.maxDepth = depth
	return jf
}

// FormatCompact creates a more compact JSON representation
func (jf *JSONFormatter) FormatCompact(data interface{}) (string, error) {
	// Temporarily reduce indent size for compact formatting
	originalIndent := jf.indentSize
	jf.indentSize = 1
	defer func() { jf.indentSize = originalIndent }()

	return jf.FormatJSON(data)
}

// StripColors removes ANSI color codes from formatted text
func (jf *JSONFormatter) StripColors(text string) string {
	// Regex to match ANSI escape sequences
	ansiRegex := regexp.MustCompile(`\033\[[0-9;]*m`)
	return ansiRegex.ReplaceAllString(text, "")
}
