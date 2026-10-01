// Package ast provides a unified AST parser using gotreesitter (pure Go
// tree-sitter) for Go, TypeScript, JavaScript, and Python source files.
//
// The parser pre-warms grammar blobs at init time (unless
// SPROUT_SKIP_GRAMMAR_PREWARM=1) so that the first call to ParseFile does
// not pay the grammar-loading cost.  It is safe for concurrent use: each
// call to ParseFile creates its own parser instance.
//
// Usage:
//
//	result, err := ast.ParseFile("main.go", content)
//	if err != nil { ... }
//	for _, sym := range result.Symbols {
//	    fmt.Printf("%s %s at line %d\n", sym.Kind, sym.Name, sym.StartLine)
//	}
package ast

import (
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// parser_symbols.go — generic symbol dispatch and the Go / TypeScript /
// JavaScript / Python per-language extractors, split out of parser.go.

// extractSymbols walks the top-level children of the root node and extracts
// symbol declarations.  Language-specific node-type mappings handle Go,
// TypeScript, JavaScript, and Python.
func extractSymbols(root *gotreesitter.Node, bt *gotreesitter.BoundTree, lang string) []Symbol {
	var symbols []Symbol
	lang = strings.ToLower(lang)

	// Walk only direct children of root for top-level symbols.
	for i := 0; i < root.ChildCount(); i++ {
		child := root.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		nodeType := bt.NodeType(child)

		sym, ok := extractSymbol(child, bt, nodeType, lang)
		if !ok {
			continue
		}
		symbols = append(symbols, sym)
	}

	return symbols
}

// extractSymbol maps a node type to a Symbol based on language-specific rules.
func extractSymbol(node *gotreesitter.Node, bt *gotreesitter.BoundTree, nodeType, lang string) (Symbol, bool) {
	switch lang {
	case "go":
		return extractGoSymbol(node, bt, nodeType, lang)
	case "typescript", "tsx":
		return extractTSSymbol(node, bt, nodeType, lang)
	case "javascript":
		return extractTSSymbol(node, bt, nodeType, lang) // JS shares TS node types
	case "python":
		return extractPythonSymbol(node, bt, nodeType, lang)
	case "java":
		return extractJavaSymbol(node, bt, nodeType, lang)
	case "rust":
		return extractRustSymbol(node, bt, nodeType, lang)
	case "c", "cpp":
		return extractCSymbol(node, bt, nodeType, lang)
	case "c_sharp":
		return extractCSharpSymbol(node, bt, nodeType, lang)
	case "ruby":
		return extractRubySymbol(node, bt, nodeType, lang)
	case "php":
		return extractPHPSymbol(node, bt, nodeType, lang)
	case "swift":
		return extractSwiftSymbol(node, bt, nodeType, lang)
	case "kotlin":
		return extractKotlinSymbol(node, bt, nodeType, lang)
	case "dart":
		return extractDartSymbol(node, bt, nodeType, lang)
	case "lua":
		return extractLuaSymbol(node, bt, nodeType, lang)
	case "haskell":
		return extractHaskellSymbol(node, bt, nodeType, lang)
	case "bash":
		return extractBashSymbol(node, bt, nodeType, lang)
	default:
		// Generic C-family fallback: handles Kotlin, Swift, etc.
		return extractGenericSymbol(node, bt, nodeType, lang)
	}
}

// --- Generic C-family symbol extraction (Kotlin, Swift, Java, C#, Rust, etc.) ---

// genericChildName finds the name of a declaration node for C-family languages.
// Kotlin uses type_identifier, Swift uses simple_identifier, etc.
func genericChildName(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	// Try standard names first
	name := childText(node, bt, "name")
	if name != "" {
		return name
	}
	// Fallback: look for type_identifier or simple_identifier children
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		nt := bt.NodeType(child)
		if nt == "type_identifier" || nt == "simple_identifier" {
			return bt.NodeText(child)
		}
	}
	return ""
}

func extractGenericSymbol(node *gotreesitter.Node, bt *gotreesitter.BoundTree, nodeType, lang string) (Symbol, bool) {
	switch nodeType {
	case "function_declaration", "function_definition", "method_declaration", "method_definition":
		name := genericChildName(node, bt)
		return makeSymbolWithBody(name, "function", node, bt, lang), name != ""
	case "class_declaration", "class_definition":
		name := genericChildName(node, bt)
		return makeSymbolWithBody(name, "class", node, bt, lang), name != ""
	case "object_declaration": // Kotlin object (singleton)
		name := genericChildName(node, bt)
		return makeSymbolWithBody(name, "object", node, bt, lang), name != ""
	case "interface_declaration", "interface_definition":
		name := genericChildName(node, bt)
		return makeSymbolWithBody(name, "interface", node, bt, lang), name != ""
	case "enum_declaration", "enum_definition":
		name := genericChildName(node, bt)
		return makeSymbolWithBody(name, "enum", node, bt, lang), name != ""
	case "property_declaration", "variable_declaration":
		name := genericChildName(node, bt)
		if name != "" {
			return makeSymbolWithBody(name, "property", node, bt, lang), true
		}
		return Symbol{}, false
	case "type_alias", "type_declaration":
		name := genericChildName(node, bt)
		return makeSymbolWithBody(name, "type", node, bt, lang), name != ""
	default:
		return Symbol{}, false
	}
}

// --- Go symbol extraction ----------------------------------------------------

func extractGoSymbol(node *gotreesitter.Node, bt *gotreesitter.BoundTree, nodeType, lang string) (Symbol, bool) {
	switch nodeType {
	case "function_declaration":
		name := childText(node, bt, "name")
		return makeSymbolWithBody(name, "function", node, bt, lang), name != ""

	case "method_declaration":
		name := childText(node, bt, "name")
		return makeSymbolWithBody(name, "method", node, bt, lang), name != ""

	case "type_declaration":
		// type_declaration can contain type_spec (struct/interface/alias)
		// or type_alias (type Alias = string).
		for i := 0; i < node.ChildCount(); i++ {
			child := node.Child(i)
			childType := bt.NodeType(child)
			switch childType {
			case "type_spec":
				name := childText(child, bt, "name")
				if name == "" {
					continue
				}
				kind := "type"
				typeChild := bt.ChildByField(child, "type")
				if typeChild != nil {
					t := bt.NodeType(typeChild)
					switch t {
					case "struct_type":
						kind = "class"
					case "interface_type":
						kind = "interface"
					}
				}
				return makeSymbol(name, kind, child), true
			case "type_alias":
				name := childText(child, bt, "name")
				if name == "" {
					continue
				}
				return makeSymbol(name, "type", child), true
			}
		}
		return Symbol{}, false

	case "import_declaration":
		return Symbol{}, false // skip imports

	default:
		return Symbol{}, false
	}
}

// --- TypeScript / JavaScript symbol extraction --------------------------------

func extractTSSymbol(node *gotreesitter.Node, bt *gotreesitter.BoundTree, nodeType, lang string) (Symbol, bool) {
	switch nodeType {
	case "function_declaration":
		name := childText(node, bt, "name")
		return makeSymbolWithBody(name, "function", node, bt, lang), name != ""

	case "function":
		// Arrow / function expressions assigned to variables.
		name := childText(node, bt, "name")
		return makeSymbolWithBody(name, "function", node, bt, lang), name != ""

	case "class_declaration":
		name := childText(node, bt, "name")
		return makeSymbol(name, "class", node), name != ""

	case "interface_declaration":
		name := childText(node, bt, "name")
		return makeSymbol(name, "interface", node), name != ""

	case "type_alias_declaration":
		name := childText(node, bt, "name")
		return makeSymbol(name, "type", node), name != ""

	case "enum_declaration":
		name := childText(node, bt, "name")
		return makeSymbol(name, "enum", node), name != ""

	case "lexical_declaration":
		// const/let — extract the first declarator name.
		for i := 0; i < node.ChildCount(); i++ {
			child := node.Child(i)
			if bt.NodeType(child) == "variable_declarator" {
				name := childText(child, bt, "name")
				return makeSymbol(name, "variable", child), name != ""
			}
		}
		return Symbol{}, false

	case "variable_declaration":
		// var declarations.
		for i := 0; i < node.ChildCount(); i++ {
			child := node.Child(i)
			if bt.NodeType(child) == "variable_declarator" {
				name := childText(child, bt, "name")
				return makeSymbol(name, "variable", child), name != ""
			}
		}
		return Symbol{}, false

	case "export_statement":
		// Unwrap export and recurse into the exported declaration.
		for i := 0; i < node.ChildCount(); i++ {
			child := node.Child(i)
			if child == nil || !child.IsNamed() {
				continue
			}
			ctype := bt.NodeType(child)
			if sym, ok := extractTSSymbol(child, bt, ctype, lang); ok {
				return sym, true
			}
		}
		return Symbol{}, false

	case "ambient_declaration":
		for i := 0; i < node.ChildCount(); i++ {
			child := node.Child(i)
			if child == nil || !child.IsNamed() {
				continue
			}
			ctype := bt.NodeType(child)
			if sym, ok := extractTSSymbol(child, bt, ctype, lang); ok {
				return sym, true
			}
		}
		return Symbol{}, false

	case "method_definition":
		name := childText(node, bt, "name")
		return makeSymbolWithBody(name, "method", node, bt, lang), name != ""

	case "public_field_definition", "property_signature":
		name := childText(node, bt, "name")
		return makeSymbol(name, "property", node), name != ""

	default:
		return Symbol{}, false
	}
}

// --- Python symbol extraction -------------------------------------------------

func extractPythonSymbol(node *gotreesitter.Node, bt *gotreesitter.BoundTree, nodeType, lang string) (Symbol, bool) {
	switch nodeType {
	case "function_definition", "async_function_definition":
		name := childText(node, bt, "name")
		return makeSymbolWithBody(name, "function", node, bt, lang), name != ""

	case "class_definition":
		name := childText(node, bt, "name")
		return makeSymbolWithBody(name, "class", node, bt, lang), name != ""

	case "decorated_definition":
		// Unwrap decorator and extract the underlying definition.
		for i := 0; i < node.ChildCount(); i++ {
			child := node.Child(i)
			if child == nil || !child.IsNamed() {
				continue
			}
			ctype := bt.NodeType(child)
			if ctype == "decorator" {
				continue
			}
			if sym, ok := extractPythonSymbol(child, bt, ctype, lang); ok {
				// Override start to include the decorator, but keep the inner
				// node's end — giving a span that covers decorator + definition.
				sym.StartLine = int(node.StartPoint().Row) + 1
				sym.StartByte = int(node.StartByte())
				return sym, true
			}
		}
		return Symbol{}, false

	case "import_statement", "import_from_statement":
		return Symbol{}, false

	default:
		return Symbol{}, false
	}
}

// --- Java symbol extraction (top-level, non-scoped) --------------------------
