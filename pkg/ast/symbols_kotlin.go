package ast

import (
	gotreesitter "github.com/odvcencio/gotreesitter"
)

// ---------------------------------------------------------------------------
// Kotlin scoped symbol extraction
// ---------------------------------------------------------------------------

// extractKotlinSymbols walks the AST for Kotlin source and extracts top-level
// symbols plus nested class/object/interface members up to maxDepth.
//
// Kotlin grammar parsing can be fragile (producing ERROR nodes for certain
// constructs). This extractor is defensive: it skips ERROR nodes, falls back
// to looking for type_identifier/simple_identifier children when the "name"
// field is empty, and returns empty results rather than crashing on malformed
// input.
func extractKotlinSymbols(root *gotreesitter.Node, bt *gotreesitter.BoundTree, maxDepth int, lang string) []ScopedSymbol {
	if root == nil || bt == nil {
		return nil
	}

	// If the root itself is an ERROR node, the file failed to parse entirely.
	if bt.NodeType(root) == "ERROR" {
		return nil
	}

	var symbols []ScopedSymbol

	for i := 0; i < root.ChildCount(); i++ {
		child := root.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		nodeType := bt.NodeType(child)

		// Skip ERROR nodes and known noise.
		if shouldSkipNode(nodeType) {
			continue
		}
		if nodeType == "ERROR" {
			continue
		}

		switch nodeType {
		case "class_declaration":
			name := extractKotlinClassName(child, bt)
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "class", "", child, 0))

			if maxDepth > 1 {
				symbols = append(symbols, extractKotlinClassMembers(child, bt, name, lang)...)
			}

		case "object_declaration":
			name := extractKotlinClassName(child, bt)
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "class", "", child, 0))

			if maxDepth > 1 {
				symbols = append(symbols, extractKotlinClassMembers(child, bt, name, lang)...)
			}

		case "interface_declaration":
			name := extractKotlinClassName(child, bt)
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "interface", "", child, 0))

			if maxDepth > 1 {
				symbols = append(symbols, extractKotlinInterfaceMembers(child, bt, name)...)
			}

		case "function_declaration":
			name := extractKotlinFunctionName(child, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "function", "", child, bt, 0, lang))
			}

		case "enum_class_declaration":
			name := extractKotlinClassName(child, bt)
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "enum", "", child, 0))

			if maxDepth > 1 {
				symbols = append(symbols, extractKotlinEnumMembers(child, bt, name)...)
			}

		case "type_alias":
			name := childText(child, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "type", "", child, 0))
			}

		case "import_list":
			// Skip import lists.

		case "package_declaration":
			// Skip package declarations.
		}
	}

	return symbols
}

// extractKotlinClassName returns the name from a class/object/interface node.
// Try "name" field first, then fall back to type_identifier child.
func extractKotlinClassName(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	name := childText(node, bt, "name")
	if name != "" {
		return name
	}
	// Fallback: look for type_identifier child.
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "type_identifier" {
			return bt.NodeText(child)
		}
	}
	return ""
}

// extractKotlinFunctionName returns the name from a function_declaration.
// Try "name" field first, then fall back to simple_identifier/type_identifier.
func extractKotlinFunctionName(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	name := childText(node, bt, "name")
	if name != "" {
		return name
	}
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		nt := bt.NodeType(child)
		if nt == "simple_identifier" || nt == "type_identifier" {
			return bt.NodeText(child)
		}
	}
	return ""
}

// extractKotlinClassMembers walks the class_body of a class/object node
// and extracts function declarations and properties.
func extractKotlinClassMembers(classNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope, lang string) []ScopedSymbol {
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

		if memberType == "ERROR" {
			continue
		}

		switch memberType {
		case "function_declaration":
			name := extractKotlinFunctionName(member, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "method", scope, member, bt, 1, lang))
			}

		case "property_declaration":
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "property", scope, member, 1))
			}

		case "class_declaration":
			// Nested class.
			name := extractKotlinClassName(member, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "class", scope, member, 1))
			}

		case "interface_declaration":
			// Nested interface.
			name := extractKotlinClassName(member, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "interface", scope, member, 1))
			}
		}
	}

	return symbols
}

// extractKotlinInterfaceMembers extracts method declarations from an interface_body.
func extractKotlinInterfaceMembers(ifaceNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope string) []ScopedSymbol {
	if ifaceNode == nil {
		return nil
	}

	var body *gotreesitter.Node
	for i := 0; i < ifaceNode.ChildCount(); i++ {
		child := ifaceNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "class_body" || bt.NodeType(child) == "interface_body" {
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

		if memberType == "ERROR" {
			continue
		}

		switch memberType {
		case "function_declaration":
			name := extractKotlinFunctionName(member, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "method", scope, member, 1))
			}

		case "property_declaration":
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "property", scope, member, 1))
			}
		}
	}

	return symbols
}

// extractKotlinEnumMembers extracts enum entries from an enum_class_declaration.
func extractKotlinEnumMembers(enumNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope string) []ScopedSymbol {
	if enumNode == nil {
		return nil
	}

	var classBody *gotreesitter.Node
	for i := 0; i < enumNode.ChildCount(); i++ {
		child := enumNode.Child(i)
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

		if memberType == "ERROR" {
			continue
		}

		switch memberType {
		case "enum_entry":
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "constant", scope, member, 1))
			}

		case "function_declaration":
			name := extractKotlinFunctionName(member, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "method", scope, member, 1))
			}

		case "property_declaration":
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "property", scope, member, 1))
			}
		}
	}

	return symbols
}
