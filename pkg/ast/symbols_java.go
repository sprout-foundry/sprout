package ast

import (
	gotreesitter "github.com/odvcencio/gotreesitter"
)

// ---------------------------------------------------------------------------
// Java scoped symbol extraction
// ---------------------------------------------------------------------------

// extractJavaSymbols walks the AST for Java and extracts top-level symbols
// plus class/interface/enum members up to maxDepth.
func extractJavaSymbols(root *gotreesitter.Node, bt *gotreesitter.BoundTree, maxDepth int, lang string) []ScopedSymbol {
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
		case "class_declaration":
			name := childText(child, bt, "name")
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "class", "", child, 0))

			if maxDepth > 1 {
				symbols = append(symbols, extractJavaClassMembers(child, bt, name, lang)...)
			}

		case "interface_declaration":
			name := childText(child, bt, "name")
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "interface", "", child, 0))

			if maxDepth > 1 {
				symbols = append(symbols, extractJavaInterfaceMembers(child, bt, name)...)
			}

		case "enum_declaration":
			name := childText(child, bt, "name")
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "enum", "", child, 0))

			if maxDepth > 1 {
				symbols = append(symbols, extractJavaEnumMembers(child, bt, name)...)
			}

		case "method_declaration":
			// Top-level method (rare in Java, but possible in some contexts).
			name := childText(child, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "function", "", child, bt, 0, lang))
			}

		default:
		}
	}

	return symbols
}

// extractJavaClassMembers extracts field, constructor, and method definitions
// from a class_declaration node by walking its class_body child.
func extractJavaClassMembers(classNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope, lang string) []ScopedSymbol {
	if classNode == nil {
		return nil
	}

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
		case "method_declaration":
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "method", scope, member, bt, 1, lang))
			}

		case "constructor_declaration":
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "method", scope, member, bt, 1, lang))
			}

		case "field_declaration":
			name := extractJavaFieldName(member, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "property", scope, member, 1))
			}

		case "class_declaration":
			// Nested class.
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "class", scope, member, 1))
			}

		case "interface_declaration":
			// Nested interface.
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "interface", scope, member, 1))
			}
		}
	}

	return symbols
}

// extractJavaInterfaceMembers extracts method signatures from an interface_body.
func extractJavaInterfaceMembers(ifaceNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope string) []ScopedSymbol {
	if ifaceNode == nil {
		return nil
	}

	var body *gotreesitter.Node
	for i := 0; i < ifaceNode.ChildCount(); i++ {
		child := ifaceNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "interface_body" {
			body = child
			break
		}
	}
	if body == nil {
		return nil
	}

	var symbols []ScopedSymbol
	for i := 0; i < body.ChildCount(); i++ {
		member := body.Child(i)
		if member == nil || !member.IsNamed() {
			continue
		}
		memberType := bt.NodeType(member)

		switch memberType {
		case "method_declaration":
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "method", scope, member, 1))
			}

		case "field_declaration":
			name := extractJavaFieldName(member, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "property", scope, member, 1))
			}
		}
	}

	return symbols
}

// extractJavaEnumMembers extracts enum_constant entries from an enum_body.
func extractJavaEnumMembers(enumNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope string) []ScopedSymbol {
	if enumNode == nil {
		return nil
	}

	var body *gotreesitter.Node
	for i := 0; i < enumNode.ChildCount(); i++ {
		child := enumNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "enum_body" {
			body = child
			break
		}
	}
	if body == nil {
		return nil
	}

	var symbols []ScopedSymbol
	for i := 0; i < body.ChildCount(); i++ {
		member := body.Child(i)
		if member == nil || !member.IsNamed() {
			continue
		}
		memberType := bt.NodeType(member)

		switch memberType {
		case "enum_constant":
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "constant", scope, member, 1))
			}

		case "method_declaration":
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "method", scope, member, 1))
			}

		case "constructor_declaration":
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "method", scope, member, 1))
			}
		}
	}

	return symbols
}

// extractJavaFieldName extracts the field name from a field_declaration node.
// Java field declarations use variable_declarator children with a "name" field
// (the variable_identifier). For multi-field declarations like
// "int a, b;", only the first name is returned.
func extractJavaFieldName(fdNode *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	// Try the "name" field first.
	name := childText(fdNode, bt, "name")
	if name != "" {
		return name
	}

	// Fallback: look for variable_declarator child.
	for i := 0; i < fdNode.ChildCount(); i++ {
		child := fdNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "variable_declarator" {
			name := childText(child, bt, "name")
			if name != "" {
				return name
			}
		}
	}

	return ""
}
