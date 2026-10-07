package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
)

// plainRouteFiles are the webui source files (relative to the repo root) that
// register plain mux.HandleFunc routes through a registerXxxRoutes function.
// Huma operations are NOT parsed from source; they are read in-process by
// humaOpsInProcess (huma_routes.go) and merged in buildRouteSet, so the
// inventory reflects the live registration set even as the huma.Register calls
// move between files.
var plainRouteFiles = []string{
	"pkg/webui/routes.go",
	"pkg/webui/api_preview.go",
	"pkg/webui/automations_api.go",
	"pkg/webui/changes_api.go",
}

// Route is one entry in the route inventory: a mux pattern plus the handler
// expression registered for it and the register function it appears in.
type Route struct {
	// Path is the mux pattern exactly as registered (e.g. "/api/query",
	// "/api/edits/", or the Go 1.22 wildcard "/api/sessions/{id}/export").
	Path string
	// Handler is the display name for the Handler column (e.g. "handleAPIQuery",
	// "lspproxy.BridgeHandler", an inline closure, or a Huma operationId).
	Handler string
	// HandlerBase is the local handler function name used to derive HTTP
	// methods ("" for inline closures and package-qualified handlers, which
	// are not in the webui handler index, and for Huma operations, which use
	// ExplicitMethod instead).
	HandlerBase string
	// RegisterFn is the enclosing registerXxxRoutes function, or "huma" for
	// operations read from the in-process API object.
	RegisterFn string
	// ExplicitMethod is the single HTTP method a route is registered with,
	// for Huma operations read from the in-process API object (the OpenAPI
	// spec's method key, upper-cased). Empty for plain mux routes, which have
	// no single registered method and derive their method set from the
	// registry or the handler source.
	ExplicitMethod string
}

// parsePlainRoutes parses every plain-route registration file and returns every
// plain mux.HandleFunc registration in source order. Huma operations are not
// parsed here; they are read in-process and merged by buildRouteSet.
func parsePlainRoutes(root string) ([]Route, error) {
	var routes []Route
	for _, rel := range plainRouteFiles {
		path := filepath.Join(root, rel)
		data, err := os.ReadFile(path) // #nosec G703 -- paths resolve within the repo root
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", rel, err)
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, data, 0)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", rel, err)
		}
		fileRoutes, err := parsePlainRoutesFromAST(f)
		if err != nil {
			return nil, err
		}
		routes = append(routes, fileRoutes...)
	}
	return routes, nil
}

// parsePlainRoutesFromAST extracts the plain mux.HandleFunc registrations from
// a parsed file. For each enclosing registerXxxRoutes function it records the
// pattern, the handler expression, and the enclosing function name.
func parsePlainRoutesFromAST(f *ast.File) ([]Route, error) {
	var routes []Route
	ast.Inspect(f, func(n ast.Node) bool {
		fd, ok := n.(*ast.FuncDecl)
		if !ok || fd.Name == nil || fd.Body == nil {
			return true
		}
		// Attribute each registration to the enclosing register function.
		ast.Inspect(fd.Body, func(c ast.Node) bool {
			call, ok := c.(*ast.CallExpr)
			if !ok {
				return true
			}
			// Plain mux.HandleFunc registrations.
			if calleeName(call) == "HandleFunc" && len(call.Args) >= 2 {
				if pattern, ok := stringLiteral(call.Args[0]); ok {
					routes = append(routes, Route{
						Path:        pattern,
						Handler:     describeHandler(call.Args[1]),
						HandlerBase: handlerBase(call.Args[1]),
						RegisterFn:  fd.Name.Name,
					})
				}
			}
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
