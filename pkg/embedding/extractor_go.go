package embedding

// Decision (SP-025-5c): This file uses native go/ast instead of pkg/ast (tree-sitter)
// because the standard library parser is faster and more accurate for Go code.
// TypeScript and Python extractors were migrated to tree-sitter for consistency
// and to support language features regex couldn't parse; Go has no such gap.
import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// ExtractGoFile parses a Go source file and extracts all top-level function
// declarations as CodeUnit values. Test functions (Test*, Benchmark*, Fuzz*)
// are excluded by default; use WithIncludeTests to change this.
func ExtractGoFile(path string, opts ...ExtractOption) ([]CodeUnit, error) {
	cfg := &ExtractConfig{}
	cfg.ApplyOptions(opts...)

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("embedding: parse %s: %w", path, err)
	}

	// Read file content once; all extraction helpers need it.
	srcBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("embedding: read %s: %w", path, err)
	}
	src := string(srcBytes)

	var units []CodeUnit

	for _, decl := range node.Decls {
		if d, ok := decl.(*ast.FuncDecl); ok {
			unit, err := extractFuncDecl(fset, path, src, d)
			if err != nil {
				return nil, fmt.Errorf("embedding: extract decl %s: %w", d.Name.Name, err)
			}
			if shouldSkipGoUnit(unit, cfg) {
				continue
			}
			units = append(units, unit)

			// Scan function body for nested function literals (closures)
			if d.Body != nil {
				ast.Inspect(d.Body, func(n ast.Node) bool {
					if fl, ok := n.(*ast.FuncLit); ok {
						closureUnits, cerr := extractFuncLits(fset, path, src, fl)
						if cerr != nil {
							// This shouldn't happen since we already have src,
							// but if it does, skip the closure rather than failing.
							return true
						}
						units = append(units, closureUnits...)
					}
					return true
				})
			}
		}
	}

	return units, nil
}

// extractFuncDecl builds a CodeUnit from a function declaration node.
// If d.Body is nil (e.g., interface method), returns a unit with empty body
// and the declaration text as the signature.
func extractFuncDecl(fset *token.FileSet, path string, src string, d *ast.FuncDecl) (CodeUnit, error) {
	name := functionName(d)

	startPos := d.Pos()
	endPos := d.End()

	startLine := fset.Position(startPos).Line
	endLine := fset.Position(endPos).Line

	signature := ""
	body := ""

	if d.Body == nil {
		// Interface method or external declaration with no body.
		// Use the full declaration as the signature and leave body empty.
		signature = funcSignatureNoBody(fset, src, d)
	} else {
		signature = funcSignature(fset, src, d)
		body = funcBody(fset, src, d)
	}

	unit := CodeUnit{
		ID:        makeUnitID(path, name, startLine),
		File:      path,
		Name:      name,
		Signature: signature,
		Body:      body,
		StartLine: startLine,
		EndLine:   endLine,
		Language:  "go",
	}
	unit.ComputeHash()

	return unit, nil
}

// extractFuncLits recursively extracts function literals, including those
// nested inside function declarations (e.g., closure bodies).
func extractFuncLits(fset *token.FileSet, path string, src string, lit *ast.FuncLit) ([]CodeUnit, error) {
	var units []CodeUnit

	startLine := fset.Position(lit.Pos()).Line
	endLine := fset.Position(lit.End()).Line
	signature := funcLitSignature(fset, src, lit)
	body := funcLitBody(fset, src, lit)

	anonName := "anonymous"
	if lit.Type.Params.NumFields() > 0 {
		// Include first param name for context if available
		anonName = fmt.Sprintf("anonymous(%s)", lit.Type.Params.List[0].Names)
	}

	unit := CodeUnit{
		ID:        makeUnitID(path, anonName, startLine),
		File:      path,
		Name:      anonName,
		Signature: signature,
		Body:      body,
		StartLine: startLine,
		EndLine:   endLine,
		Language:  "go",
	}
	unit.ComputeHash()
	units = append(units, unit)

	// Recurse into body for nested function literals
	for _, stmt := range lit.Body.List {
		ast.Inspect(stmt, func(n ast.Node) bool {
			if fl, ok := n.(*ast.FuncLit); ok {
				nested, err := extractFuncLits(fset, path, src, fl)
				if err != nil {
					return false // stop traversal on error
				}
				units = append(units, nested...)
			}
			return true
		})
	}

	return units, nil
}

// functionName returns the display name for a FuncDecl.
// For methods it returns "(*Receiver).Method" or "(Receiver).Method".
// For functions it returns "funcName" or "_" for blank identifiers.
func functionName(d *ast.FuncDecl) string {
	if d.Name == nil || d.Name.Name == "_" {
		return "_"
	}

	if d.Recv != nil && len(d.Recv.List) > 0 {
		recv := d.Recv.List[0]
		recvName := recvType(recv.Type)
		ptr := ""
		if se, ok := recv.Type.(*ast.StarExpr); ok {
			ptr = "*"
			recvName = recvType(se.X)
		}
		return fmt.Sprintf("(%s%s).%s", ptr, recvName, d.Name.Name)
	}

	return d.Name.Name
}

// recvType extracts the type name from an expression node (ident, selector, etc.).
func recvType(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return recvType(e.X) + "." + e.Sel.Name
	default:
		return "?"
	}
}

// funcSignatureNoBody returns the full declaration text for a function with
// no body (e.g., interface method).
func funcSignatureNoBody(fset *token.FileSet, src string, d *ast.FuncDecl) string {
	srcLines := strings.Split(src, "\n")
	start := fset.Position(d.Pos()).Line - 1
	end := fset.Position(d.End()).Line - 1

	if start >= len(srcLines) {
		return ""
	}

	var parts []string
	for i := start; i <= end && i < len(srcLines); i++ {
		parts = append(parts, srcLines[i])
	}

	return strings.Join(parts, "\n")
}

// funcSignature returns the full function signature text from source.
// Safe when d.Body is nil (returns empty string).
func funcSignature(fset *token.FileSet, src string, d *ast.FuncDecl) string {
	if d.Body == nil {
		return ""
	}
	srcLines := strings.Split(src, "\n")
	start := fset.Position(d.Pos()).Line - 1
	end := fset.Position(d.Body.Pos()).Line - 2 // line before opening brace

	var parts []string
	for i := start; i <= end && i < len(srcLines); i++ {
		parts = append(parts, srcLines[i])
	}

	// Trim leading/trailing whitespace lines
	for len(parts) > 0 && strings.TrimSpace(parts[0]) == "" {
		parts = parts[1:]
	}
	for len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
		parts = parts[:len(parts)-1]
	}

	return strings.Join(parts, "\n")
}

// funcBody returns the source text of the function body (between braces).
// Returns empty string when d.Body is nil.
func funcBody(fset *token.FileSet, src string, d *ast.FuncDecl) string {
	if d.Body == nil {
		return ""
	}
	srcLines := strings.Split(src, "\n")
	start := fset.Position(d.Body.Pos()).Line // opening brace line
	end := fset.Position(d.Body.End()).Line - 1 // closing brace line

	if start >= len(srcLines) {
		return ""
	}

	lines := srcLines[start : end+1]
	return strings.Join(lines, "\n")
}

// funcLitSignature returns the function literal signature text.
func funcLitSignature(fset *token.FileSet, src string, lit *ast.FuncLit) string {
	srcLines := strings.Split(src, "\n")
	start := fset.Position(lit.Pos()).Line - 1
	end := fset.Position(lit.Body.Pos()).Line - 2

	var parts []string
	for i := start; i <= end && i < len(srcLines); i++ {
		parts = append(parts, srcLines[i])
	}

	for len(parts) > 0 && strings.TrimSpace(parts[0]) == "" {
		parts = parts[1:]
	}
	for len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
		parts = parts[:len(parts)-1]
	}

	return strings.Join(parts, "\n")
}

// funcLitBody returns the source text of a function literal body.
func funcLitBody(fset *token.FileSet, src string, lit *ast.FuncLit) string {
	if lit.Body == nil {
		return ""
	}
	srcLines := strings.Split(src, "\n")
	start := fset.Position(lit.Body.Pos()).Line
	end := fset.Position(lit.Body.End()).Line - 1

	if start >= len(srcLines) {
		return ""
	}

	lines := srcLines[start : end+1]
	return strings.Join(lines, "\n")
}

// shouldSkipGoUnit returns true if the unit should be excluded from extraction
// based on the provided ExtractConfig.
func shouldSkipGoUnit(unit CodeUnit, cfg *ExtractConfig) bool {
	// Skip blank identifiers
	if unit.Name == "_" {
		return true
	}

	// Skip test functions unless explicitly included
	if !cfg.IncludeTests {
		name := unit.Name
		// Strip receiver prefix for method-based test detection
		if i := strings.LastIndex(name, "."); i >= 0 {
			name = name[i+1:]
		}
		if strings.HasPrefix(name, "Test") ||
			strings.HasPrefix(name, "Benchmark") ||
			strings.HasPrefix(name, "Fuzz") {
			return true
		}
	}

	// Skip files with _test.go suffix
	if !cfg.IncludeTests {
		if strings.HasSuffix(filepath.Base(unit.File), "_test.go") {
			return true
		}
	}

	return false
}
