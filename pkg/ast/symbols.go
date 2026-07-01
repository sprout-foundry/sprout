// Package ast (continued) — scoped symbol extraction.
//
// This package provides ExtractSymbols, which walks the AST from root and
// extracts symbols with scope information up to a configurable nesting depth.
// Unlike the inline extractSymbols in parser.go (which only walks direct
// children of root), this implementation tracks parent scope names so that
// methods inside classes get Scope: "MyClass" and Depth: 1.
//
// The existing Symbol and helper functions (makeSymbol, childText) in parser.go
// are reused; no modifications to parser.go are required.
package ast

import (
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// DefaultMaxDepth is the default maximum nesting depth for ExtractSymbols.
// Depth 0 = top-level symbols, Depth 1 = children (methods, fields, etc.).
const DefaultMaxDepth = 2

// ScopedSymbol extends Symbol with scope/parent information and nesting depth.
type ScopedSymbol struct {
	Symbol // embed the existing Symbol for Name, Kind, StartLine, etc.

	// Scope is the parent scope path, e.g. "MyClass" for a method inside
	// a class, or "MyClass.NestedStruct" for deeper nesting.
	// Empty string for top-level symbols (Depth == 0).
	Scope string

	// Depth is the nesting level: 0 = top-level, 1 = child of top-level, etc.
	Depth int
}

// ExtractSymbols walks the AST from root and extracts symbols with scope
// information. It goes deeper than just top-level: it finds methods inside
// classes (depth 1), nested functions, class methods in Python, etc.
//
// The walk is limited to DefaultMaxDepth levels of nesting.
func ExtractSymbols(root *gotreesitter.Node, bt *gotreesitter.BoundTree, lang string) []ScopedSymbol {
	return ExtractSymbolsWithMaxDepth(root, bt, lang, DefaultMaxDepth)
}

// ExtractSymbolsWithMaxDepth is like ExtractSymbols but allows specifying
// the maximum nesting depth. A value of 1 extracts only top-level symbols;
// a value of 2 extracts top-level plus one level of nesting. Values above 2
// are currently not used for additional nesting levels.
func ExtractSymbolsWithMaxDepth(root *gotreesitter.Node, bt *gotreesitter.BoundTree, lang string, maxDepth int) []ScopedSymbol {
	if root == nil || bt == nil {
		return nil
	}

	// maxDepth ≤ 0 means no extraction at all.
	if maxDepth <= 0 {
		return nil
	}

	lang = strings.ToLower(lang)

	var symbols []ScopedSymbol

	switch lang {
	case "go":
		symbols = extractGoSymbols(root, bt, maxDepth, lang)
	case "typescript", "tsx":
		symbols = extractTSSymbols(root, bt, maxDepth, lang)
	case "javascript":
		symbols = extractTSSymbols(root, bt, maxDepth, lang) // JS shares TS node types
	case "python":
		symbols = extractPythonSymbols(root, bt, maxDepth, lang)
	default:
		// Fallback: extract top-level symbols only via the generic walker.
		symbols = extractGenericSymbols(root, bt, maxDepth, lang)
	}

	return symbols
}

// scopedSymbol creates a ScopedSymbol with the given scope and depth.
func scopedSymbol(name, kind, scope string, node *gotreesitter.Node, depth int) ScopedSymbol {
	return ScopedSymbol{
		Symbol: makeSymbol(name, kind, node),
		Scope:  scope,
		Depth:  depth,
	}
}

// scopedSymbolWithBody is like scopedSymbol but also extracts the body text
// for function/method/class symbols via extractBody. Use this for nodes where
// body extraction is meaningful (functions, methods, Python classes).
func scopedSymbolWithBody(name, kind, scope string, node *gotreesitter.Node, bt *gotreesitter.BoundTree, depth int, lang string) ScopedSymbol {
	s := scopedSymbol(name, kind, scope, node, depth)
	s.Body = extractBody(node, bt, lang)
	return s
}

// shouldSkipNode returns true for nodes that should never be extracted as
// symbols (e.g. imports, error nodes, comments).
func shouldSkipNode(nodeType string) bool {
	switch nodeType {
	case "import_declaration", "import_statement", "import_from_statement",
		"package_clause", "expression_statement",
		"ERROR", "comment", "shebang":
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Generic fallback
// ---------------------------------------------------------------------------

// extractGenericSymbols extracts top-level symbols for languages without
// a dedicated extractor. It walks direct children of root and attempts
// to classify them by common node types.
func extractGenericSymbols(root *gotreesitter.Node, bt *gotreesitter.BoundTree, maxDepth int, lang string) []ScopedSymbol {
	if root == nil || bt == nil {
		return nil
	}
	if maxDepth <= 0 {
		return nil
	}

	var symbols []ScopedSymbol

	for i := 0; i < root.ChildCount(); i++ {
		child := root.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		nodeType := bt.NodeType(child)

		if shouldSkipNode(nodeType) {
			continue
		}

		// Try to extract a name from the node.
		name := childText(child, bt, "name")
		if name == "" {
			continue
		}

		kind := guessKind(nodeType)
		switch kind {
		case "function", "method":
			symbols = append(symbols, scopedSymbolWithBody(name, kind, "", child, bt, 0, lang))
		default:
			symbols = append(symbols, scopedSymbol(name, kind, "", child, 0))
		}
	}

	return symbols
}

// guessKind maps a node type string to a best-effort symbol kind.
func guessKind(nodeType string) string {
	switch {
	case strings.Contains(nodeType, "function") || strings.Contains(nodeType, "func"):
		return "function"
	case strings.Contains(nodeType, "class"):
		return "class"
	case strings.Contains(nodeType, "interface"):
		return "interface"
	case strings.Contains(nodeType, "type"):
		return "type"
	case strings.Contains(nodeType, "enum"):
		return "enum"
	case strings.Contains(nodeType, "variable") || strings.Contains(nodeType, "var") ||
		strings.Contains(nodeType, "assignment"):
		return "variable"
	case strings.Contains(nodeType, "constant") || strings.Contains(nodeType, "const"):
		return "constant"
	case strings.Contains(nodeType, "method"):
		return "method"
	case strings.Contains(nodeType, "property") || strings.Contains(nodeType, "field"):
		return "property"
	default:
		return "symbol"
	}
}
