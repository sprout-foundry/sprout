package index

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/ast"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

const maxFileSize = 1 << 20 // 1MB — skip larger files during indexing

type Symbol struct {
	Name string `json:"name"`
	Kind string `json:"kind"` // func, class, type, method, interface, constant, variable, struct, enum, trait, impl
	Line int    `json:"line,omitempty"`
}

type FileSymbols struct {
	File    string   `json:"file"`
	Symbols []Symbol `json:"symbols"`
}

type SymbolIndex struct {
	Files []FileSymbols `json:"files"`
}

// LoadSymbols reads the cached symbol index from {root}/.sprout/symbols.json.
// Returns nil, nil if the cache file doesn't exist.
func LoadSymbols(root string) (*SymbolIndex, error) {
	cachePath := filepath.Join(root, ".sprout", "symbols.json")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var idx SymbolIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	// Validate that files still exist on disk
	validFiles := make([]FileSymbols, 0, len(idx.Files))
	for _, fs := range idx.Files {
		absPath := filepath.Join(root, fs.File)
		if _, err := os.Stat(absPath); err == nil {
			validFiles = append(validFiles, fs)
		}
	}
	idx.Files = validFiles
	return &idx, nil
}

// BuildSymbols scans the workspace root for source files and extracts symbols
// using tree-sitter AST for supported languages (Go, Python, TypeScript, JavaScript)
// with regex fallback for others. Results are sorted by file path and then by
// line number within each file.
func BuildSymbols(root string) (*SymbolIndex, error) {
	// Safety: refuse to index a user's home directory.
	if filesystem.IsHomeDir(root) {
		return nil, fmt.Errorf("index: refusing to build symbols for home directory %q — pass a project directory instead", root)
	}

	var files []string
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info == nil || info.IsDir() {
			return nil
		}
		// Skip hidden directories and files
		base := filepath.Base(path)
		if strings.HasPrefix(base, ".") {
			return nil
		}
		// Skip files larger than the size limit
		if info.Size() > maxFileSize {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".go", ".py", ".js", ".ts", ".jsx", ".tsx", ".mjs", ".rb", ".php", ".rs", ".java":
			files = append(files, path)
		}
		return nil
	}); err != nil {
		log.Printf("[debug] filepath.Walk failed in BuildSymbols: %v", err)
	}

	idx := &SymbolIndex{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		rel := f
		if r, err := filepath.Rel(root, f); err == nil {
			rel = r
		}
		symbols := extractSymbols(f, b)
		if len(symbols) > 0 {
			// Sort symbols by line number
			sort.Slice(symbols, func(i, j int) bool {
				return symbols[i].Line < symbols[j].Line
			})
			idx.Files = append(idx.Files, FileSymbols{
				File:    filepath.ToSlash(rel),
				Symbols: symbols,
			})
		}
	}
	// Sort files by path
	sort.Slice(idx.Files, func(i, j int) bool {
		return idx.Files[i].File < idx.Files[j].File
	})

	// Persist to .sprout/symbols.json
	if err := os.MkdirAll(filepath.Join(root, ".sprout"), 0755); err != nil {
		log.Printf("[debug] failed to create .sprout directory: %v", err)
	}
	outPath := filepath.Join(root, ".sprout", "symbols.json")
	if f, err := os.Create(outPath); err == nil {
		if err := json.NewEncoder(f).Encode(idx); err != nil {
			log.Printf("[debug] failed to write symbol index: %v", err)
		}
		if err := f.Close(); err != nil {
			log.Printf("[debug] failed to close symbol index file: %v", err)
		}
	}
	return idx, nil
}

// extractSymbols extracts symbols from a file.
// For AST-supported languages (Go, TypeScript, JavaScript, Python), it uses
// tree-sitter via pkg/ast as the primary extraction mechanism. For Python only,
// it supplements with regex-extracted UPPER_CASE constants (which the AST parser
// does not distinguish from regular assignments).
// For unsupported languages (Ruby, PHP, Rust, Java), it falls back entirely to
// regex-based extraction.
func extractSymbols(path string, content []byte) []Symbol {
	ext := filepath.Ext(path)
	if ast.IsSupported(path) {
		primary := extractSymbolsViaAST(path, content)
		if primary != nil {
			// Supplement Python with UPPER_CASE module-level constants.
			// The AST parser treats all top-level assignments as "variable", but
			// Python convention distinguishes UPPER_CASE names as constants.
			// For Go/TS/JS, the AST is comprehensive — no supplement needed.
			if strings.ToLower(ext) == ".py" {
				supplement := extractPythonConstants(content)
				return deduplicateSymbols(append(primary, supplement...))
			}
			return primary
		}
		// AST parsing failed; fall through to regex.
	}
	return deduplicateSymbols(extractSymbolsByRegex(ext, content))
}

// extractSymbolsViaAST uses the tree-sitter-based AST parser to extract symbols.
// It filters out non-indexable kinds and maps AST kind names to index kind names.
func extractSymbolsViaAST(path string, content []byte) []Symbol {
	result, err := ast.ParseFile(path, content)
	if err != nil {
		log.Printf("[debug] AST parse failed for %s: %v (falling back to regex)", path, err)
		return nil
	}
	defer result.Release()

	scopedSymbols := ast.ExtractSymbols(result.Root, result.Bound, result.Language)
	if scopedSymbols == nil {
		return nil
	}

	isGo := result.Language == "go"
	var out []Symbol
	for _, ss := range scopedSymbols {
		// Depth 0: always include (top-level symbols)
		// Depth 1: include only Go methods (receiver-scoped methods that are
		// top-level in Go convention). Skip struct fields, interface methods,
		// class properties, etc.
		if ss.Depth > 1 {
			continue
		}
		if ss.Depth == 1 && !(ss.Kind == "method" && isGo) {
			continue
		}

		// Filter out kinds not useful for the index.
		if !isIndexableKind(ss.Kind) {
			continue
		}

		// Map AST kind to index kind.
		kind := mapKind(ss.Kind)
		if kind == "" {
			continue
		}

		out = append(out, Symbol{
			Name: ss.Name,
			Kind: kind,
			Line: ss.StartLine,
		})
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

// isIndexableKind returns true for AST kinds that are useful for the index.
// Note: "property" is intentionally excluded — struct fields and class
// properties are depth-1 symbols that are filtered out before this check
// (only Go methods at depth 1 pass through).
func isIndexableKind(kind string) bool {
	switch kind {
	case "function", "method", "class", "interface", "type", "variable", "constant", "enum":
		return true
	default:
		return false
	}
}

// mapKind converts an AST kind string to the index kind string.
// Returns "" if the kind should not be mapped.
func mapKind(kind string) string {
	switch kind {
	case "function":
		return "func"
	case "method", "class", "interface", "type", "variable", "constant", "enum":
		return kind
	default:
		return ""
	}
}

// extractPythonConstants extracts UPPER_SNAKE_CASE module-level constant
// assignments from Python source content. The AST parser treats all top-level
// assignments as "variable", but Python convention distinguishes UPPER_CASE
// names as constants. This function supplements the AST results.
func extractPythonConstants(content []byte) []Symbol {
	re := regexp.MustCompile(`(?m)^([A-Z][A-Z0-9_]*)\s*=`)
	var out []Symbol
	matches := re.FindAllSubmatchIndex(content, -1)
	for _, m := range matches {
		if len(m) >= 4 {
			name := string(content[m[2]:m[3]])
			if name != "" {
				lineNum := bytes.Count(content[:m[0]+1], []byte{'\n'}) + 1
				out = append(out, Symbol{Name: name, Kind: "constant", Line: lineNum})
			}
		}
	}
	return out
}

// extractSymbolsByRegex extracts symbols from a file using regex patterns.
// This is used as a fallback for languages not supported by the AST parser.
func extractSymbolsByRegex(ext string, content []byte) []Symbol {
	ext = strings.ToLower(ext)
	var out []Symbol

	addSymbol := func(kind, name string, line int) {
		if name != "" {
			out = append(out, Symbol{Name: name, Kind: kind, Line: line})
		}
	}

	// addPatternMatch finds all regex matches in content, calculates line
	// numbers from byte offsets, and passes the first capture group to fn.
	//
	// NOTE on line calculation: Go's regexp with (?m)^ can match starting at
	// the '\n' character itself (not the character after it). When a blank line
	// precedes the match, m[0] points to '\n', so we must include that byte in
	// the newline count to get the correct line number.
	addPatternMatch := func(pattern, kind string, fn func(match []byte) string) {
		re := regexp.MustCompile(pattern)
		matches := re.FindAllSubmatchIndex(content, -1)
		for _, m := range matches {
			if len(m) >= 4 {
				name := fn(content[m[2]:m[3]])
				if name != "" {
					lineNum := bytes.Count(content[:m[0]+1], []byte{'\n'}) + 1
					addSymbol(kind, string(name), lineNum)
				}
			}
		}
	}

	switch ext {
	case ".go":
		// Functions: func Name(
		addPatternMatch(`(?m)^\s*func\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`, "func", func(b []byte) string { return string(b) })
		// Methods: func (recv) Name(
		addPatternMatch(`(?m)^\s*func\s+\([^)]+\)\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`, "method", func(b []byte) string { return string(b) })
		// Types: type Name struct
		addPatternMatch(`(?m)^\s*type\s+([A-Za-z_][A-Za-z0-9_]*)\s+struct\b`, "type", func(b []byte) string { return string(b) })
		// Interfaces: type X interface {
		addPatternMatch(`(?m)^\s*type\s+([A-Za-z_][A-Za-z0-9_]*)\s+interface\s*\{`, "interface", func(b []byte) string { return string(b) })
		// Type aliases: type X = Y
		addPatternMatch(`(?m)^\s*type\s+([A-Za-z_][A-Za-z0-9_]*)\s*=`, "type", func(b []byte) string { return string(b) })
		// Other named types: type Status int, type Handler func(...), type Slice []T
		// (catch-all for types not matching struct/interface/alias above)
		addPatternMatch(`(?m)^\s*type\s+([A-Za-z_][A-Za-z0-9_]*)\s+[A-Za-z(*[]`, "type", func(b []byte) string { return string(b) })
		// Const and Var: extracted line-by-line to skip block forms (const (), var ())
		constRe := regexp.MustCompile(`^\s*const\s+([A-Za-z_][A-Za-z0-9_]*)\s*=`)
		varRe := regexp.MustCompile(`^\s*var\s+([A-Za-z_][A-Za-z0-9_]*)\s*=`)
		lines := bytes.Split(content, []byte{'\n'})
		for i, line := range lines {
			text := strings.TrimSpace(string(line))
			if strings.HasPrefix(text, "const ") && !strings.Contains(text, "(") {
				if m := constRe.FindSubmatch(line); len(m) > 1 {
					addSymbol("constant", string(m[1]), i+1)
				}
			}
			if strings.HasPrefix(text, "var ") && !strings.Contains(text, "(") {
				if m := varRe.FindSubmatch(line); len(m) > 1 {
					addSymbol("variable", string(m[1]), i+1)
				}
			}
		}

	case ".py":
		// Functions: def name(
		addPatternMatch(`(?m)^\s*def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`, "func", func(b []byte) string { return string(b) })
		// Classes: class Name(
		addPatternMatch(`(?m)^\s*class\s+([A-Za-z_][A-Za-z0-9_]*)\b`, "class", func(b []byte) string { return string(b) })
		// Constants: UPPER_SNAKE_CASE = ... at module level (no indentation)
		addPatternMatch(`(?m)^([A-Z][A-Z0-9_]*)\s*=`, "constant", func(b []byte) string { return string(b) })

	case ".js", ".ts", ".jsx", ".tsx", ".mjs":
		// Functions: function name(
		addPatternMatch(`(?m)\bfunction\s+([A-Za-z_][A-Za-z0-9_$]*)\s*[<(]`, "func", func(b []byte) string { return string(b) })
		// Classes
		addPatternMatch(`(?m)\bclass\s+([A-Za-z_$][a-zA-Z0-9_$]*)\b`, "class", func(b []byte) string { return string(b) })
		// Interfaces (TypeScript)
		addPatternMatch(`(?m)\binterface\s+([A-Za-z_$][a-zA-Z0-9_$]*)\b`, "interface", func(b []byte) string { return string(b) })
		// Type aliases
		addPatternMatch(`(?m)\btype\s+([A-Za-z_$][a-zA-Z0-9_$]*)\s*=`, "type", func(b []byte) string { return string(b) })
		// Exported functions: export default function, export function
		addPatternMatch(`(?m)\bexport\s+(?:default\s+)?function\s+([A-Za-z_$][a-zA-Z0-9_$]*)`, "func", func(b []byte) string { return string(b) })
		// Arrow functions: const name = (params) => or const name = async (params) =>
		addPatternMatch(`(?m)\bconst\s+([A-Za-z_$][a-zA-Z0-9_$]*)\s*=\s*(?:async\s+)?(?:\([^)]*\)|[A-Za-z_$][a-zA-Z0-9_$]*)\s*=>`, "func", func(b []byte) string { return string(b) })
		// Constants: const NAME: Type = ... (typed)
		addPatternMatch(`(?m)\bconst\s+([A-Za-z_$][a-zA-Z0-9_$]*)\s*:\s*[A-Z]`, "constant", func(b []byte) string { return string(b) })
		// Constants: const NAME = value (untyped, non-arrow-function)
		addPatternMatch(`(?m)\bconst\s+([A-Za-z_$][a-zA-Z0-9_$]*)\s*=\s*(?:async\s+)?[^=(]`, "constant", func(b []byte) string { return string(b) })
		// Let/Var declarations
		addPatternMatch(`(?m)\b(?:let|var)\s+([A-Za-z_$][a-zA-Z0-9_$]*)\s*[=:]`, "variable", func(b []byte) string { return string(b) })

	case ".rb":
		addPatternMatch(`(?m)^\s*def\s+(?:self\.)?([A-Za-z_][A-Za-z0-9_!?]*)`, "func", func(b []byte) string { return string(b) })
		addPatternMatch(`^\s*class\s+([A-Za-z_][A-Za-z0-9_:]*)`, "class", func(b []byte) string { return string(b) })

	case ".php":
		addPatternMatch(`^\s*function\s+([A-Za-z_][A-Za-z0-9_]*)`, "func", func(b []byte) string { return string(b) })
		addPatternMatch(`^\s*class\s+([A-Za-z_][A-Za-z0-9_]*)`, "class", func(b []byte) string { return string(b) })

	case ".rs":
		addPatternMatch(`(?m)^\s*fn\s+([A-Za-z_][A-Za-z0-9_]*)`, "func", func(b []byte) string { return string(b) })
		addPatternMatch(`(?m)^\s*struct\s+([A-Za-z_][A-Za-z0-9_]*)`, "struct", func(b []byte) string { return string(b) })
		addPatternMatch(`(?m)^\s*enum\s+([A-Za-z_][A-Za-z0-9_]*)`, "enum", func(b []byte) string { return string(b) })
		addPatternMatch(`(?m)^\s*trait\s+([A-Za-z_][A-Za-z0-9_]*)`, "trait", func(b []byte) string { return string(b) })
		addPatternMatch(`(?m)^\s*impl\s+(?:<[^>]+>\s*)?([A-Za-z_][A-Za-z0-9_]*)`, "impl", func(b []byte) string { return string(b) })
		addPatternMatch(`(?m)^\s*const\s+([A-Za-z_][A-Za-z0-9_]*)`, "constant", func(b []byte) string { return string(b) })

	case ".java":
		// Classes
		addPatternMatch(`\bclass\s+([A-Za-z_][A-Za-z0-9_]*)`, "class", func(b []byte) string { return string(b) })
		// Methods: capture the name that comes right before the opening (
		// Skip keywords like if/for/while/switch/return/new
		addPatternMatch(`(?m)^\s*(?:public|private|protected|static|\s)*\s*([A-Za-z_][A-Za-z0-9_]*)\s*\([^;]*\)\s*(?:throws\s+[A-Za-z_,\s]+)?\{`, "method", func(b []byte) string { return string(b) })
	}

	return out
}

// deduplicateSymbols removes duplicate symbols by name+kind+line.
func deduplicateSymbols(out []Symbol) []Symbol {
	seen := make(map[string]bool)
	deduped := out[:0]
	for _, s := range out {
		key := fmt.Sprintf("%s:%s:%d", s.Name, s.Kind, s.Line)
		if !seen[key] {
			seen[key] = true
			deduped = append(deduped, s)
		}
	}
	return deduped
}

// SearchSymbolFiles returns files whose symbols match any of the provided tokens (case-insensitive).
// For each matching file, only the matching symbols are included.
// Returns empty slice (not nil) if no matches.
func SearchSymbolFiles(idx *SymbolIndex, tokens []string) []FileSymbols {
	tokSet := make(map[string]bool)
	for _, t := range tokens {
		t = strings.ToLower(strings.TrimSpace(t))
		if len(t) >= 3 {
			tokSet[t] = true
		}
	}
	var out []FileSymbols
	for _, fs := range idx.Files {
		var matchingSymbols []Symbol
		for _, s := range fs.Symbols {
			name := strings.ToLower(s.Name)
			for t := range tokSet {
				if strings.Contains(name, t) {
					matchingSymbols = append(matchingSymbols, s)
					break
				}
			}
		}
		if len(matchingSymbols) > 0 {
			sort.Slice(matchingSymbols, func(i, j int) bool {
				return matchingSymbols[i].Line < matchingSymbols[j].Line
			})
			out = append(out, FileSymbols{
				File:    fs.File,
				Symbols: matchingSymbols,
			})
		}
	}
	return out
}
