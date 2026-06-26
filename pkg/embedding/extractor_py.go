package embedding

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/sprout-foundry/sprout/pkg/ast"
)

// pyTestFilePattern matches common test file naming conventions.
var pyTestFilePattern = regexp.MustCompile(`_test\.py$|test_.*\.py$`)

// pyDecoratorRegex matches decorator lines: @something
var pyDecoratorRegex = regexp.MustCompile(`^\s*@`)

// ExtractPyFile parses a Python source file and extracts
// code units (functions, classes, methods) as CodeUnit values.
// Test functions (prefixed with test_) are excluded by default; use
// WithIncludeTests to change this.
func ExtractPyFile(path string, opts ...ExtractOption) ([]CodeUnit, error) {
	cfg := &ExtractConfig{}
	cfg.ApplyOptions(opts...)

	// Skip test files unless explicitly included.
	if !cfg.IncludeTests && pyTestFilePattern.MatchString(filepath.Base(path)) {
		return nil, nil
	}

	// Read the file content.
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("embedding: read %s: %w", path, err)
	}

	// Parse the file using the AST parser.
	result, err := ast.ParseFile(path, src)
	if err != nil {
		// Graceful degradation: if AST parsing fails, return empty.
		return nil, nil
	}
	defer result.Release()

	symbols := ast.ExtractSymbols(result.Root, result.Bound, "python")

	var units []CodeUnit

	for _, sym := range symbols {
		// Filter out kinds that don't represent code units.
		if !isExtractableKind(sym.Kind) {
			continue
		}

		// Filter out test functions unless explicitly included.
		if !cfg.IncludeTests && isTestFunction(sym.Name, sym.Scope) {
			continue
		}

		// Build the CodeUnit, resolving decorators and trailing blanks.
		unit, err := buildPyUnitFromSymbol(path, sym, src, result.Bound)
		if err != nil {
			continue
		}
		units = append(units, *unit)
	}

	return units, nil
}

// buildPyUnitFromSymbol creates a CodeUnit from a Python AST symbol.
// It looks back from the symbol start for decorator lines and extends the
// end past trailing blank lines to match the prior regex-based behavior.
func buildPyUnitFromSymbol(path string, sym ast.ScopedSymbol, src []byte, bt *gotreesitter.BoundTree) (*CodeUnit, error) {
	if sym.StartByte >= sym.EndByte || sym.EndByte > len(src) {
		return nil, fmt.Errorf("embedding: invalid byte range for symbol %q", sym.Name)
	}

	srcStr := string(src)
	lines := strings.Split(srcStr, "\n")

	// Resolve decorator lines preceding this symbol.
	// sym.StartByte may already point to a decorator (for top-level
	// decorated_definition nodes) or to the inner def (for class members).
	// Walk backwards from the line BEFORE the def line to find decorators.
	defLineIdx := sym.StartLine - 1 // 0-based
	firstDecorLine := defLineIdx
	for i := defLineIdx - 1; i >= 0; i-- {
		if pyDecoratorRegex.MatchString(lines[i]) {
			firstDecorLine = i
		} else if strings.TrimSpace(lines[i]) == "" {
			// Allow blank lines between decorators and def line
			if i > 0 && pyDecoratorRegex.MatchString(lines[i-1]) {
				continue // blank line between decorators
			}
			break
		} else {
			break
		}
	}

	// Determine the decorator start byte (0-based).
	decorStartByte := sym.StartByte
	if firstDecorLine < defLineIdx {
		decorStartByte = lineStartByte(lines, firstDecorLine)
	}

	// Extend end past trailing blank lines (matches old findBodyEnd behavior).
	bodyEndLine := sym.EndLine // 1-based
	for bodyEndLine < len(lines) && strings.TrimSpace(lines[bodyEndLine]) == "" {
		bodyEndLine++
	}

	// Compute end byte: end of the last non-blank line in the extended range.
	bodyEndByte := lineEndByte(lines, bodyEndLine-1) // convert back to 0-based index

	// Extract the full text including decorators through body.
	if decorStartByte >= bodyEndByte || decorStartByte >= len(src) {
		fullText := string(src[sym.StartByte:sym.EndByte])
		signature, body := splitPySignatureBody(fullText, defLineIdx, lines)
		name := sym.Name
		if sym.Scope != "" {
			name = sym.Scope + "." + sym.Name
		}
		unit := &CodeUnit{
			ID:        makeUnitID(path, name, sym.StartLine),
			File:      path,
			Name:      name,
			Signature: signature,
			Body:      body,
			StartLine: sym.StartLine,
			EndLine:   bodyEndLine,
			Language:  "python",
		}
		unit.ComputeHash()
		return unit, nil
	}

	fullText := string(src[decorStartByte:bodyEndByte])

	signature, body := splitPySignatureBody(fullText, defLineIdx, lines)

	// Build the display name: "Scope.Name" for scoped symbols (methods), "Name" otherwise.
	name := sym.Name
	if sym.Scope != "" {
		name = sym.Scope + "." + sym.Name
	}

	unit := &CodeUnit{
		ID:        makeUnitID(path, name, sym.StartLine),
		File:      path,
		Name:      name,
		Signature: signature,
		Body:      body,
		StartLine: sym.StartLine,
		EndLine:   bodyEndLine,
		Language:  "python",
	}
	unit.ComputeHash()

	return unit, nil
}

// splitPySignatureBody splits Python symbol text into signature (the def/class
// line) and body (the full text from def/class to the end).  For Python the
// signature is the def/class line (first non-decorator line).  The body is the
// full text from the first decorator (if any) through the end.
func splitPySignatureBody(text string, defLineIdx int, lines []string) (signature, body string) {
	// Signature is the def/class line, trimmed.
	if defLineIdx < 0 || defLineIdx >= len(lines) {
		return strings.TrimSpace(text), ""
	}
	signature = strings.TrimSpace(lines[defLineIdx])

	// Body is the full text, trimmed.
	body = strings.TrimSpace(text)

	return signature, body
}

// lineStartByte returns the byte offset of the start of the given 0-based line index.
func lineStartByte(lines []string, lineIdx int) int {
	byteOffset := 0
	for i := 0; i < lineIdx && i < len(lines); i++ {
		byteOffset += len(lines[i]) + 1 // +1 for the newline
	}
	return byteOffset
}

// lineEndByte returns the byte offset just past the end of the given 0-based line index.
func lineEndByte(lines []string, lineIdx int) int {
	byteOffset := 0
	for i := 0; i <= lineIdx && i < len(lines); i++ {
		byteOffset += len(lines[i]) + 1 // +1 for the newline
	}
	return byteOffset
}

// --------------------------------------------------------------------------
// Helpers — shared functions are defined in extractor_ts.go (isExtractableKind,
// isTestFunction).  Python-specific regex helpers follow.
// --------------------------------------------------------------------------
