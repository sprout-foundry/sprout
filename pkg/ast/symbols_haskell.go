package ast

import (
	gotreesitter "github.com/odvcencio/gotreesitter"
)

// ---------------------------------------------------------------------------
// Haskell scoped symbol extraction
// ---------------------------------------------------------------------------

// extractHaskellSymbols walks the AST for Haskell source and extracts
// top-level data types, functions (binds), type synonyms, and type signatures.
//
// Haskell grammar:
//
//	haskell → header → module; declarations → data_type / bind / signature / type_synomym
//
// The root typically has a "header" child (with "module" inside) and a
// "declarations" child that contains all top-level declarations.
//
// `bind` nodes are function definitions and may contain a `match` child with
// the actual function name in its pattern.
//
// `type_synomym` (note: grammar spelling is "synomym" not "synonym") represents
// type aliases like "type Foo = Bar".
func extractHaskellSymbols(root *gotreesitter.Node, bt *gotreesitter.BoundTree, maxDepth int, lang string) []ScopedSymbol {
	var symbols []ScopedSymbol

	// Find the declarations node. It's usually a direct child of root.
	var decls *gotreesitter.Node
	for i := 0; i < root.ChildCount(); i++ {
		child := root.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "declarations" {
			decls = child
			break
		}
	}
	// If no declarations wrapper, walk root children directly.
	target := decls
	if target == nil {
		target = root
	}

	walkHaskellDecls(target, bt, maxDepth, lang, &symbols)

	return symbols
}

// walkHaskellDecls iterates over the children of a declarations node (or root)
// and extracts symbols.
func walkHaskellDecls(node *gotreesitter.Node, bt *gotreesitter.BoundTree, maxDepth int, lang string, symbols *[]ScopedSymbol) {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		nodeType := bt.NodeType(child)

		switch nodeType {
		case "declarations":
			// Nested declarations (shouldn't normally happen, but handle it).
			walkHaskellDecls(child, bt, maxDepth, lang, symbols)

		case "data_type":
			name := childText(child, bt, "name")
			if name == "" {
				// Fallback: look for a "name" child node.
				for j := 0; j < child.ChildCount(); j++ {
					c := child.Child(j)
					if c == nil || !c.IsNamed() {
						continue
					}
					if bt.NodeType(c) == "name" {
						name = bt.NodeText(c)
						break
					}
				}
			}
			if name == "" {
				continue
			}
			*symbols = append(*symbols, scopedSymbol(name, "class", "", child, 0))

			// Extract data constructors as nested constants if we have depth budget.
			if maxDepth > 1 {
				*symbols = append(*symbols, extractHaskellDataConstructors(child, bt, name)...)
			}

		case "bind":
			name := extractHaskellBindName(child, bt)
			if name != "" {
				*symbols = append(*symbols, scopedSymbolWithBody(name, "function", "", child, bt, 0, lang))
			}

		case "type_synomym": // Note: grammar uses "synomym" (typo in the grammar itself).
			name := childText(child, bt, "name")
			if name == "" {
				for j := 0; j < child.ChildCount(); j++ {
					c := child.Child(j)
					if c == nil || !c.IsNamed() {
						continue
					}
					if bt.NodeType(c) == "name" {
						name = bt.NodeText(c)
						break
					}
				}
			}
			if name != "" {
				*symbols = append(*symbols, scopedSymbol(name, "type", "", child, 0))
			}

		case "signature":
			// Type signatures like "add :: Int -> Int -> Int".
			// Skip these — they're annotations, not definitions.
			// The function itself is captured by the corresponding `bind` node.

		default:
			// Skip header, module, pragma, etc.
		}
	}
}

// extractHaskellBindName returns the function name from a bind node.
//
// The bind may have a "name" field, or the name may be inside a `match` child.
// The match contains the function name as the first identifier.
func extractHaskellBindName(bindNode *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	// Try "name" field first.
	name := childText(bindNode, bt, "name")
	if name != "" {
		return name
	}

	// Fallback: look for the name in the match child's pattern.
	for i := 0; i < bindNode.ChildCount(); i++ {
		child := bindNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "match" {
			name = extractHaskellMatchName(child, bt)
			if name != "" {
				return name
			}
		}
		if bt.NodeType(child) == "name" {
			return bt.NodeText(child)
		}
	}

	// Last resort: walk for any "name" child.
	for i := 0; i < bindNode.ChildCount(); i++ {
		child := bindNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "name" {
			return bt.NodeText(child)
		}
	}

	return ""
}

// extractHaskellMatchName extracts the function name from a match node.
// The match typically starts with the function name as an identifier.
func extractHaskellMatchName(matchNode *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	for i := 0; i < matchNode.ChildCount(); i++ {
		child := matchNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "name" {
			return bt.NodeText(child)
		}
		// Fallback: identifier.
		if bt.NodeType(child) == "identifier" {
			return bt.NodeText(child)
		}
	}
	return ""
}

// extractHaskellDataConstructors extracts constructors from a data_type node.
func extractHaskellDataConstructors(dataTypeNode *gotreesitter.Node, bt *gotreesitter.BoundTree, scope string) []ScopedSymbol {
	if dataTypeNode == nil {
		return nil
	}

	// Find data_constructors child.
	var dcNode *gotreesitter.Node
	for i := 0; i < dataTypeNode.ChildCount(); i++ {
		child := dataTypeNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if bt.NodeType(child) == "data_constructors" {
			dcNode = child
			break
		}
	}
	if dcNode == nil {
		return nil
	}

	var symbols []ScopedSymbol
	for i := 0; i < dcNode.ChildCount(); i++ {
		child := dcNode.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		// Constructors are "constructor" nodes with a "name" field or a
		// "name" child node.
		if bt.NodeType(child) == "constructor" {
			name := childText(child, bt, "name")
			if name == "" {
				// Fallback: look for a "name" child.
				for j := 0; j < child.ChildCount(); j++ {
					c := child.Child(j)
					if c == nil || !c.IsNamed() {
						continue
					}
					if bt.NodeType(c) == "name" {
						name = bt.NodeText(c)
						break
					}
				}
			}
			if name != "" {
				symbols = append(symbols, scopedSymbol(name, "constant", scope, child, 1))
			}
		}
	}

	return symbols
}
