package ast

import (
	gotreesitter "github.com/odvcencio/gotreesitter"
)

// ---------------------------------------------------------------------------
// Bash scoped symbol extraction
// ---------------------------------------------------------------------------

// extractBashSymbols walks the AST for Bash source and extracts function
// definitions as top-level symbols.
//
// Bash grammar:
//
//	program → function_definition [name field] → word (name) | compound_statement (body)
//	          → command
//
// Only `function_definition` nodes produce symbols.  `command` nodes are
// invocations, not definitions, and are skipped.
func extractBashSymbols(root *gotreesitter.Node, bt *gotreesitter.BoundTree, maxDepth int, lang string) []ScopedSymbol {
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

		switch nodeType {
		case "function_definition":
			name := childText(child, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "function", "", child, bt, 0, lang))
			}

		default:
			// Skip command, comment, and other top-level nodes.
		}
	}

	return symbols
}
