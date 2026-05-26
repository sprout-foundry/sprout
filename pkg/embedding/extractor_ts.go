package embedding

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/ast"
	gotreesitter "github.com/odvcencio/gotreesitter"
)

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

	lang := langFromPath(path)

	// Extract scoped symbols from the AST.
	symbols := ast.ExtractSymbols(result.Root, result.Bound, lang)

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

		// Skip variables whose value is a plain literal (string, number, boolean).
		if sym.Kind == "variable" {
			if isPlainLiteralVar(sym, result) {
				continue
			}
		}

		// Build the CodeUnit.
		unit, err := buildUnitFromSymbol(path, sym, src, result.Bound, lang)
		if err != nil {
			continue
		}
		units = append(units, *unit)
	}

	return units, nil
}

// isExtractableKind returns true for symbol kinds that represent code units
// suitable for embedding extraction.
func isExtractableKind(kind string) bool {
	switch kind {
	case "function", "method", "class", "variable":
		return true
	default:
		return false
	}
}

// isTestFunction returns true if the symbol name matches common test function
// naming conventions used by popular testing frameworks.
func isTestFunction(name, scope string) bool {
	switch {
	case strings.HasPrefix(name, "test"),
		strings.HasPrefix(name, "Test"),
		strings.HasPrefix(name, "spec"),
		strings.HasPrefix(name, "Spec"),
		strings.HasPrefix(name, "describe"),
		strings.HasPrefix(name, "describeSkip"),
		strings.HasPrefix(name, "xit"),
		strings.HasPrefix(name, "xdescribe"),
		strings.HasPrefix(name, "it"),
		strings.HasPrefix(name, "fit"),
		strings.HasPrefix(name, "fdescribe"),
		strings.HasPrefix(name, "benchmark"),
		strings.HasPrefix(name, "Benchmark"),
		strings.HasPrefix(name, "fuzz"),
		strings.HasPrefix(name, "Fuzz"):
		return true
	}
	return false
}

// isPlainLiteralVar returns true if a variable symbol has a value that is a
// simple literal (string, number, boolean) or a direct call expression,
// indicating it is not a meaningful code unit.
func isPlainLiteralVar(sym ast.ScopedSymbol, result *ast.ASTResult) bool {
	symText := string(result.Source[sym.StartByte:sym.EndByte])

	// Check if it ends with a closing brace (object or function body).
	trimmed := strings.TrimSpace(symText)
	if strings.HasSuffix(trimmed, "}") {
		// Likely an object literal or function body — extract it.
		return false
	}

	// If it's a simple one-line assignment without braces, skip it.
	if !strings.Contains(symText, "{") && !strings.Contains(symText, "=>") {
		return true
	}

	return false
}

// buildUnitFromSymbol creates a CodeUnit from an AST symbol by extracting
// signature and body text from the source using byte offsets.
func buildUnitFromSymbol(path string, sym ast.ScopedSymbol, src []byte, bt *gotreesitter.BoundTree, lang string) (*CodeUnit, error) {
	if sym.StartByte >= sym.EndByte || sym.EndByte > len(src) {
		return nil, fmt.Errorf("embedding: invalid byte range for symbol %q", sym.Name)
	}

	// Extract the full text of the symbol from source bytes.
	fullText := string(src[sym.StartByte:sym.EndByte])

	// Split into signature (everything up to and including the opening brace)
	// and body (from the opening brace to the end).
	signature, body := splitSignatureBody(fullText)

	// Build the display name: "Scope.Name" for scoped symbols, "Name" otherwise.
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
		EndLine:   sym.EndLine,
		Language:  lang,
	}
	unit.ComputeHash()

	return unit, nil
}

// splitSignatureBody splits symbol text into signature (up to and including
// the opening brace) and body (from the opening brace to the end).
// If no brace is found, the entire text becomes the signature and the body
// is empty.
func splitSignatureBody(text string) (signature, body string) {
	idx := strings.Index(text, "{")
	if idx < 0 {
		// No brace found — treat the entire text as signature.
		return strings.TrimSpace(text), ""
	}
	signature = strings.TrimSpace(text[:idx+1])
	body = strings.TrimSpace(text[idx:])
	return signature, body
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

// --------------------------------------------------------------------------
// Shared helpers used by extractor_py.go
// --------------------------------------------------------------------------

// lineRange represents a start/end line pair (1-based, inclusive).
type lineRange struct {
	start int
	end   int
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
