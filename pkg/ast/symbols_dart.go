package ast

import (
	gotreesitter "github.com/odvcencio/gotreesitter"
)

// ---------------------------------------------------------------------------
// Dart scoped symbol extraction
// ---------------------------------------------------------------------------

// extractDartSymbols walks the AST for Dart source and extracts top-level
// symbols plus nested class/enum members up to maxDepth.
//
// Dart grammar:
//
//	program → class_definition / enum_declaration / function_signature / ...
//	class_definition → class_body → declaration → method_signature / field
//	enum_declaration → enum_body → enum_constant
func extractDartSymbols(root *gotreesitter.Node, bt *gotreesitter.BoundTree, maxDepth int, lang string) []ScopedSymbol {
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
		case "class_definition":
			name := childText(child, bt, "name")
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "class", "", child, 0))

			if maxDepth > 1 {
				symbols = append(symbols, extractDartClassMembers(child, bt, name, lang)...)
			}

		case "enum_declaration":
			name := childText(child, bt, "name")
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "enum", "", child, 0))

			if maxDepth > 1 {
				symbols = append(symbols, extractDartEnumConstants(child, bt, name)...)
			}

		case "function_signature":
			// Top-level function. May be followed by a function_body sibling.
			name := childText(child, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "function", "", child, bt, 0, lang))
			}

		default:
			// Skip other top-level nodes (import, comment, etc.).
		}
	}

	return symbols
}

// extractDartClassMembers walks the class_body and extracts methods and fields.
//
// Dart wraps class members in `declaration` nodes.  Each `declaration`
// contains a `method_signature` (for methods, possibly followed by a
// `function_body` for the implementation) or a field identifier.
func extractDartClassMembers(classNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope, lang string) []ScopedSymbol {
	if classNode == nil {
		return nil
	}

	// Find class_body child.
	var classBody *gotreesitter.Node
	for i := 0; i < classNode.ChildCount(); i++ {
		child := classNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "class_body" {
			classBody = child
			break
		}
	}
	if classBody == nil {
		return nil
	}

	var symbols []ScopedSymbol
	for i := 0; i < classBody.ChildCount(); i++ {
		member := classBody.Child(i)
		if member == nil || !member.IsNamed() {
			continue
		}
		memberType := bt.NodeType(member)

		switch memberType {
		case "declaration":
			// Unwrap declaration and inspect children for method/field.
			for j := 0; j < member.ChildCount(); j++ {
				dm := member.Child(j)
				if dm == nil || !dm.IsNamed() {
					continue
				}
				dmType := bt.NodeType(dm)

				switch dmType {
				case "method_signature":
					name := extractDartMethodName(dm, bt)
					if name != "" {
						symbols = append(symbols, scopedSymbolWithBody(name, "method", scope, dm, bt, 1, lang))
					}

				case "function_expression":
					// Arrow/inline function expressions in field initializers.
					// Not a class member itself, skip.

				default:
					// Field: look for typed_name or identifier as the field name.
					name := extractDartFieldName(dm, bt)
					if name != "" {
						symbols = append(symbols, scopedSymbol(name, "property", scope, dm, 1))
					}
				}
			}

		case "method_signature":
			// Method directly in class_body (not wrapped in declaration).
			name := extractDartMethodName(member, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "method", scope, member, bt, 1, lang))
			}

		default:
			// Skip other nodes (block, return_statement, etc.).
		}
	}

	return symbols
}

// extractDartFieldName tries to get a field name from a node inside a
// declaration.  Dart uses `typed_name` for typed fields and bare
// `identifier` for simple fields.
func extractDartFieldName(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	nt := bt.NodeType(node)
	switch nt {
	case "typed_name":
		return childText(node, bt, "name")
	case "identifier":
		return bt.NodeText(node)
	}
	// Fallback: look for a child with a "name" field.
	name := childText(node, bt, "name")
	if name != "" {
		return name
	}
	// Last resort: first named child that's an identifier.
	for i := 0; i < node.ChildCount(); i++ {
		c := node.Child(i)
		if c == nil || !c.IsNamed() {
			continue
		}
		if bt.NodeType(c) == "identifier" {
			return bt.NodeText(c)
		}
	}
	return ""
}

// extractDartEnumConstants extracts enum_constant children from an enum_body.
func extractDartEnumConstants(enumNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope string) []ScopedSymbol {
	if enumNode == nil {
		return nil
	}

	var enumBody *gotreesitter.Node
	for i := 0; i < enumNode.ChildCount(); i++ {
		child := enumNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "enum_body" {
			enumBody = child
			break
		}
	}
	if enumBody == nil {
		return nil
	}

	var symbols []ScopedSymbol
	for i := 0; i < enumBody.ChildCount(); i++ {
		member := enumBody.Child(i)
		if member == nil || !member.IsNamed() {
			continue
		}
		if bt.NodeType(member) == "enum_constant" {
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "constant", scope, member, 1))
			}
		}
	}

	return symbols
}

// extractDartMethodName handles the nested structure of method names:
// method_signature > function_signature > identifier
// or method_signature > constructor_signature > identifier
func extractDartMethodName(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		ct := bt.NodeType(child)
		if ct == "function_signature" || ct == "constructor_signature" {
			return childText(child, bt, "name")
		}
		if ct == "identifier" {
			return bt.NodeText(child)
		}
	}
	return childText(node, bt, "name")
}
