package tools

// Package tools: tree-sitter based symbol extraction for repo_map (split from repo_map.go).

import (
	"fmt"
	"path/filepath"
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

	var entries []SymbolEntry
	for _, sym := range result.Symbols {
		prefix := symbolDisplayPrefix(sym.Kind, ext)
		entries = append(entries, SymbolEntry{
			Name: prefix + " " + sym.Name,
			Line: sym.StartLine,
		})
	}
	return entries, nil
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
		return extractSymbolsViaTreeSitter(path, ext, content)
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

	var entries []SymbolEntry
	for _, sym := range result.Symbols {
		prefix := symbolDisplayPrefix(sym.Kind, ext)
		entries = append(entries, SymbolEntry{
			Name: prefix + " " + sym.Name,
			Line: sym.StartLine,
		})
	}

	// Resolve call edges using the import map built from source content.
	// This handles cross-file module resolution for TS/JS/Python so that
	// get_callers / get_callees / find_dead_code work correctly.
	edges := resolveEdgesForTS(result.Calls, buildTSImportMap(path, content))

	return &SymbolWithEdges{Symbols: entries, Edges: edges}, nil
}
