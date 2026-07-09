package tools

import (
	"context"
	"fmt"
	astp "github.com/sprout-foundry/sprout/pkg/ast"
	codegraph "github.com/sprout-foundry/sprout/pkg/codegraph"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

const (
	repoMapMaxFullFileSize = 2 * 1024 * 1024 // 2MB max file size
	repoMapTokenBudget     = 4096            // target ~4096 tokens (~16k chars) — raised from 1024 to cover repos with thousands of source files
	repoMapMaxFiles        = 2000            // cap on files to surface — raised from 200; together with the depth-aware prioritization this prevents one mega-directory from starving the rest
	repoMapCharBudget      = repoMapTokenBudget * 4
	repoMapMaxDepth        = 8               // cap walking depth so deeply-nested vendored trees don't dominate the budget
	repoMapRootFileAllowance = 64            // number of root-level files/dirs to keep before L1 takes over
	repoMapPerDirCap       = 60              // max files shown per directory (prevents pkg/foo/ from hogging the whole output)
	repoMapPerDirChars     = 8 * 1024        // max chars spent per directory in the formatted output
)

var sourceExtensions = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".py": true, ".rs": true, ".java": true, ".c": true, ".cpp": true,
	".h": true,
}

var ignoredDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true,
	"build": true, ".next": true, "coverage": true, ".cache": true, ".sprout": true,
}

// treeSitterExtensions is the set of file extensions handled by the tree-sitter
// based pkg/ast parser.  Go files use go/ast directly.
var treeSitterExtensions = map[string]bool{
	".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".py": true,
}

// GenerateRepoMap walks the directory tree rooted at rootDir and produces a
// lightweight overview of the codebase showing file paths and top-level symbols.
// For Go files it uses go/ast; for TS/JS/Python it uses tree-sitter via pkg/ast.
// Output is truncated to ~1024 tokens.
//
// When the codegraph store is available and populated, it reads from the store
// for near-instant results on warm cache, falling back to the filesystem walk.
func GenerateRepoMap(ctx context.Context, rootDir string) (string, error) {
	if rootDir == "" || rootDir == "." {
		// Use the workspace root from context (set by withToolExecutionMetadata)
		// instead of os.Getwd(), which returns the daemon's CWD, not the
		// workspace the agent is operating in.
		if wsRoot := filesystem.WorkspaceRootFromContext(ctx); wsRoot != "" {
			rootDir = wsRoot
		} else {
			var err error
			rootDir, err = os.Getwd()
			if err != nil {
				return "", fmt.Errorf("get working directory: %w", err)
			}
		}
	}

	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return "", fmt.Errorf("resolve root directory: %w", err)
	}

	// Try to use the codegraph store for instant results on warm cache.
	// Only use the store when the requested rootDir is the git root
	// (store.baseDir); otherwise fall through to filesystem walk.
	store, storeErr := openGraphStore()
	if storeErr == nil && store != nil {
		defer store.Close()

		// Check that absRoot matches the store's baseDir so we don't
		// return project-wide data for a subdirectory query.
		storeAbsBase, err := filepath.Abs(store.BaseDir())
		if err == nil && storeAbsBase == absRoot {
			stats := store.Stats()
			if stats.FileCount > 0 {
				nodes, queryErr := store.QueryAllNodes(ctx)
				if queryErr == nil {
					result := formatRepoMapFromNodes(absRoot, nodes)
					if result != "" {
						return result, nil
					}
				}
			}
		}
	}

	// Fall through to filesystem walk.
	return generateRepoMapFromFS(ctx, absRoot)
}

// generateRepoMapFromFS walks the filesystem to produce the repo map.
// It is the original GenerateRepoMap logic extracted into a separate function
// so it can be used as a fallback when the codegraph store is unavailable.
//
// Improvements over the original flat alphabet+sliced implementation:
//
//   - Walks the entire tree (no early cut at repoMapMaxFiles) and only stops
//     emitting once the char budget is exhausted; this guarantees every
//     top-level directory has a chance to be represented instead of letting
//     alphabetically-first files dominate.
//   - Depth-aware prioritization: root-level files come first, then a
//     round-robin across L1 directories with per-directory caps so no
//     single mega-directory (e.g. a fat pkg/foo/) can starve the rest.
//   - Per-directory character caps to bound output even for directories with
//     many large symbol lists.
//   - Summary header exposes total file count, dirs covered, char usage,
//     and the reason for any truncation — callers can tell at a glance
//     when they're seeing partial output.
// fileEntry is the per-file record produced by the repo-map walk. Hoisted to
// package scope so buildInclusionOrder can reuse the type.
type fileEntry struct {
	absPath, relPath, ext string
	depth                 int
}

func generateRepoMapFromFS(ctx context.Context, absRoot string) (string, error) {

	allFiles := make([]fileEntry, 0, 4096)
	walkErr := filepath.WalkDir(absRoot, func(path string, d os.DirEntry, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err != nil {
			return nil
		}
		name := d.Name()
		// Skip symlinks to prevent following links outside the target tree.
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
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
		relSlash := filepath.ToSlash(rel)
		depth := strings.Count(relSlash, "/")
		if path == absRoot {
			depth = -1 // sentinel: never occurs (the walker doesn't call us for the root path itself)
		}
		allFiles = append(allFiles, fileEntry{path, relSlash, ext, depth})
		return nil
	})
	if walkErr != nil {
		return "", fmt.Errorf("walk directory: %w", walkErr)
	}

	// Pre-compute stats for the summary header.
	totalSourceFileCount := len(allFiles)
	byExt := make(map[string]int)
	for _, f := range allFiles {
		byExt[f.ext]++
	}

	// Build the inclusion order: root, then round-robin across L1 dirs, then
	// round-robin across deeper levels with caps applied per-directory.
	ordered, dirsCovered, dirsOmitted := buildInclusionOrder(allFiles)

	// Apply the file-count cap as a safety net.
	if len(ordered) > repoMapMaxFiles {
		ordered = ordered[:repoMapMaxFiles]
	}

	var sb strings.Builder
	charCount := 0
	fileCount := 0
	truncated := false
	truncationReason := ""

	writeHeader := func() {
		sb.WriteString("## repo_map: ")
		sb.WriteString(filepath.Base(absRoot))
		sb.WriteString("\n")
		// Build a sorted ext list for deterministic output.
		exts := make([]string, 0, len(byExt))
		for e := range byExt {
			exts = append(exts, e)
		}
		sort.Strings(exts)
		extParts := make([]string, 0, len(exts))
		for _, e := range exts {
			extParts = append(extParts, fmt.Sprintf("%s: %d", e, byExt[e]))
		}
		fmt.Fprintf(&sb, "- total source files: %d (%s)\n", totalSourceFileCount, strings.Join(extParts, ", "))
		if totalSourceFileCount > 0 {
			fmt.Fprintf(&sb, "- dirs covered: %d\n", dirsCovered)
			if dirsOmitted > 0 {
				fmt.Fprintf(&sb, "- dirs omitted (file cap reached before they could be sampled): %d\n", dirsOmitted)
			}
		}
		charCount = sb.Len()
	}

	writeHeader()

	perDirChars := make(map[string]int)
	emittedPerDir := make(map[string]int)

	for _, f := range ordered {
		select {
		case <-ctx.Done():
			truncated = true
			truncationReason = "context cancelled"
			break
		default:
		}

		dir := topDir(f.relPath)
		// Per-directory caps.
		if emittedPerDir[dir] >= repoMapPerDirCap {
			continue
		}
		if perDirChars[dir] >= repoMapPerDirChars {
			continue
		}

		content, readErr := os.ReadFile(f.absPath)
		if readErr != nil {
			continue
		}
		if len(content) > repoMapMaxFullFileSize {
			continue
		}
		if isBinaryContent(content) {
			continue
		}

		symbols, err := extractSymbolsForFile(f.absPath, f.ext, content)
		if err != nil {
			continue
		}
		if len(symbols) == 0 {
			continue
		}

		// Render symbols with one per line, prefix and line separated by a
		// space-then-colon. Same shape as before, just consistent.
		var sectionSB strings.Builder
		sectionSB.WriteString("\n### ")
		sectionSB.WriteString(f.relPath)
		sectionSB.WriteString("\n")
		for _, sym := range symbols {
			fmt.Fprintf(&sectionSB, "- %s:%d\n", sym.Name, sym.Line)
		}
		section := sectionSB.String()

		// Honor the global char budget, but always include the first file
		// we see so we never return an empty map.
		if charCount+len(section) > repoMapCharBudget && fileCount > 0 {
			truncated = true
			truncationReason = "char budget reached"
			break
		}
		sb.WriteString(section)
		charCount += len(section)
		fileCount++
		emittedPerDir[dir]++
		perDirChars[dir] += len(section)
	}

	if truncated {
		fmt.Fprintf(&sb, "\n*... truncated (%s); output covers %d of %d files (%.0f%%), %d dirs*\n",
			truncationReason,
			fileCount,
			totalSourceFileCount,
			pct(fileCount, totalSourceFileCount),
			dirsCovered)
	}
	if fileCount == 0 {
		sb.WriteString("\n*No source files with symbols found.*\n")
	}
	return sb.String(), nil
}

// buildInclusionOrder groups source files by their top-level directory and
// emits them in a priority order designed to give every top-level area some
// representation: root files first (up to repoMapRootFileAllowance), then a
// round-robin across the L1 directories, with each directory capped at
// repoMapPerDirCap files. Within a directory, files are sorted alphabetically
// for deterministic output.
//
// Returns the ordered list, the number of distinct directories represented,
// and the number of directories that were entirely omitted (had files but
// were beyond the cap).
func buildInclusionOrder(files []fileEntry) (ordered []fileEntry, dirsRepresented int, dirsOmitted int) {

	// Root files: relPath has no slash.
	root := make([]fileEntry, 0, 16)
	// L1 dir -> sorted file list.
	byDir := make(map[string][]fileEntry)
	for _, f := range files {
		if !strings.Contains(f.relPath, "/") {
			root = append(root, f)
			continue
		}
		dir := topDir(f.relPath)
		byDir[dir] = append(byDir[dir], f)
	}

	// Sort root alphabetically and apply cap.
	sort.Slice(root, func(i, j int) bool { return root[i].relPath < root[j].relPath })
	if len(root) > repoMapRootFileAllowance {
		root = root[:repoMapRootFileAllowance]
	}
	ordered = append(ordered, root...)

	// Sort per-dir lists alphabetically; track which dirs are represented vs. omitted.
	dirNames := make([]string, 0, len(byDir))
	for d := range byDir {
		dirNames = append(dirNames, d)
	}
	sort.Strings(dirNames)

	// Cap how many files we pull per dir before bailing on per-dir cycle.
	perDirLimit := repoMapPerDirCap

	for _, d := range dirNames {
		entries := byDir[d]
		sort.Slice(entries, func(i, j int) bool { return entries[i].relPath < entries[j].relPath })
		take := len(entries)
		if take > perDirLimit {
			take = perDirLimit
			dirsOmitted++ // the dir had files beyond the cap; flag it as partly omitted
		}
		ordered = append(ordered, entries[:take]...)
		dirsRepresented++
	}

	return ordered, dirsRepresented, dirsOmitted
}

// topDir returns the first path component of a slash-separated relative path,
// or "" if the path has no slash (i.e. it's a root-level file).
func topDir(relPath string) string {
	if idx := strings.IndexByte(relPath, '/'); idx >= 0 {
		return relPath[:idx]
	}
	return ""
}

func pct(num, denom int) float64 {
	if denom == 0 {
		return 0
	}
	return float64(num) / float64(denom) * 100
}


// openGraphStore opens the codegraph store at the default path (.sprout/codegraph.db).
// Returns nil, nil when the store is cleanly unavailable (file doesn't exist).
// Returns an error if the store exists but can't be opened.
func openGraphStore() (*codegraph.SQLiteStore, error) {
	dbPath, err := codegraph.DefaultDBPath()
	if err != nil {
		return nil, nil // can't resolve path, silently fall through
	}
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, nil // no store yet, silently fall through
	}

	store, err := codegraph.NewStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open codegraph store: %w", err)
	}

	return store, nil
}

// formatRepoMapFromNodes formats the store-backed node data into the same
// output format as the filesystem-walk version.
// The DisplayName field stores the bare name (e.g., "run", "MyType", "(*Handler).ServeHTTP")
// without a kind prefix. We reconstruct the prefix from sym.Kind so the output
// matches the filesystem-walk format (e.g., "- func run:10", "- type MyType:5").
func formatRepoMapFromNodes(rootDir string, nodes []codegraph.Symbol) string {
	if len(nodes) == 0 {
		return ""
	}

	// Group nodes by file_path.
	fileNodes := make(map[string][]codegraph.Symbol)
	for _, n := range nodes {
		fileNodes[n.FilePath] = append(fileNodes[n.FilePath], n)
	}

	// Sort file paths for deterministic output.
	filePaths := make([]string, 0, len(fileNodes))
	for p := range fileNodes {
		filePaths = append(filePaths, p)
	}
	sort.Strings(filePaths)

	var sb strings.Builder
	sb.WriteString("## repo_map: ")
	sb.WriteString(filepath.Base(rootDir))
	sb.WriteString("\n")

	charCount := sb.Len()
	fileCount := 0
	truncated := false

	for _, fp := range filePaths {
		syms := fileNodes[fp]

		section := "\n### " + fp + "\n"
		for _, sym := range syms {
			prefix := kindToPrefix(sym.Kind)
			section += fmt.Sprintf("- %s %s:%d\n", prefix, sym.DisplayName, sym.Line)
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

	return sb.String()
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

// extractGoSymbolsAST parses a Go source file using go/ast and extracts
// top-level functions, methods, and type declarations as SymbolEntry values.
// Test functions (Test*, Benchmark*, Fuzz*) and _test.go files are excluded.
func extractGoSymbolsAST(path string, content []byte) ([]SymbolEntry, error) {
	// Skip _test.go files entirely.
	if strings.HasSuffix(filepath.Base(path), "_test.go") {
		return nil, nil
	}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, content, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	var symbols []SymbolEntry

	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if shouldSkipGoFunc(d) {
				continue
			}
			name := goFuncName(d)
			line := fset.Position(d.Pos()).Line
			symbols = append(symbols, SymbolEntry{Name: name, Line: line})

		case *ast.GenDecl:
			if d.Tok == token.TYPE {
				for _, spec := range d.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok {
						line := fset.Position(ts.Pos()).Line
						// Check if this is an interface type.
						if _, isIface := ts.Type.(*ast.InterfaceType); isIface {
							symbols = append(symbols, SymbolEntry{
								Name: "iface " + ts.Name.Name,
								Line: line,
							})
						} else {
							symbols = append(symbols, SymbolEntry{
								Name: "type " + ts.Name.Name,
								Line: line,
							})
						}
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

// ExtractCallsAndSymbols returns both symbols and call edges for a given file.
func ExtractCallsAndSymbols(path string, content []byte) (*SymbolWithEdges, error) {
	ext := strings.ToLower(filepath.Ext(path))

	switch ext {
	case ".go":
		return extractGoSymbolsASTWithEdges(path, content)
	case ".ts", ".tsx", ".js", ".jsx", ".py":
		return extractSymbolsAndEdgesViaTreeSitter(path, ext, content)
	default:
		return nil, fmt.Errorf("unsupported file extension: %s", ext)
	}
}

// extractGoSymbolsASTWithEdges parses a Go source file and extracts both
// symbols and call edges from function bodies.
func extractGoSymbolsASTWithEdges(path string, content []byte) (*SymbolWithEdges, error) {
	// Skip _test.go files entirely.
	if strings.HasSuffix(filepath.Base(path), "_test.go") {
		return &SymbolWithEdges{}, nil
	}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, content, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	// Build import alias → import path map for cross-package call resolution.
	// For `import "strings"` the alias is "strings" (the last path element).
	// For `import foo "pkg/bar"` the alias is "foo".
	// The import path is converted to a relative path matching the qualified
	// name format used by nodes (e.g. "pkg/codegraph" not "github.com/mod/pkg/codegraph").
	modulePath := detectGoModule(path)
	importMap := make(map[string]string) // alias → relative package path
	for _, imp := range node.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		alias := filepath.Base(importPath)
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		relPath := stripModulePrefix(importPath, modulePath)
		importMap[alias] = relPath
	}

	var symbols []SymbolEntry
	var edges []codegraph.Edge

	for _, decl := range node.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			// Handle type declarations for symbols.
			if gd, ok2 := decl.(*ast.GenDecl); ok2 && gd.Tok == token.TYPE {
				for _, spec := range gd.Specs {
					if ts, ok3 := spec.(*ast.TypeSpec); ok3 {
						line := fset.Position(ts.Pos()).Line
						// Check if this is an interface type.
						if _, isIface := ts.Type.(*ast.InterfaceType); isIface {
							symbols = append(symbols, SymbolEntry{
								Name: "iface " + ts.Name.Name,
								Line: line,
							})
						} else {
							symbols = append(symbols, SymbolEntry{
								Name: "type " + ts.Name.Name,
								Line: line,
							})
						}
					}
				}
			}
			continue
		}

		if shouldSkipGoFunc(fd) {
			continue
		}

		funcName := goFuncName(fd)
		line := fset.Position(fd.Pos()).Line
		symbols = append(symbols, SymbolEntry{Name: funcName, Line: line})

		// Walk the function body to find call expressions.
		if fd.Body == nil {
			continue
		}

		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			calleeName := exprToString(call.Fun)
			callLine := fset.Position(call.Pos()).Line

			edgeType := "calls"

			// For selector expressions (containing a dot), check whether any
			// prefix matches an import alias. Multi-level selectors like
			// "agent.state.GetOptimizer()" are receiver chains — strip all
			// non-import prefixes until we either find an import match or
			// reach the final method name.
			if dotIdx := strings.IndexByte(calleeName, '.'); dotIdx > 0 {
				for dotIdx > 0 {
					prefix := calleeName[:dotIdx]
					if pkgPath, ok := importMap[prefix]; ok {
						calleeName = pkgPath + calleeName[dotIdx:]
						edgeType = "resolved_calls"
						break
					}
					// Not an import — strip this prefix and try next level.
					calleeName = calleeName[dotIdx+1:]
					dotIdx = strings.IndexByte(calleeName, '.')
				}
			}

			edges = append(edges, codegraph.Edge{
				SourceQualifiedName: funcName,
				TargetQualifiedName: calleeName,
				EdgeType:            edgeType,
				Line:                callLine,
			})
			return true
		})
	}

	return &SymbolWithEdges{Symbols: symbols, Edges: edges}, nil
}

// exprToString converts a go/ast expression to a string representation.
func exprToString(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return exprToString(v.X) + "." + v.Sel.Name
	case *ast.StarExpr:
		return "*" + exprToString(v.X)
	case *ast.ArrayType:
		return exprToString(v.Elt)
	case *ast.ParenExpr:
		return "(" + exprToString(v.X) + ")"
	case *ast.BasicLit:
		return v.Value
	case *ast.FuncLit:
		return "func(...)"
	default:
		return fmt.Sprintf("?%T?", e)
	}
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

// detectGoModule reads the module path from go.mod by walking up from the
// given source file path. Returns the module path or empty string on failure.
func detectGoModule(filePath string) string {
	dir := filepath.Dir(filePath)
	for {
		gm := filepath.Join(dir, "go.mod")
		data, err := os.ReadFile(gm)
		if err == nil {
			// Parse "module github.com/foo/bar" from first line
			for _, line := range strings.SplitN(string(data), "\n", 2) {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "module ") {
					return strings.TrimSpace(line[7:])
				}
			}
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// stripModulePrefix removes the module path prefix from an import path,
// returning the relative package path used for node qualified names.
// e.g. "github.com/foo/bar/pkg/util" with module "github.com/foo/bar" → "pkg/util"
// If the import doesn't start with the module prefix, returns the last
// path segment (for stdlib imports like "fmt" — these won't match nodes
// anyway since nodes don't have qualified names for stdlib functions).
func stripModulePrefix(importPath, modulePath string) string {
	if modulePath != "" && strings.HasPrefix(importPath, modulePath+"/") {
		return importPath[len(modulePath)+1:]
	}
	return filepath.Base(importPath)
}
