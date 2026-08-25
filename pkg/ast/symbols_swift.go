package ast

import (
	gotreesitter "github.com/odvcencio/gotreesitter"
)

// ---------------------------------------------------------------------------
// Swift scoped symbol extraction
// ---------------------------------------------------------------------------

// extractSwiftSymbols walks the AST for Swift source and extracts top-level
// symbols plus nested class/struct/protocol members up to maxDepth.
//
// Swift class/struct names use type_identifier children; function names use
// simple_identifier children. We try the "name" field first then fall back.
func extractSwiftSymbols(root *gotreesitter.Node, bt *gotreesitter.BoundTree, maxDepth int, lang string) []ScopedSymbol {
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
		case "import_declaration":
			// Skip imports.

		case "class_declaration":
			name := extractSwiftClassName(child, bt)
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "class", "", child, 0))

			if maxDepth > 1 {
				symbols = append(symbols, extractSwiftClassMembers(child, bt, name, lang)...)
			}

		case "struct_declaration":
			name := extractSwiftStructName(child, bt)
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "class", "", child, 0))

			if maxDepth > 1 {
				symbols = append(symbols, extractSwiftClassMembers(child, bt, name, lang)...)
			}

		case "protocol_declaration":
			name := extractSwiftProtocolName(child, bt)
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "interface", "", child, 0))

			if maxDepth > 1 {
				symbols = append(symbols, extractSwiftProtocolMembers(child, bt, name)...)
			}

		case "enum_declaration":
			name := extractSwiftEnumName(child, bt)
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "enum", "", child, 0))

			if maxDepth > 1 {
				symbols = append(symbols, extractSwiftEnumMembers(child, bt, name)...)
			}

		case "function_declaration":
			name := extractSwiftFunctionName(child, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "function", "", child, bt, 0, lang))
			}

		case "actor_declaration":
			name := extractSwiftClassName(child, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "class", "", child, 0))
				if maxDepth > 1 {
					symbols = append(symbols, extractSwiftClassMembers(child, bt, name, lang)...)
				}
			}

		case "extension_declaration":
			// Extensions are not top-level types — skip.
		}
	}

	return symbols
}

// extractSwiftClassName returns the name from a class_declaration.
// The name field may point to a type_identifier child.
func extractSwiftClassName(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
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

// extractSwiftStructName returns the name from a struct_declaration.
func extractSwiftStructName(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	name := childText(node, bt, "name")
	if name != "" {
		return name
	}
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

// extractSwiftProtocolName returns the name from a protocol_declaration.
func extractSwiftProtocolName(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	name := childText(node, bt, "name")
	if name != "" {
		return name
	}
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

// extractSwiftEnumName returns the name from an enum_declaration.
func extractSwiftEnumName(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	name := childText(node, bt, "name")
	if name != "" {
		return name
	}
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

// extractSwiftFunctionName returns the name from a function_declaration.
// The name may be in a simple_identifier child.
func extractSwiftFunctionName(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	name := childText(node, bt, "name")
	if name != "" {
		return name
	}
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "simple_identifier" {
			return bt.NodeText(child)
		}
	}
	return ""
}

// extractSwiftClassMembers extracts properties, methods, and initializers
// from a class or struct by walking the class_body child.
func extractSwiftClassMembers(classNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope, lang string) []ScopedSymbol {
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
		case "property_declaration":
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "property", scope, member, 1))
			}

		case "function_declaration":
			name := extractSwiftFunctionName(member, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "method", scope, member, bt, 1, lang))
			}

		case "init_declaration":
			// Initializer — name is "init".
			symbols = append(symbols, scopedSymbolWithBody("init", "method", scope, member, bt, 1, lang))

		case "deinit_declaration":
			symbols = append(symbols, scopedSymbolWithBody("deinit", "method", scope, member, bt, 1, lang))

		case "enum_class_body":
			// Enum entries nested inside a class-like enum structure.
			symbols = append(symbols, extractSwiftEnumEntries(member, bt, scope)...)
		}
	}

	return symbols
}

// extractSwiftProtocolMembers extracts function declarations from a protocol_body.
func extractSwiftProtocolMembers(protoNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope string) []ScopedSymbol {
	if protoNode == nil {
		return nil
	}

	var body *gotreesitter.Node
	for i := 0; i < protoNode.ChildCount(); i++ {
		child := protoNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "protocol_body" {
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
		case "protocol_function_declaration":
			name := childText(member, bt, "name")
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

// extractSwiftEnumMembers extracts enum case entries from an enum_declaration.
func extractSwiftEnumMembers(enumNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope string) []ScopedSymbol {
	if enumNode == nil {
		return nil
	}

	// Enum cases are inside an enum_declaration body. Look for enum_entry nodes.
	var symbols []ScopedSymbol
	for i := 0; i < enumNode.ChildCount(); i++ {
		child := enumNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		childType := bt.NodeType(child)

		switch childType {
		case "enum_entry":
			name := childText(child, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "constant", scope, child, 1))
			}

		case "class_body", "enum_class_body":
			// Enum entries might be nested in a body node.
			symbols = append(symbols, extractSwiftEnumEntries(child, bt, scope)...)

		case "function_declaration":
			name := extractSwiftFunctionName(child, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "method", scope, child, 1))
			}
		}
	}

	return symbols
}

// extractSwiftEnumEntries looks for enum_entry nodes inside a body node.
func extractSwiftEnumEntries(bodyNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope string) []ScopedSymbol {
	var symbols []ScopedSymbol
	for i := 0; i < bodyNode.ChildCount(); i++ {
		child := bodyNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "enum_entry" {
			name := childText(child, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "constant", scope, child, 1))
			}
		}
	}
	return symbols
}
