package embedding

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// pyFuncRegex matches function declarations: def name( or async def name(
// Group 1: indentation (spaces/tabs before def/async)
// Group 2: function name
var pyFuncRegex = regexp.MustCompile(`(?m)^((?:\s*(?:async\s+)?)def\s+(\w+))\s*\(`)

// pyClassRegex matches class declarations: class Name( or class Name:
// Group 1: indentation (spaces/tabs before class)
// Group 2: class name
var pyClassRegex = regexp.MustCompile(`(?m)^(\s*)class\s+(\w+)`)

// pyDecoratorRegex matches decorator lines: @something
var pyDecoratorRegex = regexp.MustCompile(`^\s*@`)

// pyTestFilePattern matches common test file naming conventions.
var pyTestFilePattern = regexp.MustCompile(`_test\.py$|test_.*\.py$`)

// ExtractPyFile parses a Python source file and extracts code units
// (functions, methods, classes) as CodeUnit values.
// Test functions (prefixed with test_) are excluded by default; use
// WithIncludeTests to change this.
func ExtractPyFile(path string, opts ...ExtractOption) ([]CodeUnit, error) {
	cfg := &ExtractConfig{}
	cfg.ApplyOptions(opts...)

	// Skip test files unless explicitly included.
	if !cfg.IncludeTests && pyTestFilePattern.MatchString(filepath.Base(path)) {
		return nil, nil
	}

	srcBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("embedding: read %s: %w", path, err)
	}

	src := string(srcBytes)
	lines := strings.Split(src, "\n")

	var units []CodeUnit

	// Track which line ranges are consumed by class bodies
	// to avoid double-extracting methods as standalone functions.
	var consumedRanges []lineRange

	// First pass: extract classes (they contain methods).
	classMatches := findPyDefMatches(pyClassRegex, lines)
	for _, m := range classMatches {
		indentLevel := getIndentLevel(lines[m.startLine-1])

		// Find the body end by indentation.
		bodyEnd := findBodyEnd(lines, m.startLine, indentLevel)

		// Build signature and body (body includes decorators).
		signature := extractPySignature(lines, m.startLine)
		body := extractPyBodyWithDecorators(lines, m.startLine, bodyEnd)

		unit := CodeUnit{
			ID:        fmt.Sprintf("%s:%s", path, m.name),
			File:      path,
			Name:      m.name,
			Signature: signature,
			Body:      body,
			StartLine: m.startLine,
			EndLine:   bodyEnd,
			Language:  "python",
		}
		unit.ComputeHash()
		units = append(units, unit)

		consumedRanges = append(consumedRanges, lineRange{m.startLine, bodyEnd})

		// Extract methods within the class body.
		classBodyStart := m.startLine + 1
		classBodyEnd := bodyEnd

		methodMatches := findPyDefMatchesInRegion(pyFuncRegex, lines, classBodyStart, classBodyEnd)
		for _, fm := range methodMatches {
			funcIndentLevel := getIndentLevel(lines[fm.startLine-1])

			// Method body ends when indentation drops to or below the function's indent,
			// but no further than the class body end.
			methodBodyEnd := findBodyEnd(lines, fm.startLine, funcIndentLevel)
			if methodBodyEnd > classBodyEnd {
				methodBodyEnd = classBodyEnd
			}

			methodSignature := extractPySignature(lines, fm.startLine)
			methodBody := extractPyBodyWithDecorators(lines, fm.startLine, methodBodyEnd)

			methodUnit := CodeUnit{
				ID:        fmt.Sprintf("%s:%s.%s", path, m.name, fm.name),
				File:      path,
				Name:      fmt.Sprintf("%s.%s", m.name, fm.name),
				Signature: methodSignature,
				Body:      methodBody,
				StartLine: fm.startLine,
				EndLine:   methodBodyEnd,
				Language:  "python",
			}
			methodUnit.ComputeHash()
			units = append(units, methodUnit)

			consumedRanges = append(consumedRanges, lineRange{fm.startLine, methodBodyEnd})
		}
	}

	// Second pass: extract top-level function declarations (not inside classes).
	funcMatches := findPyDefMatches(pyFuncRegex, lines)
	for _, fm := range funcMatches {
		// Skip if already consumed by a class body.
		if isWithinConsumedRange(fm.startLine, consumedRanges) {
			continue
		}

		// Skip test functions unless explicitly included.
		if !cfg.IncludeTests && strings.HasPrefix(fm.name, "test_") {
			continue
		}

		indentLevel := getIndentLevel(lines[fm.startLine-1])

		// Skip functions that are nested inside other constructs (non-zero indent).
		// Nested functions are included in the parent function's body.
		if indentLevel > 0 {
			continue
		}

		bodyEnd := findBodyEnd(lines, fm.startLine, indentLevel)

		signature := extractPySignature(lines, fm.startLine)
		body := extractPyBodyWithDecorators(lines, fm.startLine, bodyEnd)

		unit := CodeUnit{
			ID:        fmt.Sprintf("%s:%s", path, fm.name),
			File:      path,
			Name:      fm.name,
			Signature: signature,
			Body:      body,
			StartLine: fm.startLine,
			EndLine:   bodyEnd,
			Language:  "python",
		}
		unit.ComputeHash()
		units = append(units, unit)

		consumedRanges = append(consumedRanges, lineRange{fm.startLine, bodyEnd})
	}

	return units, nil
}

// pyDefMatch holds the result of a Python def/class regex match.
type pyDefMatch struct {
	name     string // function or class name
	startLine int  // 1-based line where the def/class keyword appears
}

// findPyDefMatches finds all def or class matches in the lines.
func findPyDefMatches(re *regexp.Regexp, lines []string) []pyDefMatch {
	var matches []pyDefMatch

	for i, line := range lines {
		locs := re.FindAllStringSubmatchIndex(line, -1)
		for _, loc := range locs {
			if len(loc) < 4 {
				continue
			}
			name := line[loc[4]:loc[5]]
			matches = append(matches, pyDefMatch{
				name:      name,
				startLine: i + 1, // 1-based
			})
		}
	}

	return matches
}

// findPyDefMatchesInRegion finds def matches only within a region of lines.
// regionStart and regionEnd are 1-based inclusive.
func findPyDefMatchesInRegion(re *regexp.Regexp, lines []string, regionStart, regionEnd int) []pyDefMatch {
	var matches []pyDefMatch

	for i := regionStart; i <= regionEnd && i <= len(lines); i++ {
		line := lines[i-1]
		locs := re.FindAllStringSubmatchIndex(line, -1)
		for _, loc := range locs {
			if len(loc) < 4 {
				continue
			}
			name := line[loc[4]:loc[5]]
			matches = append(matches, pyDefMatch{
				name:      name,
				startLine: i,
			})
		}
	}

	return matches
}

// getIndentLevel returns the indentation level of a line (number of leading spaces).
// Tabs are counted as 4 spaces for consistency.
func getIndentLevel(line string) int {
	count := 0
	for _, ch := range line {
		if ch == ' ' {
			count++
		} else if ch == '\t' {
			count += 4
		} else {
			break
		}
	}
	return count
}

// findBodyEnd finds the last line (1-based) of a function/class body.
// The body starts on defLine and continues until a non-blank, non-comment
// line with indentation <= defIndentLevel, or until end of file.
func findBodyEnd(lines []string, defLine int, defIndentLevel int) int {
	bodyStart := defLine // 1-based

	// Scan from the line after the def/class line to find the body extent.
	endLine := defLine
	bodyStarted := false

	for i := bodyStart + 1; i <= len(lines); i++ {
		line := lines[i-1]
		trimmed := strings.TrimSpace(line)

		// Blank lines and comments within the body don't end it.
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			if bodyStarted {
				endLine = i
			}
			continue
		}

		lineIndent := getIndentLevel(line)

		// If indentation returns to or below the definition level, body ends.
		if lineIndent <= defIndentLevel {
			break
		}

		bodyStarted = true
		endLine = i
	}

	return endLine
}

// extractPySignature returns the def/class line text (with leading whitespace trimmed).
func extractPySignature(lines []string, defLine int) string {
	if defLine < 1 || defLine > len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[defLine-1])
}

// extractPyBodyWithDecorators returns the body text from defLine to endLine
// (both 1-based, inclusive), plus any decorator lines that precede the defLine.
// Decorators are included in the body text but StartLine still refers to the
// def/class line (not the decorator line).
func extractPyBodyWithDecorators(lines []string, defLine, endLine int) string {
	// Find decorators preceding the def/class line.
	decoratorEnd := defLine // 1-based, exclusive
	for i := defLine - 2; i >= 0; i-- {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if pyDecoratorRegex.MatchString(trimmed) {
			decoratorEnd = i + 1 // 1-based, exclusive boundary
		} else if trimmed == "" {
			// Allow blank lines between decorators and def line
			// but stop if we hit a non-decorator, non-blank line
			// Check if the line before this blank line is a decorator
			if i > 0 && pyDecoratorRegex.MatchString(strings.TrimSpace(lines[i-1])) {
				continue // Keep going, blank line between decorators
			}
			break
		} else {
			break // Non-decorator, non-blank line
		}
	}

	// Collect all lines from decoratorEnd to endLine.
	if defLine < 1 || endLine > len(lines) || defLine > endLine {
		return ""
	}

	var parts []string
	start := decoratorEnd
	if start < 1 {
		start = 1
	}
	for i := start; i <= endLine; i++ {
		if i >= 1 && i <= len(lines) {
			parts = append(parts, lines[i-1])
		}
	}

	return strings.Join(parts, "\n")
}