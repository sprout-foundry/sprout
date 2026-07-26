package ast

import (
	gotreesitter "github.com/odvcencio/gotreesitter"
)

// ---------------------------------------------------------------------------
// Ruby scoped symbol extraction
// ---------------------------------------------------------------------------

// extractRubySymbols walks the AST for Ruby source and extracts top-level
// symbols plus nested class/module members up to maxDepth.
//
// Ruby classes and modules have their name in a "constant" child node.
// Class/module members are nested inside a "body_statement" child.
// Top-level "method" nodes are standalone functions.
func extractRubySymbols(root *gotreesitter.Node, bt *gotreesitter.BoundTree, maxDepth int, lang string) []ScopedSymbol {
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
		case "method", "singleton_method":
			// Top-level method is a standalone function (not inside a class/module).
			name := childText(child, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "function", "", child, bt, 0, lang))
			}

		case "class", "module":
			name := extractRubyClassName(child, bt)
			if name == "" {
				continue
			}
			kind := "class"
			symbols = append(symbols, scopedSymbolWithBody(name, kind, "", child, bt, 0, lang))

			// Extract nested members if we have depth budget.
			if maxDepth > 1 {
				symbols = append(symbols, extractRubyClassMembers(child, bt, name, lang)...)
			}

		case "sclass":
			// Singleton class (class << self). Name from the self reference.
			// Skip for now — no meaningful name to extract.
		}
	}

	return symbols
}

// extractRubyClassName returns the class/module name from a class or module node.
// The name is stored in a "constant" child (e.g. "Foo::Bar" from "class Foo::Bar").
// We try the "name" field first, then fall back to the constant child text.
func extractRubyClassName(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	name := childText(node, bt, "name")
	if name != "" {
		return name
	}
	// Fallback: look for a "constant" child node.
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "constant" {
			return bt.NodeText(child)
		}
	}
	return ""
}

// extractRubyClassMembers walks the body_statement of a class/module node
// and extracts method and singleton_method declarations.
func extractRubyClassMembers(classNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope, lang string) []ScopedSymbol {
	if classNode == nil {
		return nil
	}

	// Find body_statement child.
	var bodyStmt *gotreesitter.Node
	for i := 0; i < classNode.ChildCount(); i++ {
		child := classNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "body_statement" {
			bodyStmt = child
			break
		}
	}
	if bodyStmt == nil {
		return nil
	}

	var symbols []ScopedSymbol
	for i := 0; i < bodyStmt.ChildCount(); i++ {
		member := bodyStmt.Child(i)
		if member == nil || !member.IsNamed() {
			continue
		}
		memberType := bt.NodeType(member)

		switch memberType {
		case "method", "singleton_method":
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "method", scope, member, bt, 1, lang))
			}

		case "call":
			// Skip call nodes (attr_accessor, include, etc.) — these are
			// expressions, not declarations.
		}
	}

	return symbols
}
