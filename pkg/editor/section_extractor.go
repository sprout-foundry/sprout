package editor

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// extractRelevantSection identifies the specific section of a file that needs to be edited
// Returns the section content, start line, end line, and any error
func extractRelevantSection(content, instructions, filePath string) (string, int, int, error) {
	lines := strings.Split(content, "\n")
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".go":
		// Prefer AST-backed span detection; fallback to regex/heuristics
		if sec, s, e, err := extractGoSectionAST(strings.Join(lines, "\n"), instructions); err == nil && sec != "" {
			return sec, s, e, nil
		}
		if sec, s, e, err := extractGoSection(lines, instructions); err == nil {
			return sec, s, e, nil
		}
	case ".py":
		if sec, s, e, err := extractPythonSection(lines, instructions); err == nil {
			return sec, s, e, nil
		}
	case ".js", ".ts":
		if sec, s, e, err := extractJSSection(lines, instructions); err == nil && sec != "" {
			return sec, s, e, nil
		}
		if sec, s, e, err := extractCLikeSection(lines, instructions); err == nil {
			return sec, s, e, nil
		}
	case ".java", ".c", ".cpp", ".h", ".cs", ".swift":
		if sec, s, e, err := extractCLikeSection(lines, instructions); err == nil {
			return sec, s, e, nil
		}
	case ".rb":
		if sec, s, e, err := extractRubySection(lines, instructions); err == nil {
			return sec, s, e, nil
		}
	case ".php":
		if sec, s, e, err := extractPHPSection(lines, instructions); err == nil {
			return sec, s, e, nil
		}
	case ".rs":
		if sec, s, e, err := extractRustSection(lines, instructions); err == nil && sec != "" {
			return sec, s, e, nil
		}
		if sec, s, e, err := extractCLikeSection(lines, instructions); err == nil {
			return sec, s, e, nil
		}
	case ".md", ".markdown", ".txt":
		if sec, s, e, err := extractMarkdownSection(lines, instructions); err == nil {
			return sec, s, e, nil
		}
	}
	// Fallback
	return extractGenericSection(lines, instructions)
}

// extractGoSection extracts relevant Go code sections (functions, types, etc.)
func extractGoSection(lines []string, instructions string) (string, int, int, error) {
	instructionsLower := strings.ToLower(instructions)

	// Handle "top of file" requests specially
	if strings.Contains(instructionsLower, "top of") || strings.Contains(instructionsLower, "beginning of") ||
		strings.Contains(instructionsLower, "start of") {
		// Return the first few lines of the file including package declaration and imports
		maxLines := 10 // capture roughly package + imports
		if len(lines) == 0 {
			return "", 0, 0, fmt.Errorf("empty file")
		}
		endLine := maxLines - 1
		if endLine >= len(lines) {
			endLine = len(lines) - 1
		}
		if endLine < 0 { // safety
			endLine = 0
		}
		section := strings.Join(lines[0:endLine+1], "\n")
		return section, 0, endLine, nil
	}

	// Try to find function names mentioned in instructions
	funcPattern := regexp.MustCompile(`func\s+(\w+)`)
	typePattern := regexp.MustCompile(`type\s+(\w+)`)

	for i, line := range lines {
		// Check for function declarations
		if matches := funcPattern.FindStringSubmatch(line); len(matches) > 1 {
			funcName := strings.ToLower(matches[1])
			if strings.Contains(instructionsLower, funcName) {
				// Find the end of this function
				endLine := findGoFunctionEnd(lines, i)
				section := strings.Join(lines[i:endLine+1], "\n")
				return section, i, endLine, nil
			}
		}

		// Check for type declarations
		if matches := typePattern.FindStringSubmatch(line); len(matches) > 1 {
			typeName := strings.ToLower(matches[1])
			if strings.Contains(instructionsLower, typeName) {
				// Find the end of this type
				endLine := findGoTypeEnd(lines, i)
				section := strings.Join(lines[i:endLine+1], "\n")
				return section, i, endLine, nil
			}
		}
	}

	// If no specific function/type found, try to find a logical block
	return extractGenericSection(lines, instructions)
}

// extractPythonSection tries to anchor on def/class blocks using indentation
func extractPythonSection(lines []string, instructions string) (string, int, int, error) {
	lower := strings.ToLower(instructions)
	// Top-of-file requests
	if strings.Contains(lower, "top of") || strings.Contains(lower, "beginning of") || strings.Contains(lower, "start of") {
		end := 15
		if end >= len(lines) {
			end = len(lines) - 1
		}
		if end < 0 {
			end = 0
		}
		return strings.Join(lines[0:end+1], "\n"), 0, end, nil
	}
	reDef := regexp.MustCompile(`^\s*def\s+([A-Za-z0-9_]+)\s*\(`)
	reClass := regexp.MustCompile(`^\s*class\s+([A-Za-z0-9_]+)\s*[:\(]`)
	for i, line := range lines {
		if m := reDef.FindStringSubmatch(line); len(m) > 1 {
			name := strings.ToLower(m[1])
			if strings.Contains(lower, name) {
				return expandPythonBlock(lines, i), i, pythonBlockEnd(lines, i), nil
			}
		}
		if m := reClass.FindStringSubmatch(line); len(m) > 1 {
			name := strings.ToLower(m[1])
			if strings.Contains(lower, name) {
				return expandPythonBlock(lines, i), i, pythonBlockEnd(lines, i), nil
			}
		}
	}
	// Fallback: choose best match line, then expand to enclosing def/class
	sec, start, end, err := extractGenericSection(lines, instructions)
	if err != nil {
		return "", 0, 0, err
	}
	for i := start; i >= 0; i-- {
		if reDef.MatchString(lines[i]) || reClass.MatchString(lines[i]) {
			s := i
			e := pythonBlockEnd(lines, i)
			return strings.Join(lines[s:e+1], "\n"), s, e, nil
		}
	}
	return sec, start, end, nil
}

func indentLevel(s string) int {
	n := 0
	for _, ch := range s {
		if ch == ' ' {
			n++
		} else if ch == '\t' {
			n += 4
		} else {
			break
		}
	}
	return n
}

func pythonBlockEnd(lines []string, start int) int {
	base := indentLevel(lines[start])
	for i := start + 1; i < len(lines); i++ {
		ln := lines[i]
		if strings.TrimSpace(ln) == "" {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(ln), "@") { // decorator lines inside class
			continue
		}
		if indentLevel(ln) <= base {
			return i - 1
		}
	}
	return len(lines) - 1
}

func expandPythonBlock(lines []string, start int) string {
	end := pythonBlockEnd(lines, start)
	return strings.Join(lines[start:end+1], "\n")
}

// extractJSSection attempts to capture function/class blocks in JS/TS with better anchors
func extractJSSection(lines []string, instructions string) (string, int, int, error) {
	lower := strings.ToLower(instructions)
	// Top-of-file
	if strings.Contains(lower, "top of") || strings.Contains(lower, "beginning of") || strings.Contains(lower, "start of") {
		end := 15
		if end >= len(lines) {
			end = len(lines) - 1
		}
		if end < 0 {
			end = 0
		}
		return strings.Join(lines[0:end+1], "\n"), 0, end, nil
	}
	// Patterns: function foo() { ... }, const foo = () => { ... }, class Name { ... }
	reFunc := regexp.MustCompile(`(?i)^\s*(?:export\s+)?function\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	reArrow := regexp.MustCompile(`(?i)^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*\([^)]*\)\s*=>\s*\{`)
	reClass := regexp.MustCompile(`(?i)^\s*(?:export\s+)?class\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	// search for named target first
	for i, ln := range lines {
		if m := reFunc.FindStringSubmatch(ln); len(m) > 1 {
			name := strings.ToLower(m[1])
			if strings.Contains(lower, name) {
				e := matchBracesEnd(lines, i)
				return strings.Join(lines[i:e+1], "\n"), i, e, nil
			}
		}
		if m := reArrow.FindStringSubmatch(ln); len(m) > 1 {
			name := strings.ToLower(m[1])
			if strings.Contains(lower, name) {
				e := matchBracesEnd(lines, i)
				return strings.Join(lines[i:e+1], "\n"), i, e, nil
			}
		}
		if m := reClass.FindStringSubmatch(ln); len(m) > 1 {
			name := strings.ToLower(m[1])
			if strings.Contains(lower, name) {
				e := matchBracesEnd(lines, i)
				return strings.Join(lines[i:e+1], "\n"), i, e, nil
			}
		}
	}
	// fallback to generic C-like
	return extractCLikeSection(lines, instructions)
}

// extractCLikeSection anchors to function/class with brace matching
func extractCLikeSection(lines []string, instructions string) (string, int, int, error) {
	lower := strings.ToLower(instructions)
	if strings.Contains(lower, "top of") || strings.Contains(lower, "beginning of") || strings.Contains(lower, "start of") {
		end := 15
		if end >= len(lines) {
			end = len(lines) - 1
		}
		if end < 0 {
			end = 0
		}
		return strings.Join(lines[0:end+1], "\n"), 0, end, nil
	}
	// Rough function/class patterns
	reFunc := regexp.MustCompile(`(?i)^\s*([A-Za-z_][A-Za-z0-9_\*\s<>:,\[\]]+)\s+([A-Za-z_][A-Za-z0-9_]*)\s*\([^;]*\)\s*\{`)
	reClass := regexp.MustCompile(`(?i)^\s*(class|struct|interface)\s+([A-Za-z_][A-Za-z0-9_]*)[^{]*\{`)
	// Try to find named targets first
	for i, ln := range lines {
		if m := reFunc.FindStringSubmatch(ln); len(m) > 2 {
			name := strings.ToLower(m[2])
			if strings.Contains(lower, name) {
				e := matchBracesEnd(lines, i)
				return strings.Join(lines[i:e+1], "\n"), i, e, nil
			}
		}
		if m := reClass.FindStringSubmatch(ln); len(m) > 2 {
			name := strings.ToLower(m[2])
			if strings.Contains(lower, name) {
				e := matchBracesEnd(lines, i)
				return strings.Join(lines[i:e+1], "\n"), i, e, nil
			}
		}
	}
	// Fallback: generic match, then expand to surrounding brace block
	sec, start, end, err := extractGenericSection(lines, instructions)
	if err != nil {
		return "", 0, 0, err
	}
	// find block start by scanning backwards for '{'
	s := start
	found := false
	braceDepth := 0
	for i := start; i >= 0; i-- {
		if strings.Contains(lines[i], "{") {
			s = i
			found = true
			break
		}
	}
	if found {
		e := s
		for i := s; i < len(lines); i++ {
			for _, ch := range lines[i] {
				if ch == '{' {
					braceDepth++
				}
				if ch == '}' {
					braceDepth--
					if braceDepth == 0 {
						e = i
						return strings.Join(lines[s:e+1], "\n"), s, e, nil
					}
				}
			}
		}
	}
	return sec, start, end, nil
}

// extractMarkdownSection targets a heading-delimited block relevant to the instructions.
// It looks for the best-matching heading based on instruction keywords and returns that section.
func extractMarkdownSection(lines []string, instructions string) (string, int, int, error) {
	lowerInstr := strings.ToLower(instructions)
	words := strings.Fields(lowerInstr)
	isHeading := func(s string) bool {
		t := strings.TrimSpace(s)
		if strings.HasPrefix(t, "#") {
			i := 0
			for i < len(t) && t[i] == '#' {
				i++
			}
			return i > 0 && i < len(t) && t[i] == ' '
		}
		return false
	}
	type hdr struct {
		idx   int
		score int
	}
	var headers []hdr
	for i, ln := range lines {
		if isHeading(ln) {
			s := strings.ToLower(ln)
			sc := 0
			for _, w := range words {
				if len(w) < 3 {
					continue
				}
				if strings.Contains(s, w) {
					sc++
				}
			}
			headers = append(headers, hdr{idx: i, score: sc})
		}
	}
	if len(headers) == 0 {
		return extractGenericSection(lines, instructions)
	}
	best := headers[0]
	for _, h := range headers[1:] {
		if h.score > best.score {
			best = h
		}
	}
	start := best.idx
	end := len(lines) - 1
	for i := start + 1; i < len(lines); i++ {
		if isHeading(lines[i]) {
			end = i - 1
			break
		}
	}
	if start < 0 {
		start = 0
	}
	if end < start {
		if start+40 < len(lines) {
			end = start + 40
		} else {
			end = len(lines) - 1
		}
	}
	section := strings.Join(lines[start:end+1], "\n")
	return section, start, end, nil
}

func matchBracesEnd(lines []string, start int) int {
	depth := 0
	started := false
	for i := start; i < len(lines); i++ {
		for _, ch := range lines[i] {
			if ch == '{' {
				depth++
				started = true
			}
			if ch == '}' {
				depth--
				if started && depth == 0 {
					return i
				}
			}
		}
	}
	if start+20 < len(lines) {
		return start + 20
	}
	return len(lines) - 1
}

// extractRubySection anchors to def/class/module ... end blocks
func extractRubySection(lines []string, instructions string) (string, int, int, error) {
	lower := strings.ToLower(instructions)
	if strings.Contains(lower, "top of") || strings.Contains(lower, "beginning of") || strings.Contains(lower, "start of") {
		end := 15
		if end >= len(lines) {
			end = len(lines) - 1
		}
		if end < 0 {
			end = 0
		}
		return strings.Join(lines[0:end+1], "\n"), 0, end, nil
	}
	reHdr := regexp.MustCompile(`^\s*(def|class|module)\s+([A-Za-z0-9_\?\!:]+)`) // capture name-ish
	for i := 0; i < len(lines); i++ {
		if m := reHdr.FindStringSubmatch(lines[i]); len(m) > 2 {
			name := strings.ToLower(m[2])
			if strings.Contains(lower, name) {
				e := rubyBlockEnd(lines, i)
				return strings.Join(lines[i:e+1], "\n"), i, e, nil
			}
		}
	}
	sec, start, end, err := extractGenericSection(lines, instructions)
	if err != nil {
		return "", 0, 0, err
	}
	for i := start; i >= 0; i-- {
		if reHdr.MatchString(lines[i]) {
			e := rubyBlockEnd(lines, i)
			return strings.Join(lines[i:e+1], "\n"), i, e, nil
		}
	}
	return sec, start, end, nil
}

func rubyBlockEnd(lines []string, start int) int {
	depth := 0
	for i := start; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if strings.HasPrefix(t, "def ") || strings.HasPrefix(t, "class ") || strings.HasPrefix(t, "module ") || strings.HasSuffix(t, " do") {
			depth++
		}
		if t == "end" || strings.HasPrefix(t, "end ") {
			depth--
			if depth <= 0 {
				return i
			}
		}
	}
	if start+20 < len(lines) {
		return start + 20
	}
	return len(lines) - 1
}

// extractPHPSection uses brace-based block detection for functions/classes
func extractPHPSection(lines []string, instructions string) (string, int, int, error) {
	lower := strings.ToLower(instructions)
	if strings.Contains(lower, "top of") || strings.Contains(lower, "beginning of") || strings.Contains(lower, "start of") {
		end := 15
		if end >= len(lines) {
			end = len(lines) - 1
		}
		if end < 0 {
			end = 0
		}
		return strings.Join(lines[0:end+1], "\n"), 0, end, nil
	}
	reFunc := regexp.MustCompile(`^\s*function\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	reClass := regexp.MustCompile(`^\s*class\s+([A-Za-z_][A-Za-z0-9_]*)`)
	for i := 0; i < len(lines); i++ {
		if m := reFunc.FindStringSubmatch(lines[i]); len(m) > 1 {
			name := strings.ToLower(m[1])
			if strings.Contains(lower, name) {
				e := matchBracesEnd(lines, i)
				return strings.Join(lines[i:e+1], "\n"), i, e, nil
			}
		}
		if m := reClass.FindStringSubmatch(lines[i]); len(m) > 1 {
			name := strings.ToLower(m[1])
			if strings.Contains(lower, name) {
				e := matchBracesEnd(lines, i)
				return strings.Join(lines[i:e+1], "\n"), i, e, nil
			}
		}
	}
	// Fallback
	return extractCLikeSection(lines, instructions)
}

// extractRustSection anchors to fn/module blocks in Rust using brace matching
func extractRustSection(lines []string, instructions string) (string, int, int, error) {
	lower := strings.ToLower(instructions)
	if strings.Contains(lower, "top of") || strings.Contains(lower, "beginning of") || strings.Contains(lower, "start of") {
		end := 15
		if end >= len(lines) {
			end = len(lines) - 1
		}
		if end < 0 {
			end = 0
		}
		return strings.Join(lines[0:end+1], "\n"), 0, end, nil
	}
	reFn := regexp.MustCompile(`(?m)^\s*fn\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	reMod := regexp.MustCompile(`(?m)^\s*mod\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	for i, ln := range lines {
		if m := reFn.FindStringSubmatch(ln); len(m) > 1 {
			name := strings.ToLower(m[1])
			if strings.Contains(lower, name) {
				e := matchBracesEnd(lines, i)
				return strings.Join(lines[i:e+1], "\n"), i, e, nil
			}
		}
		if m := reMod.FindStringSubmatch(ln); len(m) > 1 {
			name := strings.ToLower(m[1])
			if strings.Contains(lower, name) {
				e := matchBracesEnd(lines, i)
				return strings.Join(lines[i:e+1], "\n"), i, e, nil
			}
		}
	}
	return "", 0, 0, fmt.Errorf("no rust section identified")
}

// findGoFunctionEnd finds the end line of a Go function starting at startLine
func findGoFunctionEnd(lines []string, startLine int) int {
	braceCount := 0
	foundOpenBrace := false

	for i := startLine; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])

		for _, char := range line {
			if char == '{' {
				braceCount++
				foundOpenBrace = true
			} else if char == '}' {
				braceCount--
				if foundOpenBrace && braceCount == 0 {
					return i
				}
			}
		}
	}

	// If we couldn't find the end, return a reasonable default
	return startLine + 20 // Arbitrary limit
}

// findGoTypeEnd finds the end line of a Go type declaration starting at startLine
func findGoTypeEnd(lines []string, startLine int) int {
	line := strings.TrimSpace(lines[startLine])

	// If it's a simple type (no braces), it's just one line
	if !strings.Contains(line, "{") {
		return startLine
	}

	// Otherwise, find the matching closing brace
	return findGoFunctionEnd(lines, startLine)
}

// extractGenericSection extracts a relevant section using simple heuristics
func extractGenericSection(lines []string, instructions string) (string, int, int, error) {
	instructionsLower := strings.ToLower(instructions)
	words := strings.Fields(instructionsLower)

	// Look for lines that contain keywords from the instructions
	bestMatch := -1
	bestScore := 0

	for i, line := range lines {
		lineLower := strings.ToLower(line)
		score := 0

		for _, word := range words {
			if len(word) > 3 && strings.Contains(lineLower, word) {
				score++
			}
		}

		if score > bestScore {
			bestScore = score
			bestMatch = i
		}
	}

	if bestMatch == -1 {
		return "", 0, 0, fmt.Errorf("could not find relevant section")
	}

	// Extract a reasonable context around the best match
	start := bestMatch - 5
	if start < 0 {
		start = 0
	}

	end := bestMatch + 15
	if end >= len(lines) {
		end = len(lines) - 1
	}

	section := strings.Join(lines[start:end+1], "\n")
	return section, start, end, nil
}
