package ast

import (
	gotreesitter "github.com/odvcencio/gotreesitter"
)

// ---------------------------------------------------------------------------
// Lua scoped symbol extraction
// ---------------------------------------------------------------------------

// extractLuaSymbols walks the AST for Lua source and extracts top-level
// functions and optionally variables.
//
// Lua grammar:
//
//	chunk → function_declaration [name field] / variable_declaration / ...
//	function_declaration → name | parameters | block
//
// The "name" field may point to a dot_index_expression (e.g. "M.process")
// or a simple identifier.  childText(node, bt, "name") handles both.
func extractLuaSymbols(root *gotreesitter.Node, bt *gotreesitter.BoundTree, maxDepth int, lang string) []ScopedSymbol {
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
		case "function_declaration":
			name := childText(child, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "function", "", child, bt, 0, lang))
			}

		case "variable_declaration":
			// Local variable assignments like "local M = {}".
			// Extract as a variable using the first identifier found.
			name := extractLuaVarName(child, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "variable", "", child, 0))
			}

		default:
			// Skip return_statement, local_function, etc.
		}
	}

	return symbols
}

// extractLuaVarName finds the first identifier in a variable_declaration.
// For "local M = {}", returns "M".  For "local a, b = 1, 2", returns "a".
func extractLuaVarName(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	// Walk children looking for the first identifier.
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "identifier" {
			return bt.NodeText(child)
		}
		// Destructured: "local a, b = ..." uses variable_list.
		if bt.NodeType(child) == "variable_list" {
			for j := 0; j < child.ChildCount(); j++ {
				v := child.Child(j)
				if v == nil || !v.IsNamed() {
					continue
				}
				if bt.NodeType(v) == "identifier" {
					return bt.NodeText(v)
				}
			}
		}
	}
	return ""
}
