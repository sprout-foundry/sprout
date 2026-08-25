package ast

import (
	gotreesitter "github.com/odvcencio/gotreesitter"
)

// ---------------------------------------------------------------------------
// C / C++ scoped symbol extraction
// ---------------------------------------------------------------------------

// extractCSymbols walks the AST for C or C++ and extracts top-level symbols
// plus struct/class fields and inline methods up to maxDepth.
func extractCSymbols(root *gotreesitter.Node, bt *gotreesitter.BoundTree, maxDepth int, lang string) []ScopedSymbol {
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
		case "function_definition":
			name := extractCFunctionName(child, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "function", "", child, bt, 0, lang))
			}

		case "struct_specifier":
			name := extractCStructName(child, bt)
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "class", "", child, 0))

			if maxDepth > 1 {
				symbols = append(symbols, extractCStructFields(child, bt, name)...)
			}

		case "class_specifier":
			// C++ class.
			name := extractCStructName(child, bt)
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbol(name, "class", "", child, 0))

			if maxDepth > 1 {
				symbols = append(symbols, extractCClassMembers(child, bt, name, lang)...)
			}

		case "type_definition":
			name := extractCTypedefName(child, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "type", "", child, 0))
			}

		case "preproc_def":
			// #define constant or macro.
			name := childText(child, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "constant", "", child, 0))
			}

		case "preproc_function_def":
			// #define macro with parameters — treat as function.
			name := childText(child, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "function", "", child, 0))
			}

		default:
		}
	}

	return symbols
}

// extractCFunctionName returns the function name from a function_definition node.
// The name is nested: function_definition → function_declarator → identifier.
func extractCFunctionName(funcDef *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	declarator := bt.ChildByField(funcDef, "declarator")
	if declarator == nil {
		// Fallback: look for function_declarator child.
		for i := 0; i < funcDef.ChildCount(); i++ {
			c := funcDef.Child(i)
			if c == nil || !c.IsNamed() {
				continue
			}
			if bt.NodeType(c) == "function_declarator" {
				declarator = c
				break
			}
		}
	}
	if declarator == nil {
		return ""
	}

	// The identifier is nested: may be a declarator → function_declarator → identifier,
	// or directly a declarator → identifier.
	return extractCDeclaratorName(declarator, bt)
}

// extractCDeclaratorName recursively walks a declarator chain to find the
// underlying identifier. Handles pointer_declarator, function_declarator,
// array_declarator, etc. wrapping around the identifier.
func extractCDeclaratorName(declarator *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	nt := bt.NodeType(declarator)
	switch nt {
	case "identifier":
		return bt.NodeText(declarator)
	case "field_identifier":
		return bt.NodeText(declarator)
	default:
		// For pointer_declarator, function_declarator, array_declarator, etc.
		// the actual name is a child identifier or another declarator.
		for i := 0; i < declarator.ChildCount(); i++ {
			child := declarator.Child(i)
			if child == nil || !child.IsNamed() {
				continue
			}
			childType := bt.NodeType(child)
			switch childType {
			case "identifier", "field_identifier":
				return bt.NodeText(child)
			default:
				// Recurse into nested declarators.
				if name := extractCDeclaratorName(child, bt); name != "" {
					return name
				}
			}
		}
	}
	return ""
}

// extractCStructName returns the name of a struct/class specifier.
// The name is a type_identifier child, NOT a "name" field.
func extractCStructName(specifier *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	// Try "name" field first (some grammars use it).
	name := childText(specifier, bt, "name")
	if name != "" {
		return name
	}

	// Fallback: look for the type_identifier child.
	for i := 0; i < specifier.ChildCount(); i++ {
		child := specifier.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "type_identifier" {
			return bt.NodeText(child)
		}
	}
	return ""
}

// extractCTypedefName returns the typedef alias name.
// In "typedef struct Foo { ... } MyType;", MyType is the LAST type_identifier.
func extractCTypedefName(typeDef *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	// Try "declarator" field first — the typedef name might be in a declarator.
	name := childText(typeDef, bt, "declarator")
	if name != "" {
		return name
	}

	// Fallback: last type_identifier child.
	var lastTypeIdent string
	for i := 0; i < typeDef.ChildCount(); i++ {
		child := typeDef.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "type_identifier" {
			lastTypeIdent = bt.NodeText(child)
		}
	}
	return lastTypeIdent
}

// extractCFieldName returns the field name from a field_declaration node.
// C/C++ field names are nested in declarators, NOT in a "name" field.
func extractCFieldName(fdNode *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	// Try "name" field first (some versions use it).
	name := childText(fdNode, bt, "name")
	if name != "" {
		return name
	}

	// Look for field_identifier child.
	for i := 0; i < fdNode.ChildCount(); i++ {
		child := fdNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		childType := bt.NodeType(child)
		switch childType {
		case "field_identifier", "identifier":
			return bt.NodeText(child)
		case "declarator", "pointer_declarator", "function_declarator",
			"array_declarator", "parenthesized_declarator":
			// Recurse into the declarator to find the identifier.
			if name := extractCDeclaratorName(child, bt); name != "" {
				return name
			}
		}
	}

	return ""
}

// extractCStructFields extracts field declarations from a struct_specifier.
func extractCStructFields(structNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope string) []ScopedSymbol {
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

		if childType == "field_declaration_list" {
			symbols = append(symbols, extractCFieldsFromList(child, bt, scope)...)
		}
	}

	return symbols
}

// extractCClassMembers extracts fields and inline methods from a C++ class_specifier.
// Inline methods (function_definition inside field_declaration_list) are extracted
// as methods with body text.
func extractCClassMembers(classNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope, lang string) []ScopedSymbol {
	if classNode == nil {
		return nil
	}

	// Find the field_declaration_list (or class body).
	var body *gotreesitter.Node
	for i := 0; i < classNode.ChildCount(); i++ {
		child := classNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "field_declaration_list" {
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

		// Skip access specifiers (public:, private:, etc.).
		if memberType == "access_specifier" {
			continue
		}

		switch memberType {
		case "field_declaration":
			name := extractCFieldName(member, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "property", scope, member, 1))
			}

		case "function_definition":
			name := extractCFunctionName(member, bt)
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "method", scope, member, bt, 1, lang))
			}

		case "method_declaration", "constructor_declarator", "destructor_declarator":
			name := childText(member, bt, "name")
			if name == "" {
				name = extractCDeclaratorName(member, bt)
			}
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "method", scope, member, 1))
			}
		}
	}

	return symbols
}

// extractCFieldsFromList walks a field_declaration_list and extracts
// all field names from field_declaration nodes.
func extractCFieldsFromList(listNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope string) []ScopedSymbol {
	if listNode == nil {
		return nil
	}

	var symbols []ScopedSymbol
	for i := 0; i < listNode.ChildCount(); i++ {
		fd := listNode.Child(i)
		if fd == nil || !fd.IsNamed() {
			continue
		}
		if bt.NodeType(fd) != "field_declaration" {
			continue
		}
		name := extractCFieldName(fd, bt)
		if name != "" {
			symbols = append(symbols, scopedSymbol(name, "property", scope, fd, 1))
		}
	}

	return symbols
}
