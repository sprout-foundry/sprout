package ast

import (
	gotreesitter "github.com/odvcencio/gotreesitter"
)

// ---------------------------------------------------------------------------
// PHP scoped symbol extraction
// ---------------------------------------------------------------------------

// extractPHPSymbols walks the AST for PHP source and extracts top-level symbols
// plus nested class/interface/enum members up to maxDepth.
//
// PHP class/interface members are in a "declaration_list" child.
// PHP enum members are in an "enum_declaration_list" child.
// Property names are nested inside "property_element" > "variable_name".
func extractPHPSymbols(root *gotreesitter.Node, bt *gotreesitter.BoundTree, maxDepth int, lang string) []ScopedSymbol {
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
		case "php_tag":
			// Skip the opening <?php tag.

		case "namespace_definition":
			// Skip namespace declarations at top level.
			// The namespace scope is implicit from the file.

		case "function_definition":
			name := childText(child, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "function", "", child, bt, 0, lang))
			}

		case "class_declaration":
			name := childText(child, bt, "name")
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "class", "", child, 0))

			if maxDepth > 1 {
				symbols = append(symbols, extractPHPClassMembers(child, bt, name, lang)...)
			}

		case "interface_declaration":
			name := childText(child, bt, "name")
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "interface", "", child, 0))

			if maxDepth > 1 {
				symbols = append(symbols, extractPHPInterfaceMembers(child, bt, name)...)
			}

		case "enum_declaration":
			name := childText(child, bt, "name")
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "enum", "", child, 0))

			if maxDepth > 1 {
				symbols = append(symbols, extractPHPEnumMembers(child, bt, name)...)
			}
		}
	}

	return symbols
}

// extractPHPClassMembers extracts properties, constants, and methods from a
// class_declaration node by walking its declaration_list child.
func extractPHPClassMembers(classNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope, lang string) []ScopedSymbol {
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

		case "property_declaration":
			name := extractPHPPropertyName(member, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "property", scope, member, 1))
			}

		case "const_declaration":
			for j := 0; j < member.ChildCount(); j++ {
				ce := member.Child(j)
				if ce == nil || !ce.IsNamed() {
					continue
				}
				if bt.NodeType(ce) == "const_element" {
					name := childText(ce, bt, "name")
					if name != "" {
						symbols = append(symbols, scopedSymbol(name, "constant", scope, ce, 1))
					}
				}
			}
		}
	}

	return symbols
}

// extractPHPInterfaceMembers extracts method declarations from an
// interface_declaration by walking its declaration_list child.
func extractPHPInterfaceMembers(ifaceNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope string) []ScopedSymbol {
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
		}
	}

	return symbols
}

// extractPHPEnumMembers extracts enum case entries from an enum_declaration.
// Enum cases appear in an enum_declaration_list child.
func extractPHPEnumMembers(enumNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope string) []ScopedSymbol {
	if enumNode == nil {
		return nil
	}

	// Try enum_declaration_list first (PHP 8.1+ enums).
	var declList *gotreesitter.Node
	for i := 0; i < enumNode.ChildCount(); i++ {
		child := enumNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "enum_declaration_list" {
			declList = child
			break
		}
	}

	// Fallback: if enum cases are in declaration_list (different grammar versions).
	if declList == nil {
		for i := 0; i < enumNode.ChildCount(); i++ {
			child := enumNode.Child(i)
			if child == nil || !child.IsNamed() {
				continue
			}
			if bt.NodeType(child) == "declaration_list" {
				declList = child
				break
			}
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
		case "enum_case":
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "constant", scope, member, 1))
			}

		case "method_declaration":
			// Enum can have methods.
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "method", scope, member, 1))
			}
		}
	}

	return symbols
}

// extractPHPPropertyName returns the property name from a property_declaration node.
// PHP property names include the '$' prefix. We strip it for consistency.
func extractPHPPropertyName(propNode *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	name := childText(propNode, bt, "name")
	if name != "" {
		return stripPHPDollarSign(name)
	}

	// Fallback: look for property_element > variable_name.
	for i := 0; i < propNode.ChildCount(); i++ {
		child := propNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "property_element" {
			for j := 0; j < child.ChildCount(); j++ {
				pe := child.Child(j)
				if pe == nil || !pe.IsNamed() {
					continue
				}
				if bt.NodeType(pe) == "variable_name" {
					return stripPHPDollarSign(bt.NodeText(pe))
				}
			}
		}
	}

	// Last fallback: look for any variable_name child directly.
	for i := 0; i < propNode.ChildCount(); i++ {
		child := propNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "variable_name" {
			return stripPHPDollarSign(bt.NodeText(child))
		}
	}

	return ""
}

// stripPHPDollarSign removes leading '$' from PHP variable names.
func stripPHPDollarSign(name string) string {
	if len(name) > 0 && name[0] == '$' {
		return name[1:]
	}
	return name
}
