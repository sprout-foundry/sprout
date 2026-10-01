// Package ast (continued) — per-language body extractors.
//
// Each language registers its BodyExtractor via init(); the registry core
// lives in body.go.
package ast

import (
	gotreesitter "github.com/odvcencio/gotreesitter"
)

// ---------------------------------------------------------------------------
// Go body extractor
// ---------------------------------------------------------------------------

// goBodyExtractor extracts body text from Go function and method declarations.
type goBodyExtractor struct{}

func (e *goBodyExtractor) ExtractBody(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	if node == nil || bt == nil {
		return ""
	}
	switch bt.NodeType(node) {
	case "function_declaration", "method_declaration":
		body := bt.ChildByField(node, "body")
		if body != nil {
			return bt.NodeText(body)
		}
		return ""
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// TypeScript / JavaScript body extractor
// ---------------------------------------------------------------------------

// tsBodyExtractor extracts body text from TS/JS function-like nodes.
type tsBodyExtractor struct{}

func (e *tsBodyExtractor) ExtractBody(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	if node == nil || bt == nil {
		return ""
	}
	switch bt.NodeType(node) {
	case "function_declaration", "method_definition", "function":
		body := bt.ChildByField(node, "body")
		if body != nil {
			return bt.NodeText(body)
		}
		return ""
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// Python body extractor
// ---------------------------------------------------------------------------

// pythonBodyExtractor extracts body text from Python function and class
// definitions. Unlike Go/TS where classes return empty Body, Python classes
// return the block content since the block IS the class body in this
// indentation-based language.
type pythonBodyExtractor struct{}

func (e *pythonBodyExtractor) ExtractBody(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	if node == nil || bt == nil {
		return ""
	}
	switch bt.NodeType(node) {
	case "function_definition", "async_function_definition", "class_definition":
		body := bt.ChildByField(node, "body")
		if body != nil {
			return bt.NodeText(body)
		}
		return ""
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// Java body extractor
// ---------------------------------------------------------------------------

// javaBodyExtractor extracts body text from Java method and constructor
// declarations.
type javaBodyExtractor struct{}

func (e *javaBodyExtractor) ExtractBody(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	if node == nil || bt == nil {
		return ""
	}
	switch bt.NodeType(node) {
	case "method_declaration", "constructor_declaration":
		body := bt.ChildByField(node, "body")
		if body != nil {
			return bt.NodeText(body)
		}
		return ""
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// Rust body extractor
// ---------------------------------------------------------------------------

// rustBodyExtractor extracts body text from Rust function items.
type rustBodyExtractor struct{}

func (e *rustBodyExtractor) ExtractBody(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	if node == nil || bt == nil {
		return ""
	}
	switch bt.NodeType(node) {
	case "function_item":
		body := bt.ChildByField(node, "body")
		if body != nil {
			return bt.NodeText(body)
		}
		return ""
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// C / C++ body extractor
// ---------------------------------------------------------------------------

// cBodyExtractor extracts body text from C/C++ function definitions.
// The body is a compound_statement (curly-braced block).
type cBodyExtractor struct{}

func (e *cBodyExtractor) ExtractBody(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	if node == nil || bt == nil {
		return ""
	}
	switch bt.NodeType(node) {
	case "function_definition":
		body := bt.ChildByField(node, "body")
		if body != nil {
			return bt.NodeText(body)
		}
		return ""
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// C# body extractor
// ---------------------------------------------------------------------------

// csharpBodyExtractor extracts body text from C# method and constructor
// declarations.
type csharpBodyExtractor struct{}

func (e *csharpBodyExtractor) ExtractBody(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	if node == nil || bt == nil {
		return ""
	}
	switch bt.NodeType(node) {
	case "method_declaration", "constructor_declaration":
		body := bt.ChildByField(node, "body")
		if body != nil {
			return bt.NodeText(body)
		}
		return ""
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// Ruby body extractor
// ---------------------------------------------------------------------------

// rubyBodyExtractor extracts body text from Ruby method declarations.
// The body is in the "body" field which maps to body_statement.
type rubyBodyExtractor struct{}

func (e *rubyBodyExtractor) ExtractBody(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	if node == nil || bt == nil {
		return ""
	}
	switch bt.NodeType(node) {
	case "method", "singleton_method":
		body := bt.ChildByField(node, "body")
		if body != nil {
			return bt.NodeText(body)
		}
		return ""
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// PHP body extractor
// ---------------------------------------------------------------------------

// phpBodyExtractor extracts body text from PHP function and method declarations.
// The body is in the "body" field which maps to compound_statement.
type phpBodyExtractor struct{}

func (e *phpBodyExtractor) ExtractBody(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	if node == nil || bt == nil {
		return ""
	}
	switch bt.NodeType(node) {
	case "function_definition", "method_declaration":
		body := bt.ChildByField(node, "body")
		if body != nil {
			return bt.NodeText(body)
		}
		return ""
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// Swift body extractor
// ---------------------------------------------------------------------------

// swiftBodyExtractor extracts body text from Swift function and init declarations.
// The body is in the "body" field which maps to function_body.
type swiftBodyExtractor struct{}

func (e *swiftBodyExtractor) ExtractBody(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	if node == nil || bt == nil {
		return ""
	}
	switch bt.NodeType(node) {
	case "function_declaration", "init_declaration", "protocol_function_declaration":
		body := bt.ChildByField(node, "body")
		if body != nil {
			return bt.NodeText(body)
		}
		return ""
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// Kotlin body extractor
// ---------------------------------------------------------------------------

// kotlinBodyExtractor extracts body text from Kotlin function declarations.
// The body is in the "body" field which maps to function_body or block.
type kotlinBodyExtractor struct{}

func (e *kotlinBodyExtractor) ExtractBody(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	if node == nil || bt == nil {
		return ""
	}
	switch bt.NodeType(node) {
	case "function_declaration":
		body := bt.ChildByField(node, "body")
		if body != nil {
			return bt.NodeText(body)
		}
		return ""
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// Dart body extractor
// ---------------------------------------------------------------------------

// dartBodyExtractor extracts body text from Dart function signatures.
// The body is a sibling function_body node, not a child.  In tree-sitter-dart,
// function_signature and function_body are separate sibling nodes at the same
// level (e.g., at root for top-level functions, or in class_body for methods).
type dartBodyExtractor struct{}

func (e *dartBodyExtractor) ExtractBody(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	if node == nil || bt == nil {
		return ""
	}
	switch bt.NodeType(node) {
	case "function_signature", "method_signature":
		// The body is the next sibling function_body node.
		body := node.NextSibling()
		if body != nil && bt.NodeType(body) == "function_body" {
			return bt.NodeText(body)
		}
		return ""
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// Lua body extractor
// ---------------------------------------------------------------------------

// luaBodyExtractor extracts body text from Lua function declarations.
// The body is the "body" field which maps to a block node.
type luaBodyExtractor struct{}

func (e *luaBodyExtractor) ExtractBody(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	if node == nil || bt == nil {
		return ""
	}
	switch bt.NodeType(node) {
	case "function_declaration":
		body := bt.ChildByField(node, "body")
		if body != nil {
			return bt.NodeText(body)
		}
		return ""
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// Haskell body extractor
// ---------------------------------------------------------------------------

// haskellBodyExtractor extracts body text from Haskell bind nodes.
// Haskell has no braces — the "body" field may not be consistently set.
// This is a best-effort extractor.
type haskellBodyExtractor struct{}

func (e *haskellBodyExtractor) ExtractBody(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	if node == nil || bt == nil {
		return ""
	}
	switch bt.NodeType(node) {
	case "bind":
		body := bt.ChildByField(node, "body")
		if body != nil {
			return bt.NodeText(body)
		}
		// Fallback: look for a match child.
		for i := 0; i < node.ChildCount(); i++ {
			child := node.Child(i)
			if child == nil || !child.IsNamed() {
				continue
			}
			if bt.NodeType(child) == "match" {
				return bt.NodeText(child)
			}
		}
		return ""
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// Bash body extractor
// ---------------------------------------------------------------------------

// bashBodyExtractor extracts body text from Bash function definitions.
// The body is the "body" field which maps to compound_statement.
type bashBodyExtractor struct{}

func (e *bashBodyExtractor) ExtractBody(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	if node == nil || bt == nil {
		return ""
	}
	switch bt.NodeType(node) {
	case "function_definition":
		body := bt.ChildByField(node, "body")
		if body != nil {
			return bt.NodeText(body)
		}
		return ""
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// Elixir body extractor
// ---------------------------------------------------------------------------

// elixirBodyExtractor extracts body text from Elixir call nodes representing
// function definitions (def/defp/defmacro).  The body is the do_block child.
type elixirBodyExtractor struct{}

func (e *elixirBodyExtractor) ExtractBody(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	if node == nil || bt == nil {
		return ""
	}
	if bt.NodeType(node) != "call" {
		return ""
	}
	// Only extract body for function-definition calls.
	keyword := elixirKeyword(node, bt)
	if keyword == "" {
		return ""
	}
	// Find the do_block child.
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "do_block" {
			return bt.NodeText(child)
		}
	}
	return ""
}

// elixirKeyword returns the keyword from a call node ("def", "defp", etc.).
func elixirKeyword(callNode *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	for i := 0; i < callNode.ChildCount(); i++ {
		child := callNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "identifier" {
			return bt.NodeText(child)
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Generic fallback
// ---------------------------------------------------------------------------

// genericBodyExtractor is a best-effort fallback for languages without a
// registered BodyExtractor. It looks for child fields named "body" or "block".
type genericBodyExtractor struct{}

func (e *genericBodyExtractor) ExtractBody(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	if node == nil || bt == nil {
		return ""
	}
	// Try common field names for body nodes across grammars.
	for _, field := range []string{"body", "block"} {
		child := bt.ChildByField(node, field)
		if child != nil {
			return bt.NodeText(child)
		}
	}
	return ""
}

// init registers the body extractors for all supported languages.
func init() {
	RegisterBodyExtractor("go", &goBodyExtractor{})
	RegisterBodyExtractor("typescript", &tsBodyExtractor{})
	RegisterBodyExtractor("tsx", &tsBodyExtractor{})
	RegisterBodyExtractor("javascript", &tsBodyExtractor{})
	RegisterBodyExtractor("python", &pythonBodyExtractor{})
	RegisterBodyExtractor("java", &javaBodyExtractor{})
	RegisterBodyExtractor("rust", &rustBodyExtractor{})
	RegisterBodyExtractor("c", &cBodyExtractor{})
	RegisterBodyExtractor("cpp", &cBodyExtractor{})
	RegisterBodyExtractor("c_sharp", &csharpBodyExtractor{})
	RegisterBodyExtractor("ruby", &rubyBodyExtractor{})
	RegisterBodyExtractor("php", &phpBodyExtractor{})
	RegisterBodyExtractor("swift", &swiftBodyExtractor{})
	RegisterBodyExtractor("kotlin", &kotlinBodyExtractor{})
	RegisterBodyExtractor("dart", &dartBodyExtractor{})
	RegisterBodyExtractor("lua", &luaBodyExtractor{})
	RegisterBodyExtractor("haskell", &haskellBodyExtractor{})
	RegisterBodyExtractor("bash", &bashBodyExtractor{})
	RegisterBodyExtractor("elixir", &elixirBodyExtractor{})
}
