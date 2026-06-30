package ast

import (
	gotreesitter "github.com/odvcencio/gotreesitter"
)

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
//
//	block → assignment → identifier : type = integer
//
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
