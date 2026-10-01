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
	gotreesitter "github.com/odvcencio/gotreesitter"
)

// parser_symbols_other.go — the remaining per-language symbol extractors
// (Java, Rust, C, C#, Ruby, PHP, Swift, Kotlin, Dart, Lua, Haskell, Bash),
// split out of parser.go.

func extractJavaSymbol(node *gotreesitter.Node, bt *gotreesitter.BoundTree, nodeType, lang string) (Symbol, bool) {
	switch nodeType {
	case "class_declaration":
		name := childText(node, bt, "name")
		return makeSymbol(name, "class", node), name != ""
	case "interface_declaration":
		name := childText(node, bt, "name")
		return makeSymbol(name, "interface", node), name != ""
	case "enum_declaration":
		name := childText(node, bt, "name")
		return makeSymbol(name, "enum", node), name != ""
	case "method_declaration":
		name := childText(node, bt, "name")
		return makeSymbolWithBody(name, "function", node, bt, lang), name != ""
	case "import_declaration":
		return Symbol{}, false
	default:
		return Symbol{}, false
	}
}

// --- Rust symbol extraction (top-level, non-scoped) --------------------------

func extractRustSymbol(node *gotreesitter.Node, bt *gotreesitter.BoundTree, nodeType, lang string) (Symbol, bool) {
	switch nodeType {
	case "function_item":
		name := childText(node, bt, "name")
		return makeSymbolWithBody(name, "function", node, bt, lang), name != ""
	case "struct_item":
		name := childText(node, bt, "name")
		return makeSymbol(name, "class", node), name != ""
	case "trait_item":
		name := childText(node, bt, "name")
		return makeSymbol(name, "interface", node), name != ""
	case "type_item":
		name := childText(node, bt, "name")
		return makeSymbol(name, "type", node), name != ""
	case "const_item":
		name := childText(node, bt, "name")
		return makeSymbol(name, "constant", node), name != ""
	case "enum_item":
		name := childText(node, bt, "name")
		return makeSymbol(name, "enum", node), name != ""
	case "mod_item":
		name := childText(node, bt, "name")
		return makeSymbol(name, "module", node), name != ""
	case "use_declaration":
		return Symbol{}, false
	default:
		return Symbol{}, false
	}
}

// --- C / C++ symbol extraction (top-level, non-scoped) -----------------------

func extractCSymbol(node *gotreesitter.Node, bt *gotreesitter.BoundTree, nodeType, lang string) (Symbol, bool) {
	switch nodeType {
	case "function_definition":
		name := extractCFunctionName(node, bt)
		return makeSymbolWithBody(name, "function", node, bt, lang), name != ""
	case "struct_specifier":
		name := extractCStructName(node, bt)
		return makeSymbol(name, "class", node), name != ""
	case "class_specifier":
		name := extractCStructName(node, bt)
		return makeSymbol(name, "class", node), name != ""
	case "type_definition":
		name := extractCTypedefName(node, bt)
		return makeSymbol(name, "type", node), name != ""
	case "preproc_def":
		name := childText(node, bt, "name")
		return makeSymbol(name, "constant", node), name != ""
	case "preproc_function_def":
		name := childText(node, bt, "name")
		return makeSymbol(name, "function", node), name != ""
	case "preproc_include":
		return Symbol{}, false
	default:
		return Symbol{}, false
	}
}

// --- C# symbol extraction (top-level, non-scoped) ----------------------------

func extractCSharpSymbol(node *gotreesitter.Node, bt *gotreesitter.BoundTree, nodeType, lang string) (Symbol, bool) {
	switch nodeType {
	case "class_declaration":
		name := childText(node, bt, "name")
		return makeSymbol(name, "class", node), name != ""
	case "interface_declaration":
		name := childText(node, bt, "name")
		return makeSymbol(name, "interface", node), name != ""
	case "enum_declaration":
		name := childText(node, bt, "name")
		return makeSymbol(name, "enum", node), name != ""
	case "struct_declaration":
		name := childText(node, bt, "name")
		return makeSymbol(name, "class", node), name != ""
	case "method_declaration":
		name := childText(node, bt, "name")
		return makeSymbolWithBody(name, "function", node, bt, lang), name != ""
	case "using_directive":
		return Symbol{}, false
	default:
		return Symbol{}, false
	}
}

// --- Ruby symbol extraction (top-level, non-scoped) -------------------------

func extractRubySymbol(node *gotreesitter.Node, bt *gotreesitter.BoundTree, nodeType, lang string) (Symbol, bool) {
	switch nodeType {
	case "method", "singleton_method":
		name := childText(node, bt, "name")
		return makeSymbolWithBody(name, "function", node, bt, lang), name != ""
	case "class":
		name := extractRubySymbolClassName(node, bt)
		return makeSymbolWithBody(name, "class", node, bt, lang), name != ""
	case "module":
		name := extractRubySymbolClassName(node, bt)
		return makeSymbolWithBody(name, "class", node, bt, lang), name != ""
	case "sclass":
		// Singleton class — no meaningful name.
		return Symbol{}, false
	default:
		return Symbol{}, false
	}
}

// extractRubySymbolClassName gets the name from a class/module node.
func extractRubySymbolClassName(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	name := childText(node, bt, "name")
	if name != "" {
		return name
	}
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "constant" {
			return bt.NodeText(child)
		}
	}
	return ""
}

// --- PHP symbol extraction (top-level, non-scoped) --------------------------

func extractPHPSymbol(node *gotreesitter.Node, bt *gotreesitter.BoundTree, nodeType, lang string) (Symbol, bool) {
	switch nodeType {
	case "class_declaration":
		name := childText(node, bt, "name")
		return makeSymbol(name, "class", node), name != ""
	case "interface_declaration":
		name := childText(node, bt, "name")
		return makeSymbol(name, "interface", node), name != ""
	case "enum_declaration":
		name := childText(node, bt, "name")
		return makeSymbol(name, "enum", node), name != ""
	case "function_definition":
		name := childText(node, bt, "name")
		return makeSymbolWithBody(name, "function", node, bt, lang), name != ""
	case "php_tag", "namespace_definition":
		return Symbol{}, false
	default:
		return Symbol{}, false
	}
}

// --- Swift symbol extraction (top-level, non-scoped) ------------------------

func extractSwiftSymbol(node *gotreesitter.Node, bt *gotreesitter.BoundTree, nodeType, lang string) (Symbol, bool) {
	switch nodeType {
	case "class_declaration":
		name := extractSwiftSymbolClassName(node, bt)
		return makeSymbol(name, "class", node), name != ""
	case "struct_declaration":
		name := extractSwiftSymbolClassName(node, bt)
		return makeSymbol(name, "class", node), name != ""
	case "protocol_declaration":
		name := extractSwiftSymbolClassName(node, bt)
		return makeSymbol(name, "interface", node), name != ""
	case "enum_declaration":
		name := extractSwiftSymbolClassName(node, bt)
		return makeSymbol(name, "enum", node), name != ""
	case "actor_declaration":
		name := extractSwiftSymbolClassName(node, bt)
		return makeSymbol(name, "class", node), name != ""
	case "function_declaration":
		name := extractSwiftSymbolFunctionName(node, bt)
		return makeSymbolWithBody(name, "function", node, bt, lang), name != ""
	case "import_declaration":
		return Symbol{}, false
	default:
		return Symbol{}, false
	}
}

// extractSwiftSymbolClassName gets the name from a Swift class/struct/protocol node.
func extractSwiftSymbolClassName(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
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

// extractSwiftSymbolFunctionName gets the name from a Swift function declaration.
func extractSwiftSymbolFunctionName(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
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

// --- Kotlin symbol extraction (top-level, non-scoped) ------------------------

func extractKotlinSymbol(node *gotreesitter.Node, bt *gotreesitter.BoundTree, nodeType, lang string) (Symbol, bool) {
	switch nodeType {
	case "class_declaration":
		name := extractKotlinSymbolName(node, bt)
		return makeSymbol(name, "class", node), name != ""
	case "object_declaration":
		name := extractKotlinSymbolName(node, bt)
		return makeSymbol(name, "class", node), name != ""
	case "interface_declaration":
		name := extractKotlinSymbolName(node, bt)
		return makeSymbol(name, "interface", node), name != ""
	case "enum_class_declaration":
		name := extractKotlinSymbolName(node, bt)
		return makeSymbol(name, "enum", node), name != ""
	case "function_declaration":
		name := extractKotlinSymbolFunctionName(node, bt)
		return makeSymbolWithBody(name, "function", node, bt, lang), name != ""
	case "type_alias":
		name := childText(node, bt, "name")
		return makeSymbol(name, "type", node), name != ""
	case "import_list", "package_declaration", "ERROR":
		return Symbol{}, false
	default:
		return Symbol{}, false
	}
}

// extractKotlinSymbolName gets the name from a Kotlin class/object/interface node.
func extractKotlinSymbolName(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
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

// extractKotlinSymbolFunctionName gets the name from a Kotlin function declaration.
func extractKotlinSymbolFunctionName(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	name := childText(node, bt, "name")
	if name != "" {
		return name
	}
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "simple_identifier" || bt.NodeType(child) == "type_identifier" {
			return bt.NodeText(child)
		}
	}
	return ""
}

// --- Dart symbol extraction (top-level, non-scoped) -------------------------

func extractDartSymbol(node *gotreesitter.Node, bt *gotreesitter.BoundTree, nodeType, lang string) (Symbol, bool) {
	switch nodeType {
	case "class_definition":
		name := childText(node, bt, "name")
		return makeSymbol(name, "class", node), name != ""
	case "enum_declaration":
		name := childText(node, bt, "name")
		return makeSymbol(name, "enum", node), name != ""
	case "function_signature":
		name := childText(node, bt, "name")
		return makeSymbolWithBody(name, "function", node, bt, lang), name != ""
	default:
		return Symbol{}, false
	}
}

// --- Lua symbol extraction (top-level, non-scoped) --------------------------

func extractLuaSymbol(node *gotreesitter.Node, bt *gotreesitter.BoundTree, nodeType, lang string) (Symbol, bool) {
	switch nodeType {
	case "function_declaration":
		name := childText(node, bt, "name")
		return makeSymbolWithBody(name, "function", node, bt, lang), name != ""
	case "variable_declaration":
		name := extractLuaVarNameTop(node, bt)
		if name != "" {
			return makeSymbol(name, "variable", node), true
		}
		return Symbol{}, false
	default:
		return Symbol{}, false
	}
}

// extractLuaVarNameTop is the top-level (parser.go) version for Lua variable names.
func extractLuaVarNameTop(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "identifier" {
			return bt.NodeText(child)
		}
		if bt.NodeType(child) == "variable_list" {
			for j := 0; j < child.ChildCount(); j++ {
				v := child.Child(j)
				if v == nil || !v.IsNamed() {
					continue
				}
				if bt.NodeType(v) == "identifier" {
					return bt.NodeText(v)
				}
			}
		}
	}
	return ""
}

// --- Haskell symbol extraction (top-level, non-scoped) -----------------------

func extractHaskellSymbol(node *gotreesitter.Node, bt *gotreesitter.BoundTree, nodeType, lang string) (Symbol, bool) {
	switch nodeType {
	case "data_type":
		name := extractHaskellSymbolName(node, bt)
		return makeSymbol(name, "class", node), name != ""
	case "bind":
		name := extractHaskellBindNameTop(node, bt)
		return makeSymbolWithBody(name, "function", node, bt, lang), name != ""
	case "type_synomym":
		name := childText(node, bt, "name")
		if name == "" {
			for i := 0; i < node.ChildCount(); i++ {
				c := node.Child(i)
				if c == nil || !c.IsNamed() {
					continue
				}
				if bt.NodeType(c) == "name" {
					name = bt.NodeText(c)
					break
				}
			}
		}
		return makeSymbol(name, "type", node), name != ""
	case "declarations", "header":
		// Wrapper nodes — not actual symbols.
		return Symbol{}, false
	default:
		return Symbol{}, false
	}
}

// extractHaskellSymbolName gets the name from a Haskell data_type node.
func extractHaskellSymbolName(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	name := childText(node, bt, "name")
	if name != "" {
		return name
	}
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "name" {
			return bt.NodeText(child)
		}
	}
	return ""
}

// extractHaskellBindNameTop is the top-level (parser.go) version for bind names.
func extractHaskellBindNameTop(bindNode *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	name := childText(bindNode, bt, "name")
	if name != "" {
		return name
	}
	for i := 0; i < bindNode.ChildCount(); i++ {
		child := bindNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "match" {
			for j := 0; j < child.ChildCount(); j++ {
				m := child.Child(j)
				if m == nil || !m.IsNamed() {
					continue
				}
				if bt.NodeType(m) == "name" || bt.NodeType(m) == "identifier" {
					return bt.NodeText(m)
				}
			}
		}
		if bt.NodeType(child) == "name" {
			return bt.NodeText(child)
		}
	}
	return ""
}

// --- Bash symbol extraction (top-level, non-scoped) -------------------------

func extractBashSymbol(node *gotreesitter.Node, bt *gotreesitter.BoundTree, nodeType, lang string) (Symbol, bool) {
	switch nodeType {
	case "function_definition":
		name := childText(node, bt, "name")
		return makeSymbolWithBody(name, "function", node, bt, lang), name != ""
	case "command", "comment":
		return Symbol{}, false
	default:
		return Symbol{}, false
	}
}

// --- Helpers ------------------------------------------------------------------
