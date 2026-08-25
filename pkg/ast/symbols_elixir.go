package ast

import (
	gotreesitter "github.com/odvcencio/gotreesitter"
)

// ---------------------------------------------------------------------------
// Elixir scoped symbol extraction
// ---------------------------------------------------------------------------

// extractElixirSymbols walks the AST for Elixir source and extracts modules
// (defmodule), public functions (def), private functions (defp), and macros
// (defmacro).
//
// Elixir's tree-sitter grammar is expression-oriented: everything is a `call`
// node.  The first identifier child of a call tells you the keyword:
// "defmodule", "def", "defp", "defmacro".  The function/module name lives
// inside `arguments > call > identifier` (for functions/macros) or
// `arguments > alias` (for modules).
func extractElixirSymbols(root *gotreesitter.Node, bt *gotreesitter.BoundTree, maxDepth int, lang string) []ScopedSymbol {
	if root == nil {
		return nil
	}

	var symbols []ScopedSymbol
	for i := 0; i < root.ChildCount(); i++ {
		child := root.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) != "call" {
			continue
		}

		keyword := extractElixirKeyword(child, bt)
		switch keyword {
		case "defmodule":
			name := extractElixirModuleName(child, bt)
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "module", "", child, 0))
			if maxDepth > 1 {
				symbols = append(symbols, extractElixirModuleMembers(child, bt, name, lang)...)
			}

		case "def":
			name := extractElixirFunctionName(child, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "function", "", child, bt, 0, lang))
			}

		case "defp":
			name := extractElixirFunctionName(child, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "function", "", child, bt, 0, lang))
			}

		case "defmacro":
			name := extractElixirFunctionName(child, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "function", "", child, bt, 0, lang))
			}
		}
	}

	return symbols
}

// extractElixirModuleMembers extracts functions from inside a defmodule call's
// do_block body.
func extractElixirModuleMembers(moduleNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope, lang string) []ScopedSymbol {
	// Find the do_block inside the defmodule call.
	var doBlock *gotreesitter.Node
	for i := 0; i < moduleNode.ChildCount(); i++ {
		child := moduleNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "do_block" {
			doBlock = child
			break
		}
	}
	if doBlock == nil {
		return nil
	}

	var symbols []ScopedSymbol
	for i := 0; i < doBlock.ChildCount(); i++ {
		child := doBlock.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) != "call" {
			continue
		}

		keyword := extractElixirKeyword(child, bt)
		switch keyword {
		case "def":
			name := extractElixirFunctionName(child, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "method", scope, child, bt, 1, lang))
			}

		case "defp":
			name := extractElixirFunctionName(child, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "method", scope, child, bt, 1, lang))
			}

		case "defmacro":
			name := extractElixirFunctionName(child, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "method", scope, child, bt, 1, lang))
			}

		case "defmodule":
			// Nested module — extract as nested module.
			name := extractElixirModuleName(child, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "module", scope, child, 1))
			}
		}
	}

	return symbols
}

// extractElixirKeyword returns the keyword from a call node ("def", "defp",
// "defmodule", "defmacro", etc.) by checking the first identifier child.
func extractElixirKeyword(callNode *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	for i := 0; i < callNode.ChildCount(); i++ {
		child := callNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "identifier" {
			return bt.NodeText(child)
		}
	}
	return ""
}

// extractElixirFunctionName extracts the function name from a def/defp/defmacro call.
// The name is nested: call > arguments > call > identifier.
func extractElixirFunctionName(callNode *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	// Find the arguments child.
	for i := 0; i < callNode.ChildCount(); i++ {
		child := callNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) != "arguments" {
			continue
		}
		// Inside arguments, look for a call node (the function call like "hello(name)").
		for j := 0; j < child.ChildCount(); j++ {
			arg := child.Child(j)
			if arg == nil || !arg.IsNamed() {
				continue
			}
			if bt.NodeType(arg) == "call" {
				// The function name is the first identifier inside this call.
				for k := 0; k < arg.ChildCount(); k++ {
					c := arg.Child(k)
					if c == nil || !c.IsNamed() {
						continue
					}
					if bt.NodeType(c) == "identifier" {
						return bt.NodeText(c)
					}
				}
			}
			// Direct identifier (for macro with no args like "defmacro foo").
			if bt.NodeType(arg) == "identifier" {
				return bt.NodeText(arg)
			}
		}
	}
	return ""
}

// extractElixirModuleName extracts the module name from a defmodule call.
// The name is in: call > arguments > alias (the module's alias node).
func extractElixirModuleName(callNode *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	for i := 0; i < callNode.ChildCount(); i++ {
		child := callNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) != "arguments" {
			continue
		}
		// Look for alias nodes (e.g. "MyApp" or "MyApp.SubModule").
		for j := 0; j < child.ChildCount(); j++ {
			arg := child.Child(j)
			if arg == nil || !arg.IsNamed() {
				continue
			}
			if bt.NodeType(arg) == "alias" {
				return bt.NodeText(arg)
			}
		}
	}
	return ""
}
