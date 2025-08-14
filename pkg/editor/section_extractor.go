package editor

import (
	"fmt"
	"regexp"
	"strings"
)

// extractRelevantSection identifies the specific section of a file that needs to be edited
// Returns the section content, start line, end line, and any error
func extractRelevantSection(content, instructions, filePath string) (string, int, int, error) {
	lines := strings.Split(content, "\n")
	// Use language-agnostic extraction so partial edits work consistently across languages
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
