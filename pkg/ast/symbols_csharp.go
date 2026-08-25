package ast

import (
	gotreesitter "github.com/odvcencio/gotreesitter"
)

// ---------------------------------------------------------------------------
// C# scoped symbol extraction
// ---------------------------------------------------------------------------

// extractCSharpSymbols walks the AST for C# and extracts top-level symbols
// plus class/interface/enum members up to maxDepth.
func extractCSharpSymbols(root *gotreesitter.Node, bt *gotreesitter.BoundTree, maxDepth int, lang string) []ScopedSymbol {
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
				symbols = append(symbols, extractCSharpClassMembers(child, bt, name, lang)...)
			}

		case "interface_declaration":
			name := childText(child, bt, "name")
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "interface", "", child, 0))

			if maxDepth > 1 {
				symbols = append(symbols, extractCSharpInterfaceMembers(child, bt, name)...)
			}

		case "enum_declaration":
			name := childText(child, bt, "name")
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "enum", "", child, 0))

			if maxDepth > 1 {
				symbols = append(symbols, extractCSharpEnumMembers(child, bt, name)...)
			}

		case "method_declaration":
			// Top-level method (rare in C#).
			name := childText(child, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "function", "", child, bt, 0, lang))
			}

		case "namespace_declaration":
			// Extract namespace as a module-level symbol.
			name := childText(child, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "module", "", child, 0))

				// Recurse into namespace body for nested declarations.
				if maxDepth > 1 {
					symbols = append(symbols, extractCSharpNamespaceContents(child, bt, name, maxDepth, lang)...)
				}
			}

		default:
		}
	}

	return symbols
}

// extractCSharpNamespaceContents walks a namespace body for nested type declarations.
func extractCSharpNamespaceContents(nsNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope string, maxDepth int, lang string) []ScopedSymbol {
	if nsNode == nil {
		return nil
	}

	var symbols []ScopedSymbol
	for i := 0; i < nsNode.ChildCount(); i++ {
		child := nsNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		nodeType := bt.NodeType(child)

		switch nodeType {
		case "class_declaration":
			name := childText(child, bt, "name")
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "class", scope, child, 1))

		case "interface_declaration":
			name := childText(child, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "interface", scope, child, 1))
			}

		case "enum_declaration":
			name := childText(child, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "enum", scope, child, 1))
			}

		case "method_declaration":
			name := childText(child, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "function", scope, child, bt, 1, lang))
			}

		case "struct_declaration":
			name := childText(child, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "class", scope, child, 1))
			}
		}
	}

	return symbols
}

// extractCSharpClassMembers extracts field, constructor, and method definitions
// from a class_declaration node by walking its declaration_list child.
func extractCSharpClassMembers(classNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope, lang string) []ScopedSymbol {
	if classNode == nil {
		return nil
	}

	var declList *gotreesitter.Node
	for i := 0; i < classNode.ChildCount(); i++ {
		child := classNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "declaration_list" {
			declList = child
			break
		}
	}
	if declList == nil {
		return nil
	}

	var symbols []ScopedSymbol
	for i := 0; i < declList.ChildCount(); i++ {
		member := declList.Child(i)
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
			name := extractCSharpFieldName(member, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "property", scope, member, 1))
			}

		case "property_declaration":
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "property", scope, member, 1))
			}

		case "event_declaration":
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "property", scope, member, 1))
			}

		case "indexer_declaration":
			symbols = append(symbols, scopedSymbol("this", "property", scope, member, 1))

		case "class_declaration":
			// Nested class.
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "class", scope, member, 1))
			}

		case "struct_declaration":
			// Nested struct.
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

		case "enum_declaration":
			// Nested enum.
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "enum", scope, member, 1))
			}
		}
	}

	return symbols
}

// extractCSharpInterfaceMembers extracts method signatures from a C# interface.
func extractCSharpInterfaceMembers(ifaceNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope string) []ScopedSymbol {
	if ifaceNode == nil {
		return nil
	}

	var declList *gotreesitter.Node
	for i := 0; i < ifaceNode.ChildCount(); i++ {
		child := ifaceNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "declaration_list" {
			declList = child
			break
		}
	}
	if declList == nil {
		return nil
	}

	var symbols []ScopedSymbol
	for i := 0; i < declList.ChildCount(); i++ {
		member := declList.Child(i)
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

		case "property_declaration":
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "property", scope, member, 1))
			}

		case "event_declaration":
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "property", scope, member, 1))
			}
		}
	}

	return symbols
}

// extractCSharpEnumMembers extracts enum_member_declaration entries from an
// enum_declaration node.
func extractCSharpEnumMembers(enumNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope string) []ScopedSymbol {
	if enumNode == nil {
		return nil
	}

	var memberList *gotreesitter.Node
	for i := 0; i < enumNode.ChildCount(); i++ {
		child := enumNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "enum_member_declaration_list" {
			memberList = child
			break
		}
	}
	if memberList == nil {
		return nil
	}

	var symbols []ScopedSymbol
	for i := 0; i < memberList.ChildCount(); i++ {
		member := memberList.Child(i)
		if member == nil || !member.IsNamed() {
			continue
		}
		if bt.NodeType(member) != "enum_member_declaration" {
			continue
		}
		name := childText(member, bt, "name")
		if name == "" {
			// The grammar doesn't map the identifier to a "name" field.
			// Fall back to finding the identifier child directly.
			for j := 0; j < member.ChildCount(); j++ {
				c := member.Child(j)
				if c == nil || !c.IsNamed() {
					continue
				}
				if bt.NodeType(c) == "identifier" {
					name = bt.NodeText(c)
					break
				}
			}
		}
		if name != "" {
			symbols = append(symbols, scopedSymbol(name, "constant", scope, member, 1))
		}
	}

	return symbols
}

// extractCSharpFieldName extracts the field name from a C# field_declaration node.
// C# field declarations contain a variable_declaration child with one or more
// variable_declarator children, each having a "name" field (the identifier).
// For multi-field declarations like "int a, b;", only the first name is returned.
func extractCSharpFieldName(fdNode *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	// Try "name" field first.
	name := childText(fdNode, bt, "name")
	if name != "" {
		return name
	}

	// Look for variable_declaration child.
	for i := 0; i < fdNode.ChildCount(); i++ {
		child := fdNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "variable_declaration" {
			// Look for variable_declarator within.
			for j := 0; j < child.ChildCount(); j++ {
				vd := child.Child(j)
				if vd == nil || !vd.IsNamed() {
					continue
				}
				if bt.NodeType(vd) == "variable_declarator" {
					name := childText(vd, bt, "name")
					if name != "" {
						return name
					}
				}
			}
		}
	}

	return ""
}
