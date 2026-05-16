package tools

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	repoMapMaxFileSize = 32 * 1024 // 32KB per file (non-Go files only)
	repoMapTokenBudget = 1024       // target ~1024 tokens
	repoMapMaxFiles    = 200        // max files to include
	repoMapCharBudget  = repoMapTokenBudget * 4
)

// Regex patterns for symbol extraction (top-level declarations).
// Used for non-Go languages and as a fallback for Go files that fail AST parsing.
var (
	goFuncRe      = regexp.MustCompile(`^\s*func\s+(?:\([^)]*\)\s+)?(\w+)`)
	goTypeRe      = regexp.MustCompile(`^\s*type\s+(\w+)\s+(struct|interface)\b`)
	tsFuncRe      = regexp.MustCompile(`^\s*(?:export\s+(?:default\s+)?)?(?:async\s+)?function\s+(\w+)`)
	tsClassRe     = regexp.MustCompile(`^\s*(?:export\s+(?:default\s+)?)?class\s+(\w+)`)
	tsInterfaceRe = regexp.MustCompile(`^\s*(?:export\s+)?interface\s+(\w+)`)
	tsTypeRe      = regexp.MustCompile(`^\s*(?:export\s+)?type\s+(\w+)`)
	tsConstRe     = regexp.MustCompile(`^\s*(?:export\s+)?(?:const|let|var)\s+(\w+)`)
	pyFuncRe      = regexp.MustCompile(`^\s*(?:async\s+)?def\s+(\w+)`)
	pyClassRe     = regexp.MustCompile(`^\s*class\s+(\w+)`)
)

var sourceExtensions = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".py": true, ".rs": true, ".java": true, ".c": true, ".cpp": true,
	".h": true, ".css": true, ".html": true,
}

var ignoredDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true,
	"build": true, ".next": true, "coverage": true, ".cache": true, ".sprout": true,
}

// GenerateRepoMap walks the directory tree rooted at rootDir and produces a
// lightweight, AST-like overview of the codebase.  For Go files it uses the
// go/ast parser for accurate symbol extraction.  For other languages it falls
// back to simple regex patterns.  Output is truncated to ~1024 tokens.
func GenerateRepoMap(ctx context.Context, rootDir string) (string, error) {
	if rootDir == "" || rootDir == "." {
		var err error
		rootDir, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get working directory: %w", err)
		}
	}

	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return "", fmt.Errorf("resolve root directory: %w", err)
	}

	type fileEntry struct {
		absPath, relPath, ext string
	}

	var files []fileEntry
	err = filepath.WalkDir(absRoot, func(path string, d os.DirEntry, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if ignoredDirs[name] {
				return filepath.SkipDir
			}
			if path != absRoot && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(name))
		if !sourceExtensions[ext] {
			return nil
		}
		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			return nil
		}
		files = append(files, fileEntry{path, filepath.ToSlash(rel), ext})
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("walk directory: %w", err)
	}

	sort.Slice(files, func(i, j int) bool { return files[i].relPath < files[j].relPath })
	if len(files) > repoMapMaxFiles {
		files = files[:repoMapMaxFiles]
	}

	var sb strings.Builder
	sb.WriteString("## repo_map: ")
	sb.WriteString(filepath.Base(absRoot))
	sb.WriteString("\n")

	charCount := sb.Len()
	fileCount := 0
	truncated := false

	for _, f := range files {
		select {
		case <-ctx.Done():
			return sb.String(), nil
		default:
		}

		// Go files: read full content (AST parser needs complete file).
		// Other files: truncate to 32KB (regex only needs a sample).
		var content []byte
		var readErr error
		if f.ext == ".go" {
			content, readErr = os.ReadFile(f.absPath)
		} else {
			content, readErr = os.ReadFile(f.absPath)
			if readErr == nil && len(content) > repoMapMaxFileSize {
				content = content[:repoMapMaxFileSize]
			}
		}
		if readErr != nil {
			continue
		}
		if isBinaryContent(content) {
			continue
		}

		symbols, err := extractSymbolsForFile(f.absPath, f.ext, content)
		if err != nil {
			// If extraction fails (e.g., AST parse error), skip the file.
			continue
		}
		if len(symbols) == 0 {
			continue
		}

		section := "\n### " + f.relPath + "\n"
		for _, sym := range symbols {
			section += fmt.Sprintf("- %s:%d\n", sym.Name, sym.Line)
		}
		if charCount+len(section) > repoMapCharBudget && fileCount > 0 {
			truncated = true
			break
		}
		sb.WriteString(section)
		charCount += len(section)
		fileCount++
	}

	if truncated {
		sb.WriteString("\n*... truncated (token budget reached)*\n")
	}
	if fileCount == 0 {
		sb.WriteString("\n*No source files with symbols found.*\n")
	}
	return sb.String(), nil
}

// symbolEntry pairs a symbol name with its 1-based line number.
type symbolEntry struct {
	Name string
	Line int
}

// extractSymbolsForFile extracts symbols from a file using the appropriate
// method: AST for Go, regex for other languages.
func extractSymbolsForFile(path string, ext string, content []byte) ([]symbolEntry, error) {
	if ext == ".go" {
		symbols, err := extractGoSymbolsAST(path)
		if err != nil {
			// Fall back to regex if AST fails (e.g., syntax errors, build tags).
			return extractSymbolsByRegex(ext, string(content)), nil
		}
		return symbols, nil
	}
	return extractSymbolsByRegex(ext, string(content)), nil
}

// extractGoSymbolsAST parses a Go source file using go/ast and extracts
// top-level functions, methods, and type declarations as symbolEntry values.
// Test functions (Test*, Benchmark*, Fuzz*) and _test.go files are excluded.
func extractGoSymbolsAST(path string) ([]symbolEntry, error) {
	// Skip _test.go files entirely.
	if strings.HasSuffix(filepath.Base(path), "_test.go") {
		return nil, nil
	}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	var symbols []symbolEntry

	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if shouldSkipGoFunc(d) {
				continue
			}
			name := goFuncName(d)
			line := fset.Position(d.Pos()).Line
			symbols = append(symbols, symbolEntry{Name: name, Line: line})

		case *ast.GenDecl:
			if d.Tok == token.TYPE {
				for _, spec := range d.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok {
						line := fset.Position(ts.Pos()).Line
						symbols = append(symbols, symbolEntry{
							Name: "type " + ts.Name.Name,
							Line: line,
						})
					}
				}
			}
		}
	}

	return symbols, nil
}

// shouldSkipGoFunc returns true if the function should be excluded from the
// repo map (test functions, benchmark functions, fuzz tests, blank identifiers).
func shouldSkipGoFunc(d *ast.FuncDecl) bool {
	if d.Name == nil || d.Name.Name == "_" {
		return true
	}
	name := d.Name.Name
	return strings.HasPrefix(name, "Test") ||
		strings.HasPrefix(name, "Benchmark") ||
		strings.HasPrefix(name, "Fuzz")
}

// goFuncName returns a display name for a Go function declaration.
// For methods: "(*Receiver).Method" or "(Receiver).Method"
// For functions: "func funcName"
func goFuncName(d *ast.FuncDecl) string {
	if d.Recv != nil && len(d.Recv.List) > 0 {
		recv := d.Recv.List[0]
		recvName := goRecvType(recv.Type)
		ptr := ""
		if se, ok := recv.Type.(*ast.StarExpr); ok {
			ptr = "*"
			recvName = goRecvType(se.X)
		}
		return fmt.Sprintf("func (%s%s).%s", ptr, recvName, d.Name.Name)
	}
	return "func " + d.Name.Name
}

// goRecvType extracts the type name from an expression node.
func goRecvType(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return goRecvType(e.X) + "." + e.Sel.Name
	default:
		return "?"
	}
}

// extractSymbolsByRegex extracts symbols using regex patterns.
// This is used for non-Go languages and as a fallback for Go files.
func extractSymbolsByRegex(ext string, content string) []symbolEntry {
	var symbols []symbolEntry
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lineNum := i + 1 // 1-based
		switch ext {
		case ".go":
			symbols = appendSymbolEntries(symbols, extractGoSymbolsByRegex(line, lineNum))
		case ".ts", ".tsx", ".js", ".jsx":
			symbols = appendSymbolEntries(symbols, extractTSSymbols(line, lineNum))
		case ".py":
			symbols = appendSymbolEntries(symbols, extractPySymbols(line, lineNum))
		}
	}
	return symbols
}

// extractGoSymbolsByRegex is the regex-based fallback for Go files when AST parsing fails.
func extractGoSymbolsByRegex(line string, lineNum int) []symbolEntry {
	if m := goFuncRe.FindStringSubmatch(line); m != nil {
		return []symbolEntry{{"func " + m[1], lineNum}}
	}
	if m := goTypeRe.FindStringSubmatch(line); m != nil {
		return []symbolEntry{{"type " + m[1] + " " + m[2], lineNum}}
	}
	return nil
}

func extractTSSymbols(line string, lineNum int) []symbolEntry {
	t := strings.TrimSpace(line)
	if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "*") {
		return nil
	}
	if m := tsFuncRe.FindStringSubmatch(line); m != nil {
		return []symbolEntry{{"function " + m[1], lineNum}}
	}
	if m := tsClassRe.FindStringSubmatch(line); m != nil {
		return []symbolEntry{{"class " + m[1], lineNum}}
	}
	if m := tsInterfaceRe.FindStringSubmatch(line); m != nil {
		return []symbolEntry{{"interface " + m[1], lineNum}}
	}
	if m := tsTypeRe.FindStringSubmatch(line); m != nil {
		return []symbolEntry{{"type " + m[1], lineNum}}
	}
	if m := tsConstRe.FindStringSubmatch(line); m != nil {
		if strings.HasPrefix(t, "export") || strings.HasPrefix(t, "const ") ||
			strings.HasPrefix(t, "let ") || strings.HasPrefix(t, "var ") {
			return []symbolEntry{{"const " + m[1], lineNum}}
		}
	}
	return nil
}

func extractPySymbols(line string, lineNum int) []symbolEntry {
	t := strings.TrimSpace(line)
	if strings.HasPrefix(t, "#") {
		return nil
	}
	if m := pyFuncRe.FindStringSubmatch(line); m != nil {
		return []symbolEntry{{"def " + m[1], lineNum}}
	}
	if m := pyClassRe.FindStringSubmatch(line); m != nil {
		return []symbolEntry{{"class " + m[1], lineNum}}
	}
	return nil
}

func appendSymbolEntries(base []symbolEntry, newSyms []symbolEntry) []symbolEntry {
	for _, s := range newSyms {
		found := false
		for _, e := range base {
			if e.Name == s.Name {
				found = true
				break
			}
		}
		if !found {
			base = append(base, s)
		}
	}
	return base
}
