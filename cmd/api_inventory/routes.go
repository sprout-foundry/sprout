package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
)

// Route is one entry in the route inventory: a mux pattern plus the handler
// expression registered for it and the register function it appears in.
type Route struct {
	// Path is the mux pattern exactly as registered (e.g. "/api/query",
	// "/api/edits/", or the Go 1.22 wildcard "/api/sessions/{id}/export").
	Path string
	// Handler is the display name for the Handler column (e.g. "handleAPIQuery",
	// "lspproxy.BridgeHandler", or "inline closure").
	Handler string
	// HandlerBase is the local handler function name used to derive HTTP
	// methods ("" for inline closures and package-qualified handlers, which
	// are not in the webui handler index and fall back to "any").
	HandlerBase string
	// RegisterFn is the enclosing registerXxxRoutes function.
	RegisterFn string
}

// parseRoutes parses pkg/webui/routes.go and returns every mux.HandleFunc
// registration in source order.
func parseRoutes(path string) ([]Route, error) {
	data, err := os.ReadFile(path) // #nosec G703 -- path resolves within the repo root (pkg/webui/routes.go)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, data, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var routes []Route
	ast.Inspect(f, func(n ast.Node) bool {
		fd, ok := n.(*ast.FuncDecl)
		if !ok || fd.Name == nil || fd.Body == nil {
			return true
		}
		// Attribute each HandleFunc call to the enclosing register function.
		ast.Inspect(fd.Body, func(c ast.Node) bool {
			call, ok := c.(*ast.CallExpr)
			if !ok || calleeName(call) != "HandleFunc" || len(call.Args) < 2 {
				return true
			}
			pattern, ok := stringLiteral(call.Args[0])
			if !ok {
				return true
			}
			routes = append(routes, Route{
				Path:        pattern,
				Handler:     describeHandler(call.Args[1]),
				HandlerBase: handlerBase(call.Args[1]),
				RegisterFn:  fd.Name.Name,
			})
			return true
		})
		return true
	})
	return routes, nil
}

// stringLiteral returns the value of a basic string literal expression.
func stringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind.String() != "STRING" {
		return "", false
	}
	// lit.Value includes the surrounding quotes; trim them.
	return lit.Value[1 : len(lit.Value)-1], true
}

// describeHandler renders the handler expression for the Handler column.
func describeHandler(expr ast.Expr) string {
	switch h := expr.(type) {
	case *ast.SelectorExpr:
		return h.Sel.Name
	case *ast.FuncLit:
		return "inline closure"
	case *ast.CallExpr:
		return qualifiedCalleeName(h)
	default:
		return "handler"
	}
}

// handlerBase returns the local handler function name for method derivation.
// ws.handleX -> "handleX"; a package-qualified call or an inline closure has
// no local function in the webui index, so it returns "".
func handlerBase(expr ast.Expr) string {
	if h, ok := expr.(*ast.SelectorExpr); ok {
		return h.Sel.Name
	}
	return ""
}

// qualifiedCalleeName renders a call's target with its package qualifier for
// a package-qualified call (lspproxy.BridgeHandler) or just the name for a
// plain identifier call.
func qualifiedCalleeName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		if x, ok := fun.X.(*ast.Ident); ok {
			return x.Name + "." + fun.Sel.Name
		}
		return fun.Sel.Name
	}
	return "handler"
}
