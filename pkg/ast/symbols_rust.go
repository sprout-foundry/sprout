package ast

import (
	gotreesitter "github.com/odvcencio/gotreesitter"
)

// ---------------------------------------------------------------------------
// Rust scoped symbol extraction
// ---------------------------------------------------------------------------

// extractRustSymbols walks the AST for Rust and extracts top-level symbols
// plus struct fields, trait methods, and impl methods up to maxDepth.
func extractRustSymbols(root *gotreesitter.Node, bt *gotreesitter.BoundTree, maxDepth int, lang string) []ScopedSymbol {
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
		case "function_item":
			name := childText(child, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "function", "", child, bt, 0, lang))
			}

		case "struct_item":
			name := childText(child, bt, "name")
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "class", "", child, 0))

			if maxDepth > 1 {
				symbols = append(symbols, extractRustStructFields(child, bt, name)...)
			}

		case "trait_item":
			name := childText(child, bt, "name")
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "interface", "", child, 0))

			if maxDepth > 1 {
				symbols = append(symbols, extractRustTraitMethods(child, bt, name)...)
			}

		case "impl_item":
			// impl blocks: extract the type name for scoping methods.
			scope := extractRustImplTypeName(child, bt)
			if scope == "" {
				continue
			}
			// Don't emit impl_item itself as a top-level symbol;
			// its methods are scoped under the type name.
			if maxDepth > 1 {
				symbols = append(symbols, extractRustImplMethods(child, bt, scope, lang)...)
			}

		case "type_item":
			name := childText(child, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "type", "", child, 0))
			}

		case "const_item":
			name := childText(child, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "constant", "", child, 0))
			}

		case "enum_item":
			name := childText(child, bt, "name")
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "enum", "", child, 0))

			if maxDepth > 1 {
				symbols = append(symbols, extractRustEnumVariants(child, bt, name)...)
			}

		case "mod_item":
			// Module declaration.
			name := childText(child, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "module", "", child, 0))
			}

		default:
		}
	}

	return symbols
}

// extractRustImplTypeName returns the type name that an impl_item is for.
// The type is the first named child of the impl_item that is a type_identifier.
func extractRustImplTypeName(implNode *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	for i := 0; i < implNode.ChildCount(); i++ {
		child := implNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "type_identifier" {
			return bt.NodeText(child)
		}
	}
	return ""
}

// extractRustImplMethods extracts function_item children from the declaration_list
// of an impl block, scoped to the impl type name.
func extractRustImplMethods(implNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope, lang string) []ScopedSymbol {
	if implNode == nil {
		return nil
	}

	var declList *gotreesitter.Node
	for i := 0; i < implNode.ChildCount(); i++ {
		child := implNode.Child(i)
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
		case "function_item":
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "method", scope, member, bt, 1, lang))
			}

		case "struct_item":
			// Nested struct inside impl (rare, but possible in macros).
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "class", scope, member, 1))
			}

		case "type_item":
			// Associated type alias.
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "type", scope, member, 1))
			}

		case "const_item":
			// Associated const.
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "constant", scope, member, 1))
			}
		}
	}

	return symbols
}

// extractRustStructFields extracts field declarations from a struct_item.
// The fields are inside a field_declaration_list child.
func extractRustStructFields(structNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope string) []ScopedSymbol {
	if structNode == nil {
		return nil
	}

	var fieldList *gotreesitter.Node
	for i := 0; i < structNode.ChildCount(); i++ {
		child := structNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "field_declaration_list" {
			fieldList = child
			break
		}
	}
	if fieldList == nil {
		return nil
	}

	var symbols []ScopedSymbol
	for i := 0; i < fieldList.ChildCount(); i++ {
		fd := fieldList.Child(i)
		if fd == nil || !fd.IsNamed() {
			continue
		}
		if bt.NodeType(fd) != "field_declaration" {
			continue
		}
		name := childText(fd, bt, "name")
		if name != "" {
			symbols = append(symbols, scopedSymbol(name, "property", scope, fd, 1))
		}
	}

	return symbols
}

// extractRustTraitMethods extracts function signatures from a trait_item's
// declaration_list.
func extractRustTraitMethods(traitNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope string) []ScopedSymbol {
	if traitNode == nil {
		return nil
	}

	var declList *gotreesitter.Node
	for i := 0; i < traitNode.ChildCount(); i++ {
		child := traitNode.Child(i)
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
		case "function_signature_item":
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "method", scope, member, 1))
			}

		case "type_item":
			// Associated type in trait.
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "type", scope, member, 1))
			}

		case "const_item":
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "constant", scope, member, 1))
			}
		}
	}

	return symbols
}

// extractRustEnumVariants extracts variant names from an enum_item.
//
// The tree-sitter Rust grammar produces:
//
//	enum_item → enum name enum_variant_list
//	enum_variant_list → { enum_variant ... }
//	enum_variant → name ( [enum_fields])
func extractRustEnumVariants(enumNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope string) []ScopedSymbol {
	if enumNode == nil {
		return nil
	}

	var variantList *gotreesitter.Node
	for i := 0; i < enumNode.ChildCount(); i++ {
		child := enumNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "enum_variant_list" {
			variantList = child
			break
		}
	}
	if variantList == nil {
		return nil
	}

	var symbols []ScopedSymbol
	for i := 0; i < variantList.ChildCount(); i++ {
		v := variantList.Child(i)
		if v == nil || !v.IsNamed() {
			continue
		}
		if bt.NodeType(v) != "enum_variant" {
			continue
		}
		name := childText(v, bt, "name")
		if name != "" {
			symbols = append(symbols, scopedSymbol(name, "property", scope, v, 1))
		}
	}

	return symbols
}
