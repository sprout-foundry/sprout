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
	"fmt"
	"os"
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
	"kotlin":     true,
	"swift":      true,
	"ruby":       true,
	"c":          true,
	"cpp":        true,
	"c_sharp":    true,
	"java":       true,
	"rust":       true,
	"php":        true,
	"dart":       true,
	"lua":        true,
	"elixir":     true,
	"haskell":    true,
	"bash":       true,
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

	// Calls is a list of call edges extracted from the AST.
	// Each edge represents a function/method call found within a function body.
	Calls []CallEdge
}

// CallEdge represents a call from one function to another.
type CallEdge struct {
	CallerName string // name of the calling function
	CalleeName string // name of the called function
	Line       int    // line number of the call
	CallerLine int    // line number of the caller function
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
	calls := extractCalls(root, bound, langName, symbols)

	return &ASTResult{
		Language: langName,
		FilePath: filePath,
		Root:     root,
		Source:   content,
		Tree:     tree,
		Bound:    bound,
		Symbols:  symbols,
		Calls:    calls,
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

// extractCalls walks the AST to find call expressions within function/method
// bodies and returns CallEdge values for each call found.
//
// It uses the symbols list to determine which function body each call falls
// inside by checking byte-range containment.
//
// Calls that occur outside any function body (e.g., in package-level
// variable initializers) are silently dropped since there is no enclosing
// function symbol to attribute them to.
func extractCalls(root *gotreesitter.Node, bt *gotreesitter.BoundTree, lang string, symbols []Symbol) []CallEdge {
	lang = strings.ToLower(lang)

	// Collect all function/method symbols that have a body (i.e., can contain calls).
	var funcSymbols []Symbol
	for _, sym := range symbols {
		if sym.Body != "" {
			funcSymbols = append(funcSymbols, sym)
		}
	}

	// Synthesize a file-level <init> symbol that spans the whole file so calls
	// in package-level variable initializers (var x = fn()) get attributed to
	// something other than dropped. The codegraph's FindDeadCode excludes
	// anything with an inbound edge, so emitting from <init> keeps init-time
	// callees alive in the graph.
	startByte := 0
	endByte := 0
	if root != nil {
		endByte = int(root.EndByte())
	}
	initSymbol := Symbol{
		Name:      "<init>",
		StartLine: 1,
		EndLine:   1,
		StartByte: startByte,
		EndByte:   endByte,
		Kind:      "function",
		Body:      "file-scope init",
	}
	funcSymbols = append(funcSymbols, initSymbol)

	if len(funcSymbols) == 0 {
		return nil
	}

	// Determine the call node type for the language.
	var callNodeType string
	switch lang {
	case "go":
		callNodeType = "call_expression"
	case "typescript", "tsx", "javascript":
		callNodeType = "call_expression"
	case "python":
		callNodeType = "call"
	case "java", "c", "cpp", "c_sharp", "rust":
		callNodeType = "call_expression"
	case "ruby":
		callNodeType = "call"
	case "php":
		callNodeType = "function_call_expression"
	case "swift":
		callNodeType = "call_expression"
	case "kotlin":
		callNodeType = "call_expression"
	case "dart":
		callNodeType = "function_invocation"
	case "lua":
		callNodeType = "function_call"
	case "bash":
		callNodeType = "command"
	default:
		return nil
	}

	var edges []CallEdge

	// Walk the entire tree looking for call nodes.
	Walk(root, bt, func(node *gotreesitter.Node, nodeType string, depth int) bool {
		if nodeType == callNodeType {
			// Extract callee name from the call expression.
			calleeName := extractCalleeName(node, bt)
			if calleeName == "" {
				return true
			}

			callByte := int(node.StartByte())
			callLine := int(node.StartPoint().Row) + 1

			// Find which function symbol contains this call.
			for _, sym := range funcSymbols {
				if callByte >= sym.StartByte && callByte <= sym.EndByte {
					edges = append(edges, CallEdge{
						CallerName: sym.Name,
						CalleeName: calleeName,
						Line:       callLine,
						CallerLine: sym.StartLine,
					})
					break
				}
			}
		}
		return true
	})

	return edges
}

// extractCalleeName extracts the callee function name from a call expression node.
func extractCalleeName(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	// The "function" field of a call_expression contains the callee.
	funcChild := bt.ChildByField(node, "function")
	if funcChild == nil {
		// Fallback: first named child.
		for i := 0; i < node.ChildCount(); i++ {
			child := node.Child(i)
			if child != nil && child.IsNamed() {
				return bt.NodeText(child)
			}
		}
		return ""
	}
	return bt.NodeText(funcChild)
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
// first parse is fast. Skipped when SPROUT_SKIP_GRAMMAR_PREWARM=1 — used by
// test helpers that spawn the test binary as a subprocess (e.g. the daemon
// helper). Under `go test -race`, gob-decoding every embedded grammar blob
// at init can take tens of seconds, which makes a spawned helper unable to
// become healthy within any reasonable startup window. The helper never
// parses code, so skipping the pre-warm is safe.
func init() {
	if os.Getenv("SPROUT_SKIP_GRAMMAR_PREWARM") == "1" {
		return
	}
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
