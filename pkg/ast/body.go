// Package ast (continued) — body extraction.
//
// This file provides an extensible body extraction system via a registry
// pattern. Each language registers its own BodyExtractor, and new grammars
// can be supported by calling RegisterBodyExtractor.
package ast

import (
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// BodyExtractor extracts the body text from a symbol node.
// Implementations should return the body text for function-like nodes
// and empty string for non-function nodes (classes, types, etc.),
// except where the language's semantics make the body meaningful
// (e.g. Python classes where the block IS the body).
type BodyExtractor interface {
	// ExtractBody returns the source text of the body for the given node,
	// or empty string if the node is not a function-like declaration.
	ExtractBody(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string
}

// bodyExtractorRegistry maps language names to their BodyExtractor.
var bodyExtractorRegistry = map[string]BodyExtractor{}

// RegisterBodyExtractor registers a body extractor for a language.
// This enables extensibility: new grammar support can register their own extractor.
func RegisterBodyExtractor(lang string, ext BodyExtractor) {
	bodyExtractorRegistry[strings.ToLower(lang)] = ext
}

// extractBody looks up the registered BodyExtractor for the given language
// and extracts the body text. Falls back to the generic extractor for
// unregistered languages.
func extractBody(node *gotreesitter.Node, bt *gotreesitter.BoundTree, lang string) string {
	lang = strings.ToLower(lang)
	if ext, ok := bodyExtractorRegistry[lang]; ok {
		return ext.ExtractBody(node, bt)
	}
	return (&genericBodyExtractor{}).ExtractBody(node, bt)
}

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
}
