package embedding

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// tsFuncRegex matches function declarations (top-level and nested).
// Captures the function name. Handles optional export/async/default.
var tsFuncRegex = regexp.MustCompile(`(?m)^(?:[\s\f\v]*(?:\/\/.*\n|\/\*[\s\S]*?\*\/\n)*)*[\s\f\v]*(?:(?:export|import|declare)\s+)?(?:(?:default|abstract)\s+)?(?:async\s+)?function\s+(\w+)\s*\(`)

// tsClassRegex matches class declarations.
// Captures the class name. Handles optional export/default/abstract.
var tsClassRegex = regexp.MustCompile(`(?m)^(?:[\s\f\v]*(?:\/\/.*\n|\/\*[\s\S]*?\*\/\n)*)*[\s\f\v]*(?:(?:export|import|declare)\s+)?(?:(?:default|abstract)\s+)?class\s+(\w+)`)

// tsMethodRegex matches method declarations inside classes or objects.
// Captures the method name. Handles access modifiers, static, async, get/set.
var tsMethodRegex = regexp.MustCompile(`(?m)^\s*(?:(?:public|private|protected|readonly|abstract|override)\s+)*(?:static\s+)?(?:async\s+)?(?:get\s+|set\s+)?(\w+)\s*\([^)]*\)\s*(?::\s*[^=]+?)?\s*\{`)

// tsArrowRegex matches arrow functions assigned to const/let/var.
// Captures the variable name. Handles optional export and params in parens.
var tsArrowRegex = regexp.MustCompile(`(?m)^\s*(?:(?:export|declare)\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?\([^)]*\)\s*(?::\s*[^=]+?)?\s*=>\s*\{`)

// tsArrowParensRegex matches arrow functions with single param (no parens).
var tsArrowParensRegex = regexp.MustCompile(`(?m)^\s*(?:(?:export|declare)\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?\w+\s*(?::\s*[^=]+?)?\s*=>\s*\{`)

// tsInterfaceMethodRegex matches method signatures in interface/type declarations.
// These have no body, so we capture only the signature.
var tsInterfaceMethodRegex = regexp.MustCompile(`(?m)^\s+(?:(?:public|private|protected|readonly|abstract|override)\s+)*(?:(?:static|async)\s+)?(\w+)\s*\([^)]*\)\s*(?::\s*[^;{]+)?;`)

// tsTestFilePattern matches common test file naming conventions.
var tsTestFilePattern = regexp.MustCompile(`\.(test|spec)\.(ts|tsx|js|jsx|mjs)$`)

// tsDeclarationFilePattern matches TypeScript declaration files (.d.ts).
var tsDeclarationFilePattern = regexp.MustCompile(`\.d\.(ts|tsx)$`)

// tsSupportedExtensions lists file extensions handled by this extractor.
var tsSupportedExtensions = map[string]bool{
	".ts":  true,
	".tsx": true,
	".js":  true,
	".jsx": true,
	".mjs": true,
}

// ExtractTSFile parses a TypeScript or JavaScript source file and extracts
// code units (functions, arrow functions, methods, classes) as CodeUnit values.
// Test files (.test.ts, .spec.js, etc.) are excluded by default; use
// WithIncludeTests to change this.
func ExtractTSFile(path string, opts ...ExtractOption) ([]CodeUnit, error) {
	cfg := &ExtractConfig{}
	cfg.ApplyOptions(opts...)

	// Skip test files unless explicitly included.
	if !cfg.IncludeTests && tsTestFilePattern.MatchString(filepath.Base(path)) {
		return nil, nil
	}

	// Skip .d.ts declaration files (no implementation to extract).
	if tsDeclarationFilePattern.MatchString(filepath.Base(path)) {
		return nil, nil
	}

	srcBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("embedding: read %s: %w", path, err)
	}
	src := string(srcBytes)

	var units []CodeUnit

	// Track which line ranges are already consumed by class bodies,
	// so we don't double-extract methods as standalone functions.
	var consumedRanges []lineRange

	// Extract classes first (they contain methods).
	for _, match := range findRegex(tsClassRegex, src) {
		name := match.groups[0]
		bodyStart, bodyEnd := findBraceBlock(src, match.braceLine)

		if bodyStart < 0 || bodyEnd < 0 {
			continue
		}

		sig := extractSignature(src, match.startLine, bodyStart)
		body := extractBody(src, bodyStart, bodyEnd)

		unit := CodeUnit{
			ID:        fmt.Sprintf("%s:%s", path, name),
			File:      path,
			Name:      name,
			Signature: sig,
			Body:      body,
			StartLine: match.startLine,
			EndLine:   bodyEnd,
			Language:  langFromPath(path),
		}
		unit.ComputeHash()
		units = append(units, unit)

		consumedRanges = append(consumedRanges, lineRange{bodyStart, bodyEnd})

		// Extract methods within the class body.
		classBody := extractBody(src, bodyStart, bodyEnd)
		classBodyStart := bodyStart
		for _, m := range findRegexInRegion(tsMethodRegex, classBody, classBodyStart) {
			// Check if this match is a real method (not a variable or something else
			// that happens to have parens and braces).
			// Methods don't start with "function" or "class" or "const" etc.
			// The regex already filters those out by requiring a name followed by parens.

			methodBodyStart, methodBodyEnd := findBraceBlockInRegion(src, m.braceLine)
			if methodBodyStart < 0 || methodBodyEnd < 0 {
				continue
			}

			methodSig := extractSignature(src, m.startLine, methodBodyStart)
			methodBody := extractBody(src, methodBodyStart, methodBodyEnd)

			methodName := m.groups[0]
			// Skip common false positives from the regex.
			if isFalsePositiveMethod(methodName) {
				continue
			}

			methodUnit := CodeUnit{
				ID:        fmt.Sprintf("%s:%s.%s", path, name, methodName),
				File:      path,
				Name:      fmt.Sprintf("%s.%s", name, methodName),
				Signature: methodSig,
				Body:      methodBody,
				StartLine: m.startLine,
				EndLine:   methodBodyEnd,
				Language:  langFromPath(path),
			}
			methodUnit.ComputeHash()
			units = append(units, methodUnit)

			consumedRanges = append(consumedRanges, lineRange{methodBodyStart, methodBodyEnd})
		}
	}

	// Extract top-level function declarations.
	for _, match := range findRegex(tsFuncRegex, src) {
		if isWithinConsumedRange(match.startLine, consumedRanges) {
			continue
		}

		name := match.groups[0]
		bodyStart, bodyEnd := findBraceBlock(src, match.braceLine)
		if bodyStart < 0 || bodyEnd < 0 {
			continue
		}

		sig := extractSignature(src, match.startLine, bodyStart)
		body := extractBody(src, bodyStart, bodyEnd)

		unit := CodeUnit{
			ID:        fmt.Sprintf("%s:%s", path, name),
			File:      path,
			Name:      name,
			Signature: sig,
			Body:      body,
			StartLine: match.startLine,
			EndLine:   bodyEnd,
			Language:  langFromPath(path),
		}
		unit.ComputeHash()
		units = append(units, unit)

		consumedRanges = append(consumedRanges, lineRange{bodyStart, bodyEnd})
	}

	// Extract arrow function assignments.
	for _, regex := range []*regexp.Regexp{tsArrowRegex, tsArrowParensRegex} {
		for _, match := range findRegex(regex, src) {
			if isWithinConsumedRange(match.startLine, consumedRanges) {
				continue
			}

			name := match.groups[0]
			bodyStart, bodyEnd := findBraceBlock(src, match.braceLine)
			if bodyStart < 0 || bodyEnd < 0 {
				continue
			}

			sig := extractSignature(src, match.startLine, bodyStart)
			body := extractBody(src, bodyStart, bodyEnd)

			unit := CodeUnit{
				ID:        fmt.Sprintf("%s:%s", path, name),
				File:      path,
				Name:      name,
				Signature: sig,
				Body:      body,
				StartLine: match.startLine,
				EndLine:   bodyEnd,
				Language:  langFromPath(path),
			}
			unit.ComputeHash()
			units = append(units, unit)

			consumedRanges = append(consumedRanges, lineRange{bodyStart, bodyEnd})
		}
	}

	return units, nil
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// lineRange represents a start/end line pair (1-based, inclusive).
type lineRange struct {
	start int
	end   int
}

// regexMatch holds the result of a regex match with line information.
type regexMatch struct {
	groups    []string // captured groups
	startLine int      // 1-based line where the match starts
	braceLine int      // 1-based line containing the opening brace
}

// findRegex finds all matches of the regex in the source and returns
// regexMatch values with line numbers.
func findRegex(re *regexp.Regexp, src string) []regexMatch {
	lines := strings.Split(src, "\n")
	var matches []regexMatch

	// We need to find the match position in the source, then compute line numbers.
	locs := re.FindAllStringSubmatchIndex(src, -1)
	for _, loc := range locs {
		if len(loc) < 2 {
			continue
		}

		matchStart := loc[0]

		// Extract captured groups first; we need the first group's position
		// to determine the actual declaration line (not including leading comments).
		var groups []string
		for i := 2; i < len(loc); i += 2 {
			if loc[i] >= 0 && loc[i+1] >= 0 {
				groups = append(groups, src[loc[i]:loc[i+1]])
			} else {
				groups = append(groups, "")
			}
		}

		// Determine the actual declaration start line from the captured name.
		// If the regex captured a group (e.g., the function name), use its position
		// since the full match may include leading comments that start on an earlier line.
		startLine := 1
		if len(loc) >= 4 && loc[2] >= 0 {
			// Use the captured group's start position.
			matchStart = loc[2]
		}
		for i := 0; i < matchStart && i < len(src); i++ {
			if src[i] == '\n' {
				startLine++
			}
		}

		// Find the line with the opening brace.
		braceLine := findBraceLine(src, startLine, lines)

		if braceLine < 0 {
			continue
		}

		matches = append(matches, regexMatch{
			groups:    groups,
			startLine: startLine,
			braceLine: braceLine,
		})
	}

	return matches
}

// findRegexInRegion finds all matches of the regex in a string region,
// offsetting line numbers by regionStart (1-based).
func findRegexInRegion(re *regexp.Regexp, region string, regionStart int) []regexMatch {
	var matches []regexMatch
	locs := re.FindAllStringSubmatchIndex(region, -1)

	for _, loc := range locs {
		if len(loc) < 2 {
			continue
		}

		matchStart := loc[0]
		matchLineInRegion := 0
		for i := 0; i < matchStart && i < len(region); i++ {
			if region[i] == '\n' {
				matchLineInRegion++
			}
		}

		absStartLine := regionStart + matchLineInRegion

		// Find the brace line within the region.
		regionLines := strings.Split(region, "\n")
		braceLineInRegion := findBraceLineInLines(region, matchLineInRegion, regionLines)
		if braceLineInRegion < 0 {
			continue
		}

		absBraceLine := regionStart + braceLineInRegion

		var groups []string
		for i := 2; i < len(loc); i += 2 {
			if loc[i] >= 0 && loc[i+1] >= 0 {
				groups = append(groups, region[loc[i]:loc[i+1]])
			} else {
				groups = append(groups, "")
			}
		}

		matches = append(matches, regexMatch{
			groups:    groups,
			startLine: absStartLine,
			braceLine: absBraceLine,
		})
	}

	return matches
}

// findBraceLine finds the line (1-based) containing the opening brace '{'
// for a declaration that starts at startLine (1-based).
// It searches from startLine up to startLine+20 to avoid scanning too far.
func findBraceLine(src string, startLine int, lines []string) int {
	maxSearch := startLine + 20
	for i := startLine; i <= maxSearch && i <= len(lines); i++ {
		line := lines[i-1] // convert to 0-based
		for j, ch := range line {
			if ch == '{' {
				// Verify this isn't inside a string or comment.
				if !isBraceInStringOrComment(line, j) {
					return i // 1-based
				}
			}
		}
	}
	return -1
}

// findBraceLineInLines is like findBraceLine but works on a region string.
// startLineInRegion is 0-based.
func findBraceLineInLines(region string, startLineInRegion int, lines []string) int {
	maxSearch := startLineInRegion + 20
	for i := startLineInRegion; i <= maxSearch && i < len(lines); i++ {
		line := lines[i]
		for j, ch := range line {
			if ch == '{' {
				if !isBraceInStringOrComment(line, j) {
					return i // 0-based
				}
			}
		}
	}
	return -1
}

// isBraceInStringOrComment checks if the character at position pos in line
// is inside a string literal or a line comment.
func isBraceInStringOrComment(line string, pos int) bool {
	inString := false
	stringChar := byte(0)
	escaped := false

	for i := 0; i < pos && i < len(line); i++ {
		ch := line[i]

		if escaped {
			escaped = false
			continue
		}

		if ch == '\\' {
			escaped = true
			continue
		}

		if inString {
			if ch == stringChar {
				inString = false
			}
			continue
		}

		// Check for string start.
		if ch == '"' || ch == '\'' || ch == '`' {
			inString = true
			stringChar = ch
			continue
		}

		// Check for line comment start.
		if ch == '/' && i+1 < len(line) && line[i+1] == '/' {
			// Rest of line is a comment.
			return true
		}

		// Check for block comment start.
		if ch == '/' && i+1 < len(line) && line[i+1] == '*' {
			// Inside a block comment; brace is likely in comment.
			return true
		}
	}

	return inString
}

// findBraceBlock finds the start and end lines (1-based, inclusive) of a
// brace-delimited block starting from braceLine (1-based).
// Returns -1, -1 if no matching braces are found.
func findBraceBlock(src string, braceLine int) (int, int) {
	return findBraceBlockInRegion(src, braceLine)
}

// findBraceBlockInRegion finds the brace block in the source starting from
// braceLine (1-based). Returns the start and end line (1-based, inclusive).
// Returns -1, -1 if no matching braces are found.
func findBraceBlockInRegion(src string, braceLine int) (int, int) {
	lines := strings.Split(src, "\n")
	if braceLine < 1 || braceLine > len(lines) {
		return -1, -1
	}

	// Find the opening brace position in braceLine.
	line := lines[braceLine-1]
	openIdx := -1
	for i, ch := range line {
		if ch == '{' && !isBraceInStringOrComment(line, i) {
			openIdx = i
			break
		}
	}

	if openIdx < 0 {
		return -1, -1
	}

	// Start brace counting from the character after the opening '{'.
	count := 1
	currentLine := braceLine

	for currentLine <= len(lines) {
		line = lines[currentLine-1]

		// For the brace line itself, start scanning after the opening brace.
		startCol := 0
		if currentLine == braceLine {
			startCol = openIdx + 1
		}

		inString := false
		stringChar := byte(0)
		escaped := false

		for i := startCol; i < len(line); i++ {
			ch := line[i]

			if escaped {
				escaped = false
				continue
			}

			if ch == '\\' {
				escaped = true
				continue
			}

			if inString {
				if ch == stringChar {
					inString = false
				}
				continue
			}

			// String start.
			if ch == '"' || ch == '\'' || ch == '`' {
				inString = true
				stringChar = byte(ch)
				continue
			}

			// Line comment — skip rest of line.
			if ch == '/' && i+1 < len(line) && line[i+1] == '/' {
				break
			}

			// Block comment — skip until '*/' (may span lines).
			if ch == '/' && i+1 < len(line) && line[i+1] == '*' {
				// Find the closing '*/'.
				closePos := strings.Index(line[i+2:], "*/")
				if closePos >= 0 {
					i += 2 + closePos + 1
					continue
				}
				// Comment extends past this line; skip to next line.
				break
			}

			if ch == '{' {
				count++
			} else if ch == '}' {
				count--
				if count == 0 {
					// Found the matching closing brace.
					return braceLine, currentLine
				}
			}
		}

		currentLine++
	}

	return -1, -1
}

// extractSignature returns the text from startLine to the line before bodyStart
// (exclusive of the opening brace line). All lines are 1-based.
func extractSignature(src string, startLine, bodyStart int) string {
	lines := strings.Split(src, "\n")
	if startLine < 1 || bodyStart < 1 || startLine >= bodyStart {
		// Single-line declaration: include the brace line in the signature.
		if startLine >= 1 && startLine <= len(lines) {
			return strings.TrimSpace(lines[startLine-1])
		}
		return ""
	}

	var parts []string
	for i := startLine; i < bodyStart; i++ {
		if i >= 1 && i <= len(lines) {
			parts = append(parts, lines[i-1])
		}
	}

	// Trim leading/trailing empty lines.
	for len(parts) > 0 && strings.TrimSpace(parts[0]) == "" {
		parts = parts[1:]
	}
	for len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
		parts = parts[:len(parts)-1]
	}

	return strings.Join(parts, "\n")
}

// extractBody returns the text of a brace-delimited body from startLine to
// endLine (both 1-based, inclusive).
func extractBody(src string, startLine, endLine int) string {
	lines := strings.Split(src, "\n")
	if startLine < 1 || endLine > len(lines) || startLine > endLine {
		return ""
	}

	// Include all lines from start to end, trimming trailing whitespace.
	var parts []string
	for i := startLine; i <= endLine; i++ {
		if i >= 1 && i <= len(lines) {
			parts = append(parts, lines[i-1])
		}
	}

	return strings.Join(parts, "\n")
}

// isWithinConsumedRange returns true if line falls within any consumed range.
func isWithinConsumedRange(line int, ranges []lineRange) bool {
	for _, r := range ranges {
		if line >= r.start && line <= r.end {
			return true
		}
	}
	return false
}

// isFalsePositiveMethod returns true if the method name is likely a false
// positive from the method regex (e.g., a keyword or non-method construct).
func isFalsePositiveMethod(name string) bool {
	switch name {
	case "if", "for", "while", "switch", "catch", "else", "function",
		"return", "throw", "new", "delete", "typeof", "instanceof",
		"import", "export", "class", "const", "let", "var",
		"try", "finally", "do", "case", "break", "continue":
		return true
	}
	return false
}

// langFromPath determines the language identifier from the file extension.
func langFromPath(path string) string {
	ext := filepath.Ext(path)
	switch ext {
	case ".ts", ".mts":
		return "typescript"
	case ".tsx":
		return "tsx"
	case ".js", ".mjs":
		return "javascript"
	case ".jsx":
		return "jsx"
	default:
		return "javascript"
	}
}

// isTSFile returns true if the file extension is handled by the TS/JS extractor.
func isTSFile(path string) bool {
	return tsSupportedExtensions[filepath.Ext(path)]
}
