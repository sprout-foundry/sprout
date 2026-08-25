package tools

// Package tools: tree-sitter based symbol extraction for repo_map (split from repo_map.go).

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	astp "github.com/sprout-foundry/sprout/pkg/ast"
	codegraph "github.com/sprout-foundry/sprout/pkg/codegraph"
)

// treeSitterExtensions is the set of file extensions handled by the tree-sitter
// based pkg/ast parser. Go files use go/ast directly.
var treeSitterExtensions = map[string]bool{
	".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".py": true,
}

// extractSymbolsViaTreeSitter uses the pkg/ast tree-sitter parser to extract
// symbols from TS/JS/Python files.
func extractSymbolsViaTreeSitter(path string, ext string, content []byte) ([]SymbolEntry, error) {
	result, err := astp.ParseFile(path, content)
	if err != nil {
		return nil, err
	}
	defer result.Release()

	return scopedSymbolEntries(result, ext), nil
}

// scopedSymbolEntries converts a parsed file's symbols into repo-map entries
// using the SAME extractor the embedding index uses.
//
// This previously read result.Symbols, which parser.go populates by walking
// only the top-level children of the root. ast.ExtractSymbols walks nested
// scopes as well — methods inside classes, class methods in Python, nested
// functions. Two consumers of one parser were therefore disagreeing about what
// a file contains: the semantic index knew about methods that the repo map,
// which the agent reads to decide what to open, silently omitted. repo_map's
// own `depth: 3` is documented as "full symbols", so the old behaviour did not
// match its contract either.
//
// Nested symbols are qualified with their scope ("Class.method") so the map
// stays unambiguous when several classes define the same method name.
func scopedSymbolEntries(result *astp.ASTResult, ext string) []SymbolEntry {
	scoped := astp.ExtractSymbols(result.Root, result.Bound, result.Language)

	entries := make([]SymbolEntry, 0, len(scoped))
	for _, sym := range scoped {
		name := sym.Name
		if sym.Scope != "" {
			name = sym.Scope + "." + name
		}
		entries = append(entries, SymbolEntry{
			Name: symbolDisplayPrefix(sym.Kind, ext) + " " + name,
			Line: sym.StartLine,
		})
	}
	return entries
}

// symbolDisplayPrefix maps an AST symbol kind to the display prefix used in
// the repo map output.  This preserves backward compatibility with the
// previous regex-based output format (e.g. "def" for Python functions,
// "const" for TS/JS variables).
func symbolDisplayPrefix(kind string, ext string) string {
	switch ext {
	case ".py":
		if kind == "function" {
			return "def"
		}
		return kind
	case ".ts", ".tsx", ".js", ".jsx":
		if kind == "variable" {
			return "const"
		}
		return kind
	default:
		return kind
	}
}

// SymbolEntry pairs a symbol name with its 1-based line number.
type SymbolEntry struct {
	Name string
	Line int
}

// SymbolWithEdges holds symbols and call edges for a single file.
type SymbolWithEdges struct {
	Symbols []SymbolEntry
	Edges   []codegraph.Edge
}

// ToCodegraphSymbols converts the SymbolWithEdges to codegraph Symbol and Edge slices.
// filePath is the relative path of the source file.
func (s *SymbolWithEdges) ToCodegraphSymbols(filePath string) ([]codegraph.Symbol, []codegraph.Edge, error) {
	// Infer language from file extension.
	ext := strings.ToLower(filepath.Ext(filePath))

	// Construct qualified name prefix from the file path.
	// For a file like "pkg/app/app.go", prefix is "pkg/app"
	// For "src/utils.ts", prefix is "src"
	dir := filepath.Dir(filePath)
	pkgPrefix := strings.ReplaceAll(dir, string(filepath.Separator), "/")

	var symbols []codegraph.Symbol
	for _, se := range s.Symbols {
		// Parse kind and display name from the symbol entry name.
		// Go symbols look like: "func run", "type User", "func (*Server).Start"
		// TS/JS/Python symbols look like: "main", "function greet", "class App", "def helper"
		kind := inferKind(se.Name)
		displayName := cleanDisplayName(se.Name)

		qualifiedName := pkgPrefix + "." + displayName

		symbols = append(symbols, codegraph.Symbol{
			QualifiedName: qualifiedName,
			DisplayName:   displayName,
			FilePath:      filePath,
			Line:          se.Line,
			Kind:          kind,
			Language:      inferLanguage(ext),
			FileMTime:     "", // filled in by parseAndEnrich
		})
	}

	// Build a map from bare names → qualified names so edge Source/Target
	// names can be resolved to the same qualified form used for nodes.
	// Go edges use goFuncName() output ("func run", "func (*Server).Start");
	// TS/JS/Python edges use CallerName/CalleeName (the bare function name).
	// Both the raw entry name (with prefix) and the cleaned display name are
	// mapped so edges from either extractor path resolve correctly.
	nameToQualified := make(map[string]string, len(s.Symbols)*2)
	for _, se := range s.Symbols {
		displayName := cleanDisplayName(se.Name)
		qualifiedName := pkgPrefix + "." + displayName
		nameToQualified[displayName] = qualifiedName
		nameToQualified[se.Name] = qualifiedName // Go: "func run" → "pkg/app.run"
	}

	// Transform edge names to qualified form.
	if s.Edges == nil {
		return symbols, nil, nil
	}
	edges := make([]codegraph.Edge, 0, len(s.Edges))
	for _, e := range s.Edges {
		srcQual := e.SourceQualifiedName
		if qn, ok := nameToQualified[srcQual]; ok {
			srcQual = qn
		}
		tgtQual := e.TargetQualifiedName
		if qn, ok := nameToQualified[tgtQual]; ok {
			tgtQual = qn
		}
		edges = append(edges, codegraph.Edge{
			SourceQualifiedName: srcQual,
			TargetQualifiedName: tgtQual,
			EdgeType:            e.EdgeType,
			Line:                e.Line,
		})
	}

	return symbols, edges, nil
}

// inferKind extracts the symbol kind from the display name prefix.
func inferKind(name string) string {
	if strings.HasPrefix(name, "func ") || strings.HasPrefix(name, "function ") {
		return "func"
	}
	if strings.HasPrefix(name, "type ") {
		return "type"
	}
	if strings.HasPrefix(name, "iface ") {
		return "iface"
	}
	if strings.HasPrefix(name, "def ") {
		return "func"
	}
	if strings.HasPrefix(name, "class ") {
		return "type"
	}
	if strings.HasPrefix(name, "const ") {
		return "const"
	}
	return "func" // default
}

// cleanDisplayName removes the kind prefix from a symbol name.
func cleanDisplayName(name string) string {
	prefixes := []string{"func ", "function ", "type ", "iface ", "def ", "class ", "const "}
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			return strings.TrimSpace(name[len(p):])
		}
	}
	return name
}

// inferLanguage returns the codegraph language string from a file extension.
func inferLanguage(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".py":
		return "python"
	default:
		return ""
	}
}

// kindToPrefix maps a codegraph symbol kind to the display prefix used in the
// repo map output.  This matches the prefixes produced by the filesystem-walk
// path (go/ast for Go, tree-sitter for TS/JS/Python).
func kindToPrefix(kind string) string {
	switch kind {
	case "func":
		return "func"
	case "type":
		return "type"
	case "iface":
		return "iface"
	case "const":
		return "const"
	case "var":
		return "var"
	default:
		return kind
	}
}

// extractSymbolsForFile extracts symbols from a file using the appropriate
// parser: go/ast for Go, tree-sitter via pkg/ast for TS/JS/Python.
// Unsupported extensions return an error.
func extractSymbolsForFile(path string, ext string, content []byte) ([]SymbolEntry, error) {
	if ext == ".go" {
		return extractGoSymbolsAST(path, content)
	}
	if treeSitterExtensions[ext] {
		symbols, err := extractSymbolsViaTreeSitter(path, ext, content)
		if err == nil && len(symbols) > 0 {
			return symbols, nil
		}
		// Fallback: tree-sitter returned 0 symbols (e.g. grammar blob can't
		// parse complex syntax). Try regex-based extraction.
		regexSymbols := extractSymbolsViaRegex(content, ext)
		if len(regexSymbols) > 0 {
			return regexSymbols, nil
		}
		return symbols, err // return whatever tree-sitter gave (possibly empty)
	}
	// Non-Go, non-tree-sitter extensions: use regex fallback
	regexSymbols := extractSymbolsViaRegex(content, ext)
	if len(regexSymbols) > 0 {
		return regexSymbols, nil
	}
	return nil, fmt.Errorf("unsupported file extension: %s", ext)
}

// extractSymbolsAndEdgesViaTreeSitter uses the pkg/ast tree-sitter parser to
// extract both symbols and call edges from TS/JS/Python files.
func extractSymbolsAndEdgesViaTreeSitter(path string, ext string, content []byte) (*SymbolWithEdges, error) {
	result, err := astp.ParseFile(path, content)
	if err != nil {
		return nil, err
	}
	defer result.Release()

	// Same scoped extractor as the symbol-only path above, so the two never
	// disagree about what a file contains.
	entries := scopedSymbolEntries(result, ext)

	// Resolve call edges using the import map built from source content.
	// This handles cross-file module resolution for TS/JS/Python so that
	// get_callers / get_callees / find_dead_code work correctly.
	edges := resolveEdgesForTS(result.Calls, buildTSImportMap(path, content))

	return &SymbolWithEdges{Symbols: entries, Edges: edges}, nil
}

// extractSymbolsViaRegex is a fallback symbol extractor for languages where
// tree-sitter produces 0 symbols (e.g. Kotlin with inheritance syntax that
// the grammar blob can't parse). Uses language-specific regex patterns.
func extractSymbolsViaRegex(content []byte, ext string) []SymbolEntry {
	text := string(content)
	var entries []SymbolEntry

	switch ext {
	case ".kt", ".kts":
		// Kotlin: class, object, interface, enum, fun
		entries = append(entries, regexKotlinSymbols(text)...)
	case ".swift":
		entries = append(entries, regexSwiftSymbols(text)...)
	case ".rb":
		entries = append(entries, regexRubySymbols(text)...)
	case ".m", ".mm":
		entries = append(entries, regexObjCSymbols(text)...)
	default:
		// Generic C-family: class, function, def
		entries = append(entries, regexGenericSymbols(text)...)
	}

	return entries
}

func regexKotlinSymbols(text string) []SymbolEntry {
	var entries []SymbolEntry
	lines := strings.Split(text, "\n")

	// Match: (class|object|interface|enum class) Name
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// class declaration
		if m := matchKotlinDecl(trimmed, `^(?:public |private |internal |open |abstract |sealed |data |final |annotation )*class\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "class " + m, Line: i + 1})
		}
		// object declaration (singleton)
		if m := matchKotlinDecl(trimmed, `^(?:public |private |internal |open |companion )*object\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "object " + m, Line: i + 1})
		}
		// interface
		if m := matchKotlinDecl(trimmed, `^(?:public |private |internal )*interface\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "interface " + m, Line: i + 1})
		}
		// enum class
		if m := matchKotlinDecl(trimmed, `^(?:public |private |internal )*enum\s+class\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "enum " + m, Line: i + 1})
		}
		// fun (top-level or class member)
		if m := matchKotlinDecl(trimmed, `^\s*(?:public |private |internal |protected |open |override |abstract |suspend |inline |operator |infix )*(?:fun(?:<[^>]+>)?)\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "fun " + m, Line: i + 1})
		}
	}

	return entries
}

func regexSwiftSymbols(text string) []SymbolEntry {
	var entries []SymbolEntry
	lines := strings.Split(text, "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if m := matchKotlinDecl(trimmed, `^(?:public |private |internal |open |final |class )*class\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "class " + m, Line: i + 1})
		}
		if m := matchKotlinDecl(trimmed, `^(?:public |private |internal |open |final |protocol )*protocol\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "protocol " + m, Line: i + 1})
		}
		if m := matchKotlinDecl(trimmed, `^(?:public |private |internal |open |final |static )*struct\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "struct " + m, Line: i + 1})
		}
		if m := matchKotlinDecl(trimmed, `^(?:public |private |internal |open |final |static )*enum\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "enum " + m, Line: i + 1})
		}
		if m := matchKotlinDecl(trimmed, `^\s*(?:public |private |internal |open |static |final |override )*(?:func|init)\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "func " + m, Line: i + 1})
		}
	}

	return entries
}

func regexRubySymbols(text string) []SymbolEntry {
	var entries []SymbolEntry
	lines := strings.Split(text, "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if m := matchKotlinDecl(trimmed, `^class\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "class " + m, Line: i + 1})
		}
		if m := matchKotlinDecl(trimmed, `^module\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "module " + m, Line: i + 1})
		}
		if m := matchKotlinDecl(trimmed, `^def\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "def " + m, Line: i + 1})
		}
	}

	return entries
}

func regexObjCSymbols(text string) []SymbolEntry {
	var entries []SymbolEntry
	lines := strings.Split(text, "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if m := matchKotlinDecl(trimmed, `^@(?:interface|implementation)\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "class " + m, Line: i + 1})
		}
		if m := matchKotlinDecl(trimmed, `^@protocol\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "protocol " + m, Line: i + 1})
		}
		if m := matchKotlinDecl(trimmed, `^[+-]\s*\((?:[\w\s*]+)\)(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "func " + m, Line: i + 1})
		}
	}

	return entries
}

func regexGenericSymbols(text string) []SymbolEntry {
	var entries []SymbolEntry
	lines := strings.Split(text, "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Python-style def
		if m := matchKotlinDecl(trimmed, `^def\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "def " + m, Line: i + 1})
		}
		// class
		if m := matchKotlinDecl(trimmed, `^class\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "class " + m, Line: i + 1})
		}
		// JS/TS function
		if m := matchKotlinDecl(trimmed, `^(?:export\s+)?(?:async\s+)?function\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "function " + m, Line: i + 1})
		}
	}

	return entries
}

// matchKotlinDecl runs a regex against a line and returns the first capture group.
func matchKotlinDecl(line, pattern string) string {
	re := regexp.MustCompile(pattern)
	matches := re.FindStringSubmatch(line)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}
