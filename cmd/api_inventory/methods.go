package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// httpMethodNames maps the http.MethodX constant suffix (the SelectorExpr.Sel
// of http.MethodGet, http.MethodPost, ...) to the wire method string.
var httpMethodNames = map[string]string{
	"MethodGet":     "GET",
	"MethodHead":    "HEAD",
	"MethodPost":    "POST",
	"MethodPut":     "PUT",
	"MethodDelete":  "DELETE",
	"MethodPatch":   "PATCH",
	"MethodOptions": "OPTIONS",
}

// methodOrder is the canonical display order for a set of methods.
var methodOrder = []string{"GET", "HEAD", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"}

// handlerIndex holds, for each top-level function in the webui package, the
// HTTP methods it constrains and the handler functions its body references.
type handlerIndex struct {
	methods map[string]map[string]bool
	calls   map[string]map[string]bool
}

func newHandlerIndex() *handlerIndex {
	return &handlerIndex{
		methods: make(map[string]map[string]bool),
		calls:   make(map[string]map[string]bool),
	}
}

func (h *handlerIndex) addMethod(fn, method string) {
	if h.methods[fn] == nil {
		h.methods[fn] = make(map[string]bool)
	}
	h.methods[fn][method] = true
}

func (h *handlerIndex) addCall(fn, callee string) {
	if h.calls[fn] == nil {
		h.calls[fn] = make(map[string]bool)
	}
	h.calls[fn][callee] = true
}

// buildHandlerIndex parses every non-test .go file in dir and records, for
// each top-level function, the HTTP methods it checks and the handler
// functions (names starting with "handle") its body references. Parsing is
// purely syntactic, so build tags are irrelevant.
func buildHandlerIndex(dir string) (*handlerIndex, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	idx := newHandlerIndex()
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		full := filepath.Join(dir, name)
		f, err := parser.ParseFile(fset, full, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", full, err)
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Name == nil || fd.Body == nil {
				continue
			}
			collectFunctionMethods(idx, fd.Name.Name, fd.Body)
		}
	}
	return idx, nil
}

// collectFunctionMethods walks a function body and records the HTTP methods
// it constrains plus the handler functions it references.
func collectFunctionMethods(idx *handlerIndex, fn string, body *ast.BlockStmt) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			recordMethodCall(idx, fn, x)
			if name := calleeName(x); strings.HasPrefix(name, "handle") {
				idx.addCall(fn, name)
			}
		case *ast.BinaryExpr:
			if m, ok := methodFromComparison(x); ok {
				idx.addMethod(fn, m)
			}
		case *ast.SwitchStmt:
			for _, m := range methodsFromSwitch(x) {
				idx.addMethod(fn, m)
			}
		}
		return true
	})
}

// recordMethodCall handles requireMethod(w, r, http.MethodX) and
// requireMethods(w, r, http.MethodA, http.MethodB, ...).
func recordMethodCall(idx *handlerIndex, fn string, call *ast.CallExpr) {
	name := calleeName(call)
	if name != "requireMethod" && name != "requireMethods" {
		return
	}
	for _, arg := range call.Args {
		if sel, ok := arg.(*ast.SelectorExpr); ok {
			if m, ok2 := httpMethodNames[sel.Sel.Name]; ok2 {
				idx.addMethod(fn, m)
			}
		}
	}
}

// methodFromComparison extracts the method from `r.Method == http.MethodX` /
// `r.Method != http.MethodX`.
func methodFromComparison(expr *ast.BinaryExpr) (string, bool) {
	left, okLeft := selectorName(expr.X)
	right, okRight := selectorName(expr.Y)
	if left == "Method" && okLeft && isMethodConst(right) {
		return httpMethodNames[stripMethodPrefix(right)], true
	}
	if right == "Method" && okRight && isMethodConst(left) {
		return httpMethodNames[stripMethodPrefix(left)], true
	}
	return "", false
}

// selectorName returns the final selector segment ("Method" for r.Method,
// "MethodGet" for http.MethodGet) and whether the expression is a selector.
func selectorName(expr ast.Expr) (string, bool) {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	return sel.Sel.Name, true
}

// isMethodConst reports whether name is an http.MethodX constant suffix.
func isMethodConst(name string) bool {
	_, ok := httpMethodNames[stripMethodPrefix(name)]
	return ok
}

func stripMethodPrefix(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return name
}

// methodsFromSwitch extracts the case methods from `switch r.Method { ... }`.
func methodsFromSwitch(sw *ast.SwitchStmt) []string {
	tag, ok := sw.Tag.(*ast.SelectorExpr)
	if !ok || tag.Sel.Name != "Method" {
		return nil
	}
	var out []string
	for _, clause := range sw.Body.List {
		cc, ok := clause.(*ast.CaseClause)
		if !ok {
			continue
		}
		for _, e := range cc.List {
			if sel, ok := e.(*ast.SelectorExpr); ok {
				if m, ok2 := httpMethodNames[sel.Sel.Name]; ok2 {
					out = append(out, m)
				}
			}
		}
	}
	return out
}

// calleeName returns the callee identifier for a CallExpr: the Sel for a
// selector call (ws.handleX -> "handleX") or the name for a plain identifier
// call. For a package-qualified call (lspproxy.BridgeHandler) it returns the
// qualified name so the caller can recognize it is not a local handler.
func calleeName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	}
	return ""
}

// resolveRouteMethod determines the Method cell for a route. The registry's
// methods take precedence when the route (or a prefix entry covering it) is
// listed; otherwise the method is derived from the handler source, with "any"
// for a handler that performs no method check.
func resolveRouteMethod(routePath, handlerBase string, registry []RegistryEntry, idx *handlerIndex) []string {
	if methods, ok := registryMethodsForPath(routePath, registry); ok && len(methods) > 0 {
		return normalizeMethods(methods)
	}
	return handlerMethods(handlerBase, idx)
}

// handlerMethods derives the method set from the handler source, with "any"
// for a handler that performs no method check.
func handlerMethods(handlerBase string, idx *handlerIndex) []string {
	if handlerBase == "" {
		return []string{"any"}
	}
	set := map[string]bool{}
	if m, ok := idx.methods[handlerBase]; ok {
		for k := range m {
			set[k] = true
		}
	}
	// Follow one level of dispatch: a dispatcher delegates to sub-handlers
	// whose bodies carry the method checks.
	for callee := range idx.calls[handlerBase] {
		if m, ok := idx.methods[callee]; ok {
			for k := range m {
				set[k] = true
			}
		}
	}
	if len(set) == 0 {
		return []string{"any"}
	}
	var out []string
	for m := range set {
		out = append(out, m)
	}
	return normalizeMethods(out)
}

// methodProvenanceNote returns a note (or "") for a route whose registry
// method set and handler method set both exist but differ, so a reader of the
// table is aware the in-browser and daemon implementations disagree.
func methodProvenanceNote(routePath, handlerBase string, registry []RegistryEntry, idx *handlerIndex) string {
	reg, regOK := registryMethodsForPath(routePath, registry)
	if !regOK || len(reg) == 0 {
		return ""
	}
	hand := handlerMethods(handlerBase, idx)
	if len(hand) == 0 || hand[0] == "any" {
		return ""
	}
	if strings.Join(reg, ",") == strings.Join(hand, ",") {
		return ""
	}
	return fmt.Sprintf("registry methods %s vs handler methods %s", strings.Join(reg, ", "), strings.Join(hand, ", "))
}

// normalizeMethods orders methods canonically and de-duplicates them.
func normalizeMethods(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range in {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	order := map[string]int{}
	for i, m := range methodOrder {
		order[m] = i
	}
	sort.SliceStable(out, func(i, j int) bool {
		oi, knownI := order[out[i]]
		oj, knownJ := order[out[j]]
		if knownI && knownJ {
			return oi < oj
		}
		if knownI {
			return true
		}
		if knownJ {
			return false
		}
		return out[i] < out[j]
	})
	return out
}

// formatMethods renders a method set for the Method column.
func formatMethods(methods []string) string {
	return strings.Join(methods, ", ")
}
