package tools

// Package tools: Go-specific AST symbol extraction and call graph analysis for repo_map (split from repo_map.go).

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	codegraph "github.com/sprout-foundry/sprout/pkg/codegraph"
)

// extractGoSymbolsAST parses a Go source file using go/ast and extracts
// top-level functions, methods, and type declarations as SymbolEntry values.
// Test functions (Test*, Benchmark*, Fuzz*) and _test.go files are excluded.
func extractGoSymbolsAST(path string, content []byte) ([]SymbolEntry, error) {
	// Skip _test.go files entirely.
	if strings.HasSuffix(filepath.Base(path), "_test.go") {
		return nil, nil
	}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, content, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	var symbols []SymbolEntry

	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if shouldSkipGoFunc(d) {
				continue
			}
			name := goFuncName(d)
			line := fset.Position(d.Pos()).Line
			symbols = append(symbols, SymbolEntry{Name: name, Line: line})

		case *ast.GenDecl:
			if d.Tok == token.TYPE {
				for _, spec := range d.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok {
						line := fset.Position(ts.Pos()).Line
						// Check if this is an interface type.
						if _, isIface := ts.Type.(*ast.InterfaceType); isIface {
							symbols = append(symbols, SymbolEntry{
								Name: "iface " + ts.Name.Name,
								Line: line,
							})
						} else {
							symbols = append(symbols, SymbolEntry{
								Name: "type " + ts.Name.Name,
								Line: line,
							})
						}
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

	// Build import alias → import path map for cross-package call resolution.
	// For `import "strings"` the alias is "strings" (the last path element).
	// For `import foo "pkg/bar"` the alias is "foo".
	// The import path is converted to a relative path matching the qualified
	// name format used by nodes (e.g. "pkg/codegraph" not "github.com/mod/pkg/codegraph").
	modulePath := detectGoModule(path)
	importMap := make(map[string]string) // alias → relative package path
	for _, imp := range node.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		alias := filepath.Base(importPath)
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		relPath := stripModulePrefix(importPath, modulePath)
		importMap[alias] = relPath
	}

	var symbols []SymbolEntry
	var edges []codegraph.Edge

	for _, decl := range node.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			// Handle type declarations for symbols.
			if gd, ok2 := decl.(*ast.GenDecl); ok2 && gd.Tok == token.TYPE {
				for _, spec := range gd.Specs {
					if ts, ok3 := spec.(*ast.TypeSpec); ok3 {
						line := fset.Position(ts.Pos()).Line
						// Check if this is an interface type.
						if _, isIface := ts.Type.(*ast.InterfaceType); isIface {
							symbols = append(symbols, SymbolEntry{
								Name: "iface " + ts.Name.Name,
								Line: line,
							})
						} else {
							symbols = append(symbols, SymbolEntry{
								Name: "type " + ts.Name.Name,
								Line: line,
							})
						}
					}
				}
			}

			// Handle package-level variable initializers: var x = fn().
			// Static call-graph extraction otherwise misses these because the
			// call expression has no enclosing function symbol. Emit a
			// synthetic edge from "func init" to each callee so init-time
			// callees don't show up as false-positive dead code. The matching
			// "func init" node is added to the symbol list so resolveEdgeNode
			// can find it during edge insertion.
			if gd, ok2 := decl.(*ast.GenDecl); ok2 && (gd.Tok == token.VAR || gd.Tok == token.CONST) {
				const initCaller = "func init"
				symbols = append(symbols, SymbolEntry{Name: initCaller, Line: fset.Position(gd.Pos()).Line})
				for _, spec := range gd.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, val := range vs.Values {
						ast.Inspect(val, func(n ast.Node) bool {
							call, ok := n.(*ast.CallExpr)
							if !ok {
								return true
							}
							calleeName := exprToString(call.Fun)
							callLine := fset.Position(call.Pos()).Line

							edgeType := "calls"
							if dotIdx := strings.IndexByte(calleeName, '.'); dotIdx > 0 {
								for dotIdx > 0 {
									prefix := calleeName[:dotIdx]
									if pkgPath, ok := importMap[prefix]; ok {
										calleeName = pkgPath + calleeName[dotIdx:]
										edgeType = "resolved_calls"
										break
									}
									calleeName = calleeName[dotIdx+1:]
									dotIdx = strings.IndexByte(calleeName, '.')
								}
							}

							edges = append(edges, codegraph.Edge{
								SourceQualifiedName: initCaller,
								TargetQualifiedName: calleeName,
								EdgeType:            edgeType,
								Line:                callLine,
							})

							// sync.Once.Do heuristic at file scope.
							if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Do" {
								if len(call.Args) == 1 {
									if ident, ok := call.Args[0].(*ast.Ident); ok {
										edges = append(edges, codegraph.Edge{
											SourceQualifiedName: initCaller,
											TargetQualifiedName: ident.Name,
											EdgeType:            "calls",
											Line:                callLine,
										})
									}
								}
							}
							return true
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
		symbols = append(symbols, SymbolEntry{Name: funcName, Line: line})

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

			edgeType := "calls"

			// For selector expressions (containing a dot), check whether any
			// prefix matches an import alias. Multi-level selectors like
			// "agent.state.GetOptimizer()" are receiver chains — strip all
			// non-import prefixes until we either find an import match or
			// reach the final method name.
			if dotIdx := strings.IndexByte(calleeName, '.'); dotIdx > 0 {
				for dotIdx > 0 {
					prefix := calleeName[:dotIdx]
					if pkgPath, ok := importMap[prefix]; ok {
						calleeName = pkgPath + calleeName[dotIdx:]
						edgeType = "resolved_calls"
						break
					}
					// Not an import — strip this prefix and try next level.
					calleeName = calleeName[dotIdx+1:]
					dotIdx = strings.IndexByte(calleeName, '.')
				}
			}

			edges = append(edges, codegraph.Edge{
				SourceQualifiedName: funcName,
				TargetQualifiedName: calleeName,
				EdgeType:            edgeType,
				Line:                callLine,
			})

			// sync.Once.Do heuristic: when we see x.Do(fn) where fn is a simple
			// identifier, emit a synthetic call edge from the enclosing function
			// to fn. This catches loadOnce.Do(loadCatalogs) and similar patterns
			// where the call graph is otherwise invisible to static analysis.
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Do" {
				if len(call.Args) == 1 {
					if ident, ok := call.Args[0].(*ast.Ident); ok {
						edges = append(edges, codegraph.Edge{
							SourceQualifiedName: funcName,
							TargetQualifiedName: ident.Name,
							EdgeType:            "calls",
							Line:                callLine,
						})
					}
				}
			}

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

// detectGoModule reads the module path from go.mod by walking up from the
// given source file path. Returns the module path or empty string on failure.
func detectGoModule(filePath string) string {
	dir := filepath.Dir(filePath)
	for {
		gm := filepath.Join(dir, "go.mod")
		data, err := os.ReadFile(gm)
		if err == nil {
			// Parse "module github.com/foo/bar" from first line
			for _, line := range strings.SplitN(string(data), "\n", 2) {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "module ") {
					return strings.TrimSpace(line[7:])
				}
			}
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// stripModulePrefix removes the module path prefix from an import path,
// returning the relative package path used for node qualified names.
// e.g. "github.com/foo/bar/pkg/util" with module "github.com/foo/bar" → "pkg/util"
// If the import doesn't start with the module prefix, returns the last
// path segment (for stdlib imports like "fmt" — these won't match nodes
// anyway since nodes don't have qualified names for stdlib functions).
func stripModulePrefix(importPath, modulePath string) string {
	if modulePath != "" && strings.HasPrefix(importPath, modulePath+"/") {
		return importPath[len(modulePath)+1:]
	}
	return filepath.Base(importPath)
}
