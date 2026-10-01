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
