package ast

import (
	gotreesitter "github.com/odvcencio/gotreesitter"
)

// ---------------------------------------------------------------------------
// Go scoped symbol extraction
// ---------------------------------------------------------------------------

// extractGoSymbols walks the AST for Go source and extracts top-level symbols
// plus nested members (struct fields, interface methods) up to maxDepth.
//
// maxDepth semantics:
//
//	1 — extract only top-level symbols (depth 0)
//	2+ — also extract nested members (depth 1)
func extractGoSymbols(root *gotreesitter.Node, bt *gotreesitter.BoundTree, maxDepth int, lang string) []ScopedSymbol {
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

		// Always extract top-level symbols when maxDepth ≥ 1.
		switch nodeType {
		case "function_declaration":
			name := childText(child, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "function", "", child, bt, 0, lang))
			}

		case "method_declaration":
			name := childText(child, bt, "name")
			if name != "" {
				// Methods are top-level AST nodes (direct children of root) but
				// logically nested under the receiver type.  Extract the receiver
				// type name for scope.  Since they are direct children of root,
				// they are extracted at maxDepth >= 1.
				scope := extractGoReceiverName(child, bt)
				if scope == "" {
					continue // Skip methods with unresolvable receiver to maintain scope invariant.
				}
				symbols = append(symbols, scopedSymbolWithBody(name, "method", scope, child, bt, 1, lang))
			}

		case "type_declaration":
			// Walk children: type_spec or type_alias.
			for j := 0; j < child.ChildCount(); j++ {
				tc := child.Child(j)
				if tc == nil || !tc.IsNamed() {
					continue
				}
				tcType := bt.NodeType(tc)

				switch tcType {
				case "type_alias":
					name := childText(tc, bt, "name")
					if name != "" {
						symbols = append(symbols, scopedSymbol(name, "type", "", tc, 0))
					}

				case "type_spec":
					name := childText(tc, bt, "name")
					if name == "" {
						continue
					}

					// Determine kind from the type child.
					kind := "type"
					typeChild := bt.ChildByField(tc, "type")
					var innerTypeType string
					if typeChild != nil {
						innerTypeType = bt.NodeType(typeChild)
						switch innerTypeType {
						case "struct_type":
							kind = "class"
						case "interface_type":
							kind = "interface"
						case "type_identifier", "pointer_type", "generic_type":
							kind = "type"
						}
					}

					symbols = append(symbols, scopedSymbol(name, kind, "", tc, 0))

					// Extract nested members if we have depth budget.
					if maxDepth > 1 {
						scope := name // scope is the type name.
						switch innerTypeType {
						case "struct_type":
							symbols = append(symbols, extractGoStructFields(typeChild, bt, scope)...)
						case "interface_type":
							symbols = append(symbols, extractGoInterfaceMethods(typeChild, bt, scope)...)
						}
					}
				}
			}

		case "var_declaration", "const_declaration":
			// Package-level variable/const blocks.
			for j := 0; j < child.ChildCount(); j++ {
				spec := child.Child(j)
				if spec == nil || !spec.IsNamed() {
					continue
				}
				if bt.NodeType(spec) != "var_spec" && bt.NodeType(spec) != "const_spec" {
					continue
				}
				name := childText(spec, bt, "name")
				if name != "" {
					kind := "variable"
					if bt.NodeType(child) == "const_declaration" {
						kind = "constant"
					}
					symbols = append(symbols, scopedSymbol(name, kind, "", spec, 0))
				}
			}
		}
	}

	return symbols
}

// extractGoReceiverName returns the type name of the method receiver, e.g.
// "Foo" from "(f *Foo)" or "(*Foo)".
func extractGoReceiverName(methodNode *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	receiver := bt.ChildByField(methodNode, "receiver")
	if receiver == nil {
		return ""
	}
	// receiver is a parameter_list containing parameter.
	for i := 0; i < receiver.ChildCount(); i++ {
		param := receiver.Child(i)
		if param == nil || !param.IsNamed() {
			continue
		}
		if bt.NodeType(param) != "parameter_declaration" {
			continue
		}
		// The type field of the parameter gives us the receiver type.
		typeNode := bt.ChildByField(param, "type")
		if typeNode == nil {
			continue
		}
		typeType := bt.NodeType(typeNode)
		switch typeType {
		case "type_identifier":
			return bt.NodeText(typeNode)
		case "pointer_type":
			// Dereference: pointer_type contains a child with the actual type.
			for j := 0; j < typeNode.ChildCount(); j++ {
				c := typeNode.Child(j)
				if c == nil || !c.IsNamed() {
					continue
				}
				if bt.NodeType(c) == "type_identifier" {
					return bt.NodeText(c)
				}
			}
		}
	}
	return ""
}

// extractGoStructFields extracts field declarations from a struct_type node.
//
// The tree-sitter Go grammar produces:
//
//	struct_type → struct [anon] field_declaration_list
//	field_declaration_list → { [anon] field_declaration ... }
//	field_declaration → field_identifier type (or just type for embedded)
func extractGoStructFields(structNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope string) []ScopedSymbol {
	if structNode == nil {
		return nil
	}

	var symbols []ScopedSymbol
	for i := 0; i < structNode.ChildCount(); i++ {
		child := structNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		childType := bt.NodeType(child)

		switch childType {
		case "field_declaration_list":
			// Walk field_declarations inside field_declaration_list.
			for j := 0; j < child.ChildCount(); j++ {
				fd := child.Child(j)
				if fd == nil || !fd.IsNamed() {
					continue
				}
				if bt.NodeType(fd) != "field_declaration" {
					continue
				}
				name := extractGoFieldDeclarationName(fd, bt)
				if name == "" {
					continue
				}
				symbols = append(symbols, scopedSymbol(name, "property", scope, fd, 1))
			}

		case "field_declaration":
			// Single-field struct (no field_declaration_list wrapper).
			name := extractGoFieldDeclarationName(child, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "property", scope, child, 1))
			}
		}
	}
	return symbols
}

// extractGoFieldDeclarationName returns the field name from a field_declaration
// node. For embedded fields like "BaseStruct", there is no field_identifier —
// we use the type name instead.
func extractGoFieldDeclarationName(fdNode *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	// Try the "name" field first (works for named fields like "Field string").
	name := childText(fdNode, bt, "name")
	if name != "" {
		return name
	}
	// Embedded field: the type IS the name (e.g. "BaseStruct" in struct { BaseStruct }).
	typeNode := bt.ChildByField(fdNode, "type")
	if typeNode != nil && bt.NodeType(typeNode) == "type_identifier" {
		return bt.NodeText(typeNode)
	}
	return ""
}

// extractGoInterfaceMethods extracts method signatures from an interface_type node.
//
// The tree-sitter Go grammar produces:
//
//	interface_type → interface [anon] { [anon] method_elem ... }
//	method_elem → field_identifier parameter_list type
func extractGoInterfaceMethods(ifaceNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope string) []ScopedSymbol {
	if ifaceNode == nil {
		return nil
	}

	var symbols []ScopedSymbol
	for i := 0; i < ifaceNode.ChildCount(); i++ {
		child := ifaceNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		childType := bt.NodeType(child)

		// method_elem can appear directly inside interface_type (no wrapper node).
		if childType == "method_elem" {
			name := childText(child, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "method", scope, child, 1))
			}
		}
	}
	return symbols
}
