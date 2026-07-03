package tools

import (
	"context"
	"fmt"
	astp "github.com/sprout-foundry/sprout/pkg/ast"
	codegraph "github.com/sprout-foundry/sprout/pkg/codegraph"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	repoMapMaxFullFileSize = 2 * 1024 * 1024 // 2MB max file size
	repoMapTokenBudget     = 1024            // target ~1024 tokens
	repoMapMaxFiles        = 200             // max files to include
	repoMapCharBudget      = repoMapTokenBudget * 4
)

var sourceExtensions = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".py": true, ".rs": true, ".java": true, ".c": true, ".cpp": true,
	".h": true,
}

var ignoredDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true,
	"build": true, ".next": true, "coverage": true, ".cache": true, ".sprout": true,
}

// treeSitterExtensions is the set of file extensions handled by the tree-sitter
// based pkg/ast parser.  Go files use go/ast directly.
var treeSitterExtensions = map[string]bool{
	".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".py": true,
}

// GenerateRepoMap walks the directory tree rooted at rootDir and produces a
// lightweight overview of the codebase showing file paths and top-level symbols.
// For Go files it uses go/ast; for TS/JS/Python it uses tree-sitter via pkg/ast.
// Output is truncated to ~1024 tokens.
func GenerateRepoMap(ctx context.Context, rootDir string) (string, error) {
	if rootDir == "" || rootDir == "." {
		var err error
		rootDir, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get working directory: %w", err)
		}
	}

	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return "", fmt.Errorf("resolve root directory: %w", err)
	}

	type fileEntry struct {
		absPath, relPath, ext string
	}

	var files []fileEntry
	err = filepath.WalkDir(absRoot, func(path string, d os.DirEntry, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err != nil {
			return nil
		}
		name := d.Name()
		// Skip symlinks to prevent following links outside the target tree.
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if ignoredDirs[name] {
				return filepath.SkipDir
			}
			if path != absRoot && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(name))
		if !sourceExtensions[ext] {
			return nil
		}
		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			return nil
		}
		files = append(files, fileEntry{path, filepath.ToSlash(rel), ext})
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("walk directory: %w", err)
	}

	sort.Slice(files, func(i, j int) bool { return files[i].relPath < files[j].relPath })
	if len(files) > repoMapMaxFiles {
		files = files[:repoMapMaxFiles]
	}

	var sb strings.Builder
	sb.WriteString("## repo_map: ")
	sb.WriteString(filepath.Base(absRoot))
	sb.WriteString("\n")

	charCount := sb.Len()
	fileCount := 0
	truncated := false

	for _, f := range files {
		select {
		case <-ctx.Done():
			return sb.String(), nil
		default:
		}

		// Read file content with size limit.
		content, readErr := os.ReadFile(f.absPath)
		if readErr != nil {
			continue
		}
		if len(content) > repoMapMaxFullFileSize {
			continue // skip oversized files silently
		}
		if isBinaryContent(content) {
			continue
		}

		symbols, err := extractSymbolsForFile(f.absPath, f.ext, content)
		if err != nil {
			// If extraction fails (e.g., AST parse error), skip the file.
			continue
		}
		if len(symbols) == 0 {
			continue
		}

		section := "\n### " + f.relPath + "\n"
		for _, sym := range symbols {
			section += fmt.Sprintf("- %s:%d\n", sym.Name, sym.Line)
		}
		if charCount+len(section) > repoMapCharBudget && fileCount > 0 {
			truncated = true
			break
		}
		sb.WriteString(section)
		charCount += len(section)
		fileCount++
	}

	if truncated {
		sb.WriteString("\n*... truncated (token budget reached)*\n")
	}
	if fileCount == 0 {
		sb.WriteString("\n*No source files with symbols found.*\n")
	}
	return sb.String(), nil
}

// extractSymbolsViaTreeSitter uses the pkg/ast tree-sitter parser to extract
// symbols from TS/JS/Python files.
func extractSymbolsViaTreeSitter(path string, ext string, content []byte) ([]symbolEntry, error) {
	result, err := astp.ParseFile(path, content)
	if err != nil {
		return nil, err
	}
	defer result.Release()

	var entries []symbolEntry
	for _, sym := range result.Symbols {
		prefix := symbolDisplayPrefix(sym.Kind, ext)
		entries = append(entries, symbolEntry{
			Name: prefix + " " + sym.Name,
			Line: sym.StartLine,
		})
	}
	return entries, nil
}

// symbolDisplayPrefix maps an AST symbol kind to the display prefix used in
// the repo map output.  This preserves backward compatibility with the
// previous regex-based output format (e.g. "def" for Python functions,
// "const" for TS/JS variables).
func symbolDisplayPrefix(kind string, ext string) string {
	switch ext {
	case ".py":
		if kind == "function" {
			return "def"
		}
		return kind
	case ".ts", ".tsx", ".js", ".jsx":
		if kind == "variable" {
			return "const"
		}
		return kind
	default:
		return kind
	}
}

// symbolEntry pairs a symbol name with its 1-based line number.
type symbolEntry struct {
	Name string
	Line int
}

// SymbolWithEdges holds symbols and call edges for a single file.
type SymbolWithEdges struct {
	Symbols []symbolEntry
	Edges   []codegraph.Edge
}

// extractSymbolsForFile extracts symbols from a file using the appropriate
// parser: go/ast for Go, tree-sitter via pkg/ast for TS/JS/Python.
// Unsupported extensions return an error.
func extractSymbolsForFile(path string, ext string, content []byte) ([]symbolEntry, error) {
	if ext == ".go" {
		return extractGoSymbolsAST(path, content)
	}
	if treeSitterExtensions[ext] {
		return extractSymbolsViaTreeSitter(path, ext, content)
	}
	return nil, fmt.Errorf("unsupported file extension: %s", ext)
}

// extractGoSymbolsAST parses a Go source file using go/ast and extracts
// top-level functions, methods, and type declarations as symbolEntry values.
// Test functions (Test*, Benchmark*, Fuzz*) and _test.go files are excluded.
func extractGoSymbolsAST(path string, content []byte) ([]symbolEntry, error) {
	// Skip _test.go files entirely.
	if strings.HasSuffix(filepath.Base(path), "_test.go") {
		return nil, nil
	}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, content, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	var symbols []symbolEntry

	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if shouldSkipGoFunc(d) {
				continue
			}
			name := goFuncName(d)
			line := fset.Position(d.Pos()).Line
			symbols = append(symbols, symbolEntry{Name: name, Line: line})

		case *ast.GenDecl:
			if d.Tok == token.TYPE {
				for _, spec := range d.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok {
						line := fset.Position(ts.Pos()).Line
						symbols = append(symbols, symbolEntry{
							Name: "type " + ts.Name.Name,
							Line: line,
						})
					}
				}
			}
		}
	}

	return symbols, nil
}

// shouldSkipGoFunc returns true if the function should be excluded from the
// repo map (test functions, benchmark functions, fuzz tests, blank identifiers).
func shouldSkipGoFunc(d *ast.FuncDecl) bool {
	if d.Name == nil || d.Name.Name == "_" {
		return true
	}
	name := d.Name.Name
	return strings.HasPrefix(name, "Test") ||
		strings.HasPrefix(name, "Benchmark") ||
		strings.HasPrefix(name, "Fuzz")
}

// goFuncName returns a display name for a Go function declaration.
// For methods: "(*Receiver).Method" or "(Receiver).Method"
// For functions: "func funcName"
func goFuncName(d *ast.FuncDecl) string {
	if d.Recv != nil && len(d.Recv.List) > 0 {
		recv := d.Recv.List[0]
		recvName := goRecvType(recv.Type)
		ptr := ""
		if se, ok := recv.Type.(*ast.StarExpr); ok {
			ptr = "*"
			recvName = goRecvType(se.X)
		}
		return fmt.Sprintf("func (%s%s).%s", ptr, recvName, d.Name.Name)
	}
	return "func " + d.Name.Name
}

// goRecvType extracts the type name from an expression node.
func goRecvType(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return goRecvType(e.X) + "." + e.Sel.Name
	default:
		return "?"
	}
}

// ExtractCallsAndSymbols returns both symbols and call edges for a given file.
func ExtractCallsAndSymbols(path string, content []byte) (*SymbolWithEdges, error) {
	ext := strings.ToLower(filepath.Ext(path))

	switch ext {
	case ".go":
		return extractGoSymbolsASTWithEdges(path, content)
	case ".ts", ".tsx", ".js", ".jsx", ".py":
		return extractSymbolsAndEdgesViaTreeSitter(path, ext, content)
	default:
		return nil, fmt.Errorf("unsupported file extension: %s", ext)
	}
}

// extractGoSymbolsASTWithEdges parses a Go source file and extracts both
// symbols and call edges from function bodies.
func extractGoSymbolsASTWithEdges(path string, content []byte) (*SymbolWithEdges, error) {
	// Skip _test.go files entirely.
	if strings.HasSuffix(filepath.Base(path), "_test.go") {
		return &SymbolWithEdges{}, nil
	}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, content, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	var symbols []symbolEntry
	var edges []codegraph.Edge

	for _, decl := range node.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			// Handle type declarations for symbols.
			if gd, ok2 := decl.(*ast.GenDecl); ok2 && gd.Tok == token.TYPE {
				for _, spec := range gd.Specs {
					if ts, ok3 := spec.(*ast.TypeSpec); ok3 {
						line := fset.Position(ts.Pos()).Line
						symbols = append(symbols, symbolEntry{
							Name: "type " + ts.Name.Name,
							Line: line,
						})
					}
				}
			}
			continue
		}

		if shouldSkipGoFunc(fd) {
			continue
		}

		funcName := goFuncName(fd)
		line := fset.Position(fd.Pos()).Line
		symbols = append(symbols, symbolEntry{Name: funcName, Line: line})

		// Walk the function body to find call expressions.
		if fd.Body == nil {
			continue
		}

		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			calleeName := exprToString(call.Fun)
			callLine := fset.Position(call.Pos()).Line

			edges = append(edges, codegraph.Edge{
				SourceQualifiedName: funcName,
				TargetQualifiedName: calleeName,
				EdgeType:            "calls",
				Line:                callLine,
			})
			return true
		})
	}

	return &SymbolWithEdges{Symbols: symbols, Edges: edges}, nil
}

// exprToString converts a go/ast expression to a string representation.
func exprToString(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return exprToString(v.X) + "." + v.Sel.Name
	case *ast.StarExpr:
		return "*" + exprToString(v.X)
	case *ast.ArrayType:
		return exprToString(v.Elt)
	case *ast.ParenExpr:
		return "(" + exprToString(v.X) + ")"
	case *ast.BasicLit:
		return v.Value
	case *ast.FuncLit:
		return "func(...)"
	default:
		return fmt.Sprintf("?%T?", e)
	}
}

// extractSymbolsAndEdgesViaTreeSitter uses the pkg/ast tree-sitter parser to
// extract both symbols and call edges from TS/JS/Python files.
func extractSymbolsAndEdgesViaTreeSitter(path string, ext string, content []byte) (*SymbolWithEdges, error) {
	result, err := astp.ParseFile(path, content)
	if err != nil {
		return nil, err
	}
	defer result.Release()

	var entries []symbolEntry
	for _, sym := range result.Symbols {
		prefix := symbolDisplayPrefix(sym.Kind, ext)
		entries = append(entries, symbolEntry{
			Name: prefix + " " + sym.Name,
			Line: sym.StartLine,
		})
	}

	// Convert CallEdge values to codegraph.Edge values.
	var edges []codegraph.Edge
	for _, ce := range result.Calls {
		edges = append(edges, codegraph.Edge{
			SourceQualifiedName: ce.CallerName,
			TargetQualifiedName: ce.CalleeName,
			EdgeType:            "calls",
			Line:                ce.Line,
		})
	}

	return &SymbolWithEdges{Symbols: entries, Edges: edges}, nil
}
