// Package ast provides a unified AST parser using gotreesitter (pure Go
// tree-sitter) for Go, TypeScript, JavaScript, and Python source files.
//
// The parser pre-warms grammar blobs at init time (unless
// SPROUT_SKIP_GRAMMAR_PREWARM=1) so that the first call to ParseFile does
// not pay the grammar-loading cost.  It is safe for concurrent use: each
// call to ParseFile creates its own parser instance.
//
// Usage:
//
//	result, err := ast.ParseFile("main.go", content)
//	if err != nil { ... }
//	for _, sym := range result.Symbols {
//	    fmt.Printf("%s %s at line %d\n", sym.Kind, sym.Name, sym.StartLine)
//	}
package ast

import (
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// parser_calls.go — call-edge extraction and the symbol-building helpers,
// split out of parser.go.

// extractCalls walks the AST to find call expressions within function/method
// bodies and returns CallEdge values for each call found.
//
// It uses the symbols list to determine which function body each call falls
// inside by checking byte-range containment.
//
// Calls that occur outside any function body (e.g., in package-level
// variable initializers) are silently dropped since there is no enclosing
// function symbol to attribute them to.
func extractCalls(root *gotreesitter.Node, bt *gotreesitter.BoundTree, lang string, symbols []Symbol) []CallEdge {
	lang = strings.ToLower(lang)

	// Collect all function/method symbols that have a body (i.e., can contain calls).
	var funcSymbols []Symbol
	for _, sym := range symbols {
		if sym.Body != "" {
			funcSymbols = append(funcSymbols, sym)
		}
	}

	// Synthesize a file-level <init> symbol that spans the whole file so calls
	// in package-level variable initializers (var x = fn()) get attributed to
	// something other than dropped. The codegraph's FindDeadCode excludes
	// anything with an inbound edge, so emitting from <init> keeps init-time
	// callees alive in the graph.
	startByte := 0
	endByte := 0
	if root != nil {
		endByte = int(root.EndByte())
	}
	initSymbol := Symbol{
		Name:      "<init>",
		StartLine: 1,
		EndLine:   1,
		StartByte: startByte,
		EndByte:   endByte,
		Kind:      "function",
		Body:      "file-scope init",
	}
	funcSymbols = append(funcSymbols, initSymbol)

	if len(funcSymbols) == 0 {
		return nil
	}

	// Determine the call node type for the language.
	var callNodeType string
	switch lang {
	case "go":
		callNodeType = "call_expression"
	case "typescript", "tsx", "javascript":
		callNodeType = "call_expression"
	case "python":
		callNodeType = "call"
	case "java", "c", "cpp", "c_sharp", "rust":
		callNodeType = "call_expression"
	case "ruby":
		callNodeType = "call"
	case "php":
		callNodeType = "function_call_expression"
	case "swift":
		callNodeType = "call_expression"
	case "kotlin":
		callNodeType = "call_expression"
	case "dart":
		callNodeType = "function_invocation"
	case "lua":
		callNodeType = "function_call"
	case "bash":
		callNodeType = "command"
	default:
		return nil
	}

	var edges []CallEdge

	// Walk the entire tree looking for call nodes.
	Walk(root, bt, func(node *gotreesitter.Node, nodeType string, depth int) bool {
		if nodeType == callNodeType {
			// Extract callee name from the call expression.
			calleeName := extractCalleeName(node, bt)
			if calleeName == "" {
				return true
			}

			callByte := int(node.StartByte())
			callLine := int(node.StartPoint().Row) + 1

			// Find which function symbol contains this call.
			for _, sym := range funcSymbols {
				if callByte >= sym.StartByte && callByte <= sym.EndByte {
					edges = append(edges, CallEdge{
						CallerName: sym.Name,
						CalleeName: calleeName,
						Line:       callLine,
						CallerLine: sym.StartLine,
					})
					break
				}
			}
		}
		return true
	})

	return edges
}

// extractCalleeName extracts the callee function name from a call expression node.
func extractCalleeName(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	// The "function" field of a call_expression contains the callee.
	funcChild := bt.ChildByField(node, "function")
	if funcChild == nil {
		// Fallback: first named child.
		for i := 0; i < node.ChildCount(); i++ {
			child := node.Child(i)
			if child != nil && child.IsNamed() {
				return bt.NodeText(child)
			}
		}
		return ""
	}
	return bt.NodeText(funcChild)
}

// --- Helpers ------------------------------------------------------------------

func makeSymbol(name, kind string, node *gotreesitter.Node) Symbol {
	return Symbol{
		Name:      name,
		Kind:      kind,
		StartLine: int(node.StartPoint().Row) + 1,
		EndLine:   int(node.EndPoint().Row) + 1,
		StartByte: int(node.StartByte()),
		EndByte:   int(node.EndByte()),
	}
}

func makeSymbolWithBody(name, kind string, node *gotreesitter.Node, bt *gotreesitter.BoundTree, lang string) Symbol {
	s := makeSymbol(name, kind, node)
	s.Body = extractBody(node, bt, lang)
	return s
}

// childText returns the text of the named child field, or "" if not found.
func childText(node *gotreesitter.Node, bt *gotreesitter.BoundTree, field string) string {
	child := bt.ChildByField(node, field)
	if child == nil {
		return ""
	}
	return bt.NodeText(child)
}
