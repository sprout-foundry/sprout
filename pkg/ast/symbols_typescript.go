package ast

import (
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// ---------------------------------------------------------------------------
// TypeScript / JavaScript scoped symbol extraction
// ---------------------------------------------------------------------------

// extractTSSymbols walks the AST for TS/JS and extracts top-level symbols plus
// class/interface members and enum values up to maxDepth.
func extractTSSymbols(root *gotreesitter.Node, bt *gotreesitter.BoundTree, maxDepth int, lang string) []ScopedSymbol {
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

		case "class_declaration":
			name := childText(child, bt, "name")
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "class", "", child, 0))

			// Extract class body members if we have depth budget.
			if maxDepth > 1 {
				symbols = append(symbols, extractTSClassMembers(child, bt, name, lang)...)
			}

		case "interface_declaration":
			name := childText(child, bt, "name")
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "interface", "", child, 0))

			// Extract interface body members if we have depth budget.
			if maxDepth > 1 {
				symbols = append(symbols, extractTSInterfaceMembers(child, bt, name)...)
			}

		case "type_alias_declaration":
			name := childText(child, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "type", "", child, 0))
			}

		case "enum_declaration":
			name := childText(child, bt, "name")
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "enum", "", child, 0))

			// Extract enum members if we have depth budget.
			if maxDepth > 1 {
				symbols = append(symbols, extractTSEnumMembers(child, bt, name)...)
			}

		case "lexical_declaration", "variable_declaration":
			for j := 0; j < child.ChildCount(); j++ {
				decl := child.Child(j)
				if decl == nil || !decl.IsNamed() {
					continue
				}
				if bt.NodeType(decl) == "variable_declarator" {
					name := childText(decl, bt, "name")
					if name != "" {
						symbols = append(symbols, scopedSymbol(name, "variable", "", decl, 0))
					}
					break // Only first declarator for top-level symbol.
				}
			}

		case "export_statement":
			// Unwrap export and recurse into the exported declaration.
			for j := 0; j < child.ChildCount(); j++ {
				ec := child.Child(j)
				if ec == nil || !ec.IsNamed() {
					continue
				}
				ecType := bt.NodeType(ec)
				exported := extractTSSymbolScoped(ec, bt, ecType, "", maxDepth, lang)
				if len(exported) > 0 {
					symbols = append(symbols, exported...)
				}
			}

		case "ambient_declaration":
			// Unwrap ambient (declare) and recurse.
			for j := 0; j < child.ChildCount(); j++ {
				ec := child.Child(j)
				if ec == nil || !ec.IsNamed() {
					continue
				}
				ecType := bt.NodeType(ec)
				ambient := extractTSSymbolScoped(ec, bt, ecType, "", maxDepth, lang)
				if len(ambient) > 0 {
					symbols = append(symbols, ambient...)
				}
			}

		default:
			// Skip unrecognised top-level nodes.
		}
	}

	return symbols
}

// extractTSSymbolScoped extracts a single scoped symbol (with members) from a
// TS/JS node, given the current scope prefix.
func extractTSSymbolScoped(node *gotreesitter.Node, bt *gotreesitter.BoundTree, nodeType, scopePrefix string, maxDepth int, lang string) []ScopedSymbol {
	var symbols []ScopedSymbol
	currentDepth := 0
	if scopePrefix != "" {
		currentDepth = 1
	}

	switch nodeType {
	case "function_declaration":
		name := childText(node, bt, "name")
		if name != "" {
			symbols = append(symbols, scopedSymbolWithBody(name, "function", scopePrefix, node, bt, currentDepth, lang))
		}

	case "class_declaration":
		name := childText(node, bt, "name")
		if name == "" {
			break
		}
		if scopePrefix == "" {
			symbols = append(symbols, scopedSymbol(name, "class", "", node, 0))
		} else {
			symbols = append(symbols, scopedSymbol(name, "class", scopePrefix, node, currentDepth))
		}
		if maxDepth > currentDepth+1 {
			symbols = append(symbols, extractTSClassMembers(node, bt, name, lang)...)
		}

	case "interface_declaration":
		name := childText(node, bt, "name")
		if name == "" {
			break
		}
		if scopePrefix == "" {
			symbols = append(symbols, scopedSymbol(name, "interface", "", node, 0))
		} else {
			symbols = append(symbols, scopedSymbol(name, "interface", scopePrefix, node, currentDepth))
		}
		if maxDepth > currentDepth+1 {
			symbols = append(symbols, extractTSInterfaceMembers(node, bt, name)...)
		}

	case "type_alias_declaration":
		name := childText(node, bt, "name")
		if name != "" {
			symbols = append(symbols, scopedSymbol(name, "type", scopePrefix, node, currentDepth))
		}

	case "enum_declaration":
		name := childText(node, bt, "name")
		if name == "" {
			break
		}
		if scopePrefix == "" {
			symbols = append(symbols, scopedSymbol(name, "enum", "", node, 0))
		} else {
			symbols = append(symbols, scopedSymbol(name, "enum", scopePrefix, node, currentDepth))
		}
		if maxDepth > currentDepth+1 {
			symbols = append(symbols, extractTSEnumMembers(node, bt, name)...)
		}

	case "lexical_declaration", "variable_declaration":
		for i := 0; i < node.ChildCount(); i++ {
			decl := node.Child(i)
			if decl == nil || !decl.IsNamed() {
				continue
			}
			if bt.NodeType(decl) == "variable_declarator" {
				name := childText(decl, bt, "name")
				if name != "" {
					symbols = append(symbols, scopedSymbol(name, "variable", scopePrefix, decl, currentDepth))
				}
				break
			}
		}
	}

	return symbols
}

// extractTSClassMembers extracts method and property definitions from a class node.
func extractTSClassMembers(classNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope, lang string) []ScopedSymbol {
	if classNode == nil {
		return nil
	}

	// Find the class_body.
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
		case "method_definition":
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "method", scope, member, bt, 1, lang))
			}

		case "public_field_definition", "property_signature", "field_definition":
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "property", scope, member, 1))
			}

		case "constructor_declaration":
			symbols = append(symbols, scopedSymbolWithBody("constructor", "method", scope, member, bt, 1, lang))

		case "formal_parameter":
			// Handle "constructor(param: Type)" — the parameter is a direct child of class_body.
			name := childText(member, bt, "name")
			if name != "" && strings.HasPrefix(name, "this.") {
				// Shorthand constructor parameter with "this.x".
				name = name[5:]
				symbols = append(symbols, scopedSymbol(name, "property", scope, member, 1))
			}
		}
	}

	return symbols
}

// extractTSInterfaceMembers extracts property/method signatures from an interface node.
func extractTSInterfaceMembers(ifaceNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope string) []ScopedSymbol {
	if ifaceNode == nil {
		return nil
	}

	// Find the interface_body (or object_type for TS).
	var body *gotreesitter.Node
	for i := 0; i < ifaceNode.ChildCount(); i++ {
		child := ifaceNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		childType := bt.NodeType(child)
		if childType == "interface_body" || childType == "object_type" {
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
		case "method_signature":
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "method", scope, member, 1))
			}

		case "property_signature":
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "property", scope, member, 1))
			}
		}
	}

	return symbols
}

// extractTSEnumMembers extracts member names from an enum_declaration.
//
// The tree-sitter TS grammar produces enum_assignment (not enum_member) with a
// "name" field for the property_identifier.
func extractTSEnumMembers(enumNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope string) []ScopedSymbol {
	if enumNode == nil {
		return nil
	}

	// Find the enum_body.
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
		// The tree-sitter grammar uses "enum_assignment" for enum members.
		mt := bt.NodeType(member)
		if mt != "enum_assignment" && mt != "enum_member" {
			continue
		}
		name := childText(member, bt, "name")
		if name != "" {
			symbols = append(symbols, scopedSymbol(name, "constant", scope, member, 1))
		}
	}

	return symbols
}
