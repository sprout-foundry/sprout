// Package ast provides a unified AST parser using gotreesitter (pure Go
// tree-sitter) for Go, TypeScript, JavaScript, and Python source files.
//
// The parser pre-loads grammar blobs at init time so that the first call to
// ParseFile does not pay the grammar-loading cost.  It is safe for concurrent
// use: each call to ParseFile creates its own parser instance.
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
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// SupportedLanguages is the set of languages this package handles via
// tree-sitter.  Callers can check membership to decide whether to use
// the AST parser or fall back to regex / language-native tools.
var SupportedLanguages = map[string]bool{
	"go":         true,
	"typescript": true,
	"tsx":        true,
	"javascript": true,
	"python":     true,
}

// ASTResult holds the output of ParseFile: the concrete syntax tree, a
// bound tree for node-text queries, and extracted top-level symbols.
type ASTResult struct {
	// Language is the detected language name (e.g. "go", "python").
	Language string

	// FilePath is the path that was passed to ParseFile.
	FilePath string

	// Root is the root node of the parse tree.  Use this for direct
	// tree traversal when the caller needs full control.
	Root *gotreesitter.Node

	// Source is a reference to the parsed source bytes.  It is retained
	// only for the lifetime of the BoundTree; callers that need the
	// source longer should keep their own copy.
	Source []byte

	// Tree is the underlying parse tree.  Callers MUST call result.Release()
	// when finished to free arena memory.
	Tree *gotreesitter.Tree

	// Bound is a convenience wrapper that keeps the source buffer alive
	// so that Node.Text / NodeType queries work without the caller
	// tracking the source slice.
	Bound *gotreesitter.BoundTree

	// Symbols is a list of top-level symbols extracted from the AST.
	Symbols []Symbol
}

// Release frees the parse tree and bound tree.  It is safe to call
// multiple times.  After Release, the Root, Source, Tree, and Bound
// fields are nilled to prevent use-after-release.
func (r *ASTResult) Release() {
	if r.Bound != nil {
		r.Bound.Release()
		r.Bound = nil
	}
	if r.Tree != nil {
		r.Tree.Release()
		r.Tree = nil
	}
	r.Root = nil
	r.Source = nil
}

// Symbol represents a top-level code symbol extracted from the AST.
type Symbol struct {
	// Name is the declared identifier (e.g. "MyFunc", "MyStruct").
	Name string

	// Kind is a normalised symbol kind: "function", "method", "class",
	// "interface", "type", "variable", "constant", "import", "decorator",
	// "property", "enum", or "module".
	Kind string

	// StartLine is the 1-based line number where the symbol starts.
	StartLine int

	// EndLine is the 1-based line number where the symbol ends (inclusive).
	EndLine int

	// StartByte is the 0-based byte offset where the symbol starts.
	StartByte int

	// EndByte is the 0-based byte offset where the symbol ends.
	EndByte int

	// Body is the source text of the function/method body (between braces
	// or after colon). Empty for non-function symbols (classes, types,
	// variables, etc.), except for Python classes where the block IS the
	// body.
	Body string
}

// langEntry caches a resolved Language so we only load the grammar blob once
// per language.
type langEntry struct {
	name string
	lang *gotreesitter.Language
}

var (
	langCacheMu sync.RWMutex
	langCache   = make(map[string]*langEntry)
)

// getLanguage resolves a language name to its gotreesitter.Language,
// caching the result for subsequent calls.
func getLanguage(name string) (*langEntry, error) {
	langCacheMu.RLock()
	if e, ok := langCache[name]; ok {
		langCacheMu.RUnlock()
		return e, nil
	}
	langCacheMu.RUnlock()

	langCacheMu.Lock()
	defer langCacheMu.Unlock()

	// Double-check after acquiring write lock.
	if e, ok := langCache[name]; ok {
		return e, nil
	}

	entry := grammars.DetectLanguageByName(name)
	if entry == nil {
		return nil, fmt.Errorf("ast: no grammar registered for language %q", name)
	}
	if entry.Language == nil {
		return nil, fmt.Errorf("ast: grammar entry for %q has no Language loader", name)
	}

	lang := entry.Language()
	if lang == nil {
		return nil, fmt.Errorf("ast: language loader for %q returned nil", name)
	}

	e := &langEntry{name: name, lang: lang}
	langCache[name] = e
	return e, nil
}

// detectLangFromFile determines the language for a given file path using the
// gotreesitter grammar registry.
func detectLangFromFile(filePath string) (string, error) {
	entry := grammars.DetectLanguage(filePath)
	if entry == nil {
		return "", fmt.Errorf("ast: unsupported file type: %s", filePath)
	}
	return entry.Name, nil
}

// ParseFile parses source content using tree-sitter and returns an ASTResult
// with the concrete syntax tree and extracted top-level symbols.
//
// filePath is used only for language detection (via extension).  content is
// the raw source bytes to parse.
//
// The caller MUST call result.Release() when done to free the parse tree.
func ParseFile(filePath string, content []byte) (*ASTResult, error) {
	if filePath == "" {
		return nil, fmt.Errorf("ast: filePath must not be empty")
	}

	langName, err := detectLangFromFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("ast: detect language: %w", err)
	}

	return parse(langName, filePath, content)
}

// ParseContent parses source content with an explicit language name.  Use this
// when the file path is unavailable or misleading (e.g. stdin content).
func ParseContent(language string, content []byte) (*ASTResult, error) {
	if language == "" {
		return nil, fmt.Errorf("ast: language must not be empty")
	}
	return parse(language, "", content)
}

// parse is the shared implementation for ParseFile and ParseContent.
func parse(langName, filePath string, content []byte) (*ASTResult, error) {
	entry, err := getLanguage(langName)
	if err != nil {
		return nil, err
	}

	parser := gotreesitter.NewParser(entry.lang)
	if parser == nil {
		return nil, fmt.Errorf("ast: failed to create parser for language %s", langName)
	}

	tree, parseErr := parser.Parse(content)
	if parseErr != nil {
		return nil, fmt.Errorf("ast: parse failed for %s (language %s): %w", filePath, langName, parseErr)
	}
	if tree == nil {
		return nil, fmt.Errorf("ast: parse returned nil tree for %s (language %s)", filePath, langName)
	}

	root := tree.RootNode()
	bound := gotreesitter.Bind(tree)

	symbols := extractSymbols(root, bound, langName)

	return &ASTResult{
		Language: langName,
		FilePath: filePath,
		Root:     root,
		Source:   content,
		Tree:     tree,
		Bound:    bound,
		Symbols:  symbols,
	}, nil
}

// IsSupported returns true if the file extension maps to a language with a
// pre-compiled grammar in this package.
func IsSupported(filePath string) bool {
	if filePath == "" {
		return false
	}
	entry := grammars.DetectLanguage(filePath)
	if entry == nil {
		return false
	}
	return SupportedLanguages[entry.Name]
}

// DetectLanguage returns the language name for a file path, or empty string
// if unsupported.
func DetectLanguage(filePath string) string {
	entry := grammars.DetectLanguage(filePath)
	if entry == nil {
		return ""
	}
	return entry.Name
}

// --- Symbol extraction -------------------------------------------------------

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

// --- Helpers ------------------------------------------------------------------

func makeSymbol(name, kind string, node *gotreesitter.Node) Symbol {
	return Symbol{
		Name:      name,
		Kind:      kind,
		StartLine: int(node.StartPoint().Row) + 1,
		EndLine:   int(node.EndPoint().Row) + 1,
		StartByte: int(node.StartByte()),
		EndByte:   int(node.EndByte()),
	}
}

func makeSymbolWithBody(name, kind string, node *gotreesitter.Node, bt *gotreesitter.BoundTree, lang string) Symbol {
	s := makeSymbol(name, kind, node)
	s.Body = extractBody(node, bt, lang)
	return s
}

// childText returns the text of the named child field, or "" if not found.
func childText(node *gotreesitter.Node, bt *gotreesitter.BoundTree, field string) string {
	child := bt.ChildByField(node, field)
	if child == nil {
		return ""
	}
	return bt.NodeText(child)
}

// init pre-warms the grammar cache for the four supported languages so the
// first parse is fast.
func init() {
	for lang := range SupportedLanguages {
		// Best-effort: if a grammar is not available (e.g. trimmed build),
		// silently skip it.
		_, err := getLanguage(lang)
		if err != nil {
			continue
		}
	}
}

// WalkFn is the callback type for Walk.  Return false to stop walking.
type WalkFn func(node *gotreesitter.Node, nodeType string, depth int) bool

// Walk performs a depth-first walk of the AST rooted at node, calling fn for
// each named node.  The nodeType is resolved using the BoundTree.
//
// This is a convenience wrapper for callers that need the node type string
// without managing a BoundTree themselves.
func Walk(node *gotreesitter.Node, bt *gotreesitter.BoundTree, fn WalkFn) {
	var walkRecursive func(n *gotreesitter.Node, depth int) bool
	walkRecursive = func(n *gotreesitter.Node, depth int) bool {
		if n == nil || bt == nil || !n.IsNamed() {
			return true
		}
		nodeType := bt.NodeType(n)
		if !fn(n, nodeType, depth) {
			return false
		}
		for i := 0; i < n.ChildCount(); i++ {
			if !walkRecursive(n.Child(i), depth+1) {
				return false
			}
		}
		return true
	}
	walkRecursive(node, 0)
}

// FileExtension returns the normalised file extension including the dot, or
// empty string if the path has no extension.
func FileExtension(filePath string) string {
	ext := filepath.Ext(filePath)
	return strings.ToLower(ext)
}
