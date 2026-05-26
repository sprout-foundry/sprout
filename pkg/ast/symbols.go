// Package ast (continued) — scoped symbol extraction.
//
// This package provides ExtractSymbols, which walks the AST from root and
// extracts symbols with scope information up to a configurable nesting depth.
// Unlike the inline extractSymbols in parser.go (which only walks direct
// children of root), this implementation tracks parent scope names so that
// methods inside classes get Scope: "MyClass" and Depth: 1.
//
// The existing Symbol and helper functions (makeSymbol, childText) in parser.go
// are reused; no modifications to parser.go are required.
package ast

import (
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// DefaultMaxDepth is the default maximum nesting depth for ExtractSymbols.
// Depth 0 = top-level symbols, Depth 1 = children (methods, fields, etc.).
const DefaultMaxDepth = 2

// ScopedSymbol extends Symbol with scope/parent information and nesting depth.
type ScopedSymbol struct {
	Symbol // embed the existing Symbol for Name, Kind, StartLine, etc.

	// Scope is the parent scope path, e.g. "MyClass" for a method inside
	// a class, or "MyClass.NestedStruct" for deeper nesting.
	// Empty string for top-level symbols (Depth == 0).
	Scope string

	// Depth is the nesting level: 0 = top-level, 1 = child of top-level, etc.
	Depth int
}

// ExtractSymbols walks the AST from root and extracts symbols with scope
// information. It goes deeper than just top-level: it finds methods inside
// classes (depth 1), nested functions, class methods in Python, etc.
//
// The walk is limited to DefaultMaxDepth levels of nesting.
func ExtractSymbols(root *gotreesitter.Node, bt *gotreesitter.BoundTree, lang string) []ScopedSymbol {
	return ExtractSymbolsWithMaxDepth(root, bt, lang, DefaultMaxDepth)
}

// ExtractSymbolsWithMaxDepth is like ExtractSymbols but allows specifying
// the maximum nesting depth. A value of 1 extracts only top-level symbols;
// a value of 2 extracts top-level plus one level of nesting. Values above 2
// are currently not used for additional nesting levels.
func ExtractSymbolsWithMaxDepth(root *gotreesitter.Node, bt *gotreesitter.BoundTree, lang string, maxDepth int) []ScopedSymbol {
	if root == nil || bt == nil {
		return nil
	}

	// maxDepth ≤ 0 means no extraction at all.
	if maxDepth <= 0 {
		return nil
	}

	lang = strings.ToLower(lang)

	var symbols []ScopedSymbol

	switch lang {
	case "go":
		symbols = extractGoSymbols(root, bt, maxDepth, lang)
	case "typescript", "tsx":
		symbols = extractTSSymbols(root, bt, maxDepth, lang)
	case "javascript":
		symbols = extractTSSymbols(root, bt, maxDepth, lang) // JS shares TS node types
	case "python":
		symbols = extractPythonSymbols(root, bt, maxDepth, lang)
	default:
		// Fallback: extract top-level symbols only via the generic walker.
		symbols = extractGenericSymbols(root, bt, maxDepth, lang)
	}

	return symbols
}

// scopedSymbol creates a ScopedSymbol with the given scope and depth.
func scopedSymbol(name, kind, scope string, node *gotreesitter.Node, depth int) ScopedSymbol {
	return ScopedSymbol{
		Symbol: makeSymbol(name, kind, node),
		Scope:  scope,
		Depth:  depth,
	}
}

// scopedSymbolWithBody is like scopedSymbol but also extracts the body text
// for function/method/class symbols via extractBody. Use this for nodes where
// body extraction is meaningful (functions, methods, Python classes).
func scopedSymbolWithBody(name, kind, scope string, node *gotreesitter.Node, bt *gotreesitter.BoundTree, depth int, lang string) ScopedSymbol {
	s := scopedSymbol(name, kind, scope, node, depth)
	s.Body = extractBody(node, bt, lang)
	return s
}

// shouldSkipNode returns true for nodes that should never be extracted as
// symbols (e.g. imports, error nodes, comments).
func shouldSkipNode(nodeType string) bool {
	switch nodeType {
	case "import_declaration", "import_statement", "import_from_statement",
		"package_clause", "expression_statement",
		"ERROR", "comment", "shebang":
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Go scoped symbol extraction
// ---------------------------------------------------------------------------

// extractGoSymbols walks the AST for Go source and extracts top-level symbols
// plus nested members (struct fields, interface methods) up to maxDepth.
//
// maxDepth semantics:
//   1 — extract only top-level symbols (depth 0)
//   2+ — also extract nested members (depth 1)
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
//   struct_type → struct [anon] field_declaration_list
//   field_declaration_list → { [anon] field_declaration ... }
//   field_declaration → field_identifier type (or just type for embedded)
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
//   interface_type → interface [anon] { [anon] method_elem ... }
//   method_elem → field_identifier parameter_list type
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

// ---------------------------------------------------------------------------
// Python scoped symbol extraction
// ---------------------------------------------------------------------------

// extractPythonSymbols walks the AST for Python and extracts top-level symbols
// plus class methods up to maxDepth.
func extractPythonSymbols(root *gotreesitter.Node, bt *gotreesitter.BoundTree, maxDepth int, lang string) []ScopedSymbol {
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
		case "function_definition", "async_function_definition":
			name := childText(child, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "function", "", child, bt, 0, lang))
			}

		case "class_definition":
			name := childText(child, bt, "name")
			if name == "" {
				continue
			}
			symbols = append(symbols, scopedSymbolWithBody(name, "class", "", child, bt, 0, lang))

			// Extract class body members if we have depth budget.
			if maxDepth > 1 {
				symbols = append(symbols, extractPythonClassMembers(child, bt, name, lang)...)
			}

		case "type_alias":
			name := childText(child, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "type", "", child, 0))
			}

		case "decorated_definition":
			// Unwrap decorator and extract the underlying definition.
			outerStartLine := int(child.StartPoint().Row) + 1
			outerStartByte := int(child.StartByte())

			for j := 0; j < child.ChildCount(); j++ {
				dc := child.Child(j)
				if dc == nil || !dc.IsNamed() {
					continue
				}
				dcType := bt.NodeType(dc)
				if dcType == "decorator" {
					continue
				}
				// Recurse into the decorated definition.
				decorated := extractPythonDecoratedSymbol(dc, bt, dcType, "", maxDepth, lang)
				for k := range decorated {
					// Override start to include the decorator.
					decorated[k].StartLine = outerStartLine
					decorated[k].StartByte = outerStartByte
				}
				symbols = append(symbols, decorated...)
			}

		case "import_statement", "import_from_statement":
			// Skip imports.

		default:
			// Skip unrecognised top-level nodes.
		}
	}

	return symbols
}

// extractPythonDecoratedSymbol handles a decorated definition (function or class)
// and returns scoped symbols including nested members.
func extractPythonDecoratedSymbol(node *gotreesitter.Node, bt *gotreesitter.BoundTree, nodeType, scopePrefix string, maxDepth int, lang string) []ScopedSymbol {
	switch nodeType {
	case "function_definition", "async_function_definition":
		name := childText(node, bt, "name")
		if name == "" {
			return nil
		}
		return []ScopedSymbol{scopedSymbolWithBody(name, "function", scopePrefix, node, bt, 0, lang)}

	case "class_definition":
		name := childText(node, bt, "name")
		if name == "" {
			return nil
		}
		symbols := []ScopedSymbol{scopedSymbolWithBody(name, "class", scopePrefix, node, bt, 0, lang)}
		if maxDepth > 1 {
			symbols = append(symbols, extractPythonClassMembers(node, bt, name, lang)...)
		}
		return symbols
	}
	return nil
}

// extractPythonClassMembers extracts method and attribute definitions from a class node.
//
// The tree-sitter Python grammar produces for "total: int = 0":
//   block → assignment → identifier : type = integer
// We check for assignment nodes whose first named child is a simple identifier.
func extractPythonClassMembers(classNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope, lang string) []ScopedSymbol {
	if classNode == nil {
		return nil
	}

	// Find the class body (block).
	var body *gotreesitter.Node
	for i := 0; i < classNode.ChildCount(); i++ {
		child := classNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "block" {
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
		case "function_definition":
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbolWithBody(name, "method", scope, member, bt, 1, lang))
			}

		case "decorated_definition":
			// Method with decorator (e.g. @staticmethod, @property).
			for j := 0; j < member.ChildCount(); j++ {
				mc := member.Child(j)
				if mc == nil || !mc.IsNamed() {
					continue
				}
				mt := bt.NodeType(mc)
				if mt == "decorator" {
					continue
				}
				if mt == "function_definition" {
					name := childText(mc, bt, "name")
					if name != "" {
						symbols = append(symbols, scopedSymbolWithBody(name, "method", scope, mc, bt, 1, lang))
					}
					break
				}
			}

		case "expression_statement":
			// Could be attribute assignment like "x: int" or "x = 0".
			for j := 0; j < member.ChildCount(); j++ {
				expr := member.Child(j)
				if expr == nil || !expr.IsNamed() {
					continue
				}
				exprType := bt.NodeType(expr)
				switch exprType {
				case "annotated_assignment":
					name := childText(expr, bt, "left")
					if name != "" {
						symbols = append(symbols, scopedSymbol(name, "property", scope, expr, 1))
					}
				case "assignment":
					// The "left" field may not be set by tree-sitter for Python
					// assignment.  Try to get the identifier from the first child.
					name := childText(expr, bt, "left")
					if name == "" {
						// Fallback: first named child is the identifier.
						for k := 0; k < expr.ChildCount(); k++ {
							c := expr.Child(k)
							if c == nil || !c.IsNamed() {
								continue
							}
							if bt.NodeType(c) == "identifier" {
								name = bt.NodeText(c)
								break
							}
							break
						}
					}
					if name != "" {
						symbols = append(symbols, scopedSymbol(name, "property", scope, expr, 1))
					}
				}
				break // Only first expression in the statement.
			}

		case "assignment":
			// Direct assignment in block (e.g. "total: int = 0").
			name := childText(member, bt, "left")
			if name == "" {
				// Fallback: first named child is the identifier.
				for j := 0; j < member.ChildCount(); j++ {
					c := member.Child(j)
					if c == nil || !c.IsNamed() {
						continue
					}
					if bt.NodeType(c) == "identifier" {
						name = bt.NodeText(c)
						break
					}
					break
				}
			}
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "property", scope, member, 1))
			}

		case "type_alias":
			// Python 3.12+ type alias: "TypeAlias = ..."
			name := childText(member, bt, "name")
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "type", scope, member, 1))
			}
		}
	}

	return symbols
}

// ---------------------------------------------------------------------------
// Generic fallback
// ---------------------------------------------------------------------------

// extractGenericSymbols extracts top-level symbols for languages without
// a dedicated extractor. It walks direct children of root and attempts
// to classify them by common node types.
func extractGenericSymbols(root *gotreesitter.Node, bt *gotreesitter.BoundTree, maxDepth int, lang string) []ScopedSymbol {
	if root == nil || bt == nil {
		return nil
	}
	if maxDepth <= 0 {
		return nil
	}

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

		// Try to extract a name from the node.
		name := childText(child, bt, "name")
		if name == "" {
			continue
		}

		kind := guessKind(nodeType)
		switch kind {
		case "function", "method":
			symbols = append(symbols, scopedSymbolWithBody(name, kind, "", child, bt, 0, lang))
		default:
			symbols = append(symbols, scopedSymbol(name, kind, "", child, 0))
		}
	}

	return symbols
}

// guessKind maps a node type string to a best-effort symbol kind.
func guessKind(nodeType string) string {
	switch {
	case strings.Contains(nodeType, "function") || strings.Contains(nodeType, "func"):
		return "function"
	case strings.Contains(nodeType, "class"):
		return "class"
	case strings.Contains(nodeType, "interface"):
		return "interface"
	case strings.Contains(nodeType, "type"):
		return "type"
	case strings.Contains(nodeType, "enum"):
		return "enum"
	case strings.Contains(nodeType, "variable") || strings.Contains(nodeType, "var") ||
		strings.Contains(nodeType, "assignment"):
		return "variable"
	case strings.Contains(nodeType, "constant") || strings.Contains(nodeType, "const"):
		return "constant"
	case strings.Contains(nodeType, "method"):
		return "method"
	case strings.Contains(nodeType, "property") || strings.Contains(nodeType, "field"):
		return "property"
	default:
		return "symbol"
	}
}
