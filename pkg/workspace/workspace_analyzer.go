package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/alantheprice/ledit/pkg/config"
)

// generateFileHash creates a SHA256 hash of the file content.
func generateFileHash(content string) string {
	hasher := sha256.New()
	hasher.Write([]byte(content))
	return hex.EncodeToString(hasher.Sum(nil))
}

// getSummary generates a brief syntactic overview locally (no remote LLM).
func getSummary(content, filename string, cfg *config.Config) (string, string, string, error) {
	// Check if the file is a text file
	if !isTextFile(filename) {
		return "", "", "", fmt.Errorf("file type not supported for analysis")
	}

	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".go":
		return analyzeGoFile(content, filename)
	case ".py":
		return analyzePythonFile(content, filename), extractPythonExports(content), extractPythonReferences(content), nil
	case ".ts", ".tsx", ".js", ".jsx":
		return analyzeJSLikeFile(content, filename), extractJSExports(content), extractJSReferences(content), nil
	case ".json":
		return analyzeJSONFile(content, filename), "", "", nil
	case ".yaml", ".yml":
		return analyzeYAMLFile(content, filename), "", "", nil
	case ".md":
		return analyzeMarkdownFile(content, filename), "", "", nil
	case ".sh", ".bash":
		return analyzeShellFile(content, filename), extractShellExports(content), extractShellReferences(content), nil
	case ".html", ".htm":
		return analyzeHTMLFile(content, filename), "", "", nil
	case ".css":
		return analyzeCSSFile(content, filename), "", "", nil
	default:
		lines := strings.Split(content, "\n")
		nonEmpty := 0
		for _, l := range lines {
			if strings.TrimSpace(l) != "" {
				nonEmpty++
			}
		}
		summary := fmt.Sprintf("%s textual file with %d lines (%d non-empty).", ext, len(lines), nonEmpty)
		return summary, "", "", nil
	}
}

// isTextFile checks if a file has a common text-based extension.
func isTextFile(filename string) bool {
	textExtensions := []string{".txt", ".go", ".py", ".js", ".jsx", ".java", ".c", ".cpp", ".h", ".hpp", ".md", ".json", ".yaml", ".yml", ".sh", ".bash", ".sql", ".html", ".css", ".xml", ".csv", ".ts", ".tsx", ".php", ".rb", ".swift", ".kt", ".scala", ".rs", ".dart", ".perl", ".pl", ".pm", ".lua", ".vim", ".toml"}
	ext := filepath.Ext(filename)
	for _, te := range textExtensions {
		if ext == te {
			return true
		}
	}
	return false
}

// --- Language-specific analyzers (local only) ---

func analyzeGoFile(content, filename string) (string, string, string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, content, parser.ParseComments)
	if err != nil || file == nil {
		lines := strings.Split(content, "\n")
		return fmt.Sprintf("Go file in package '%s' (%d lines).", filePackageSafe(file), len(lines)), "", "", nil
	}

	pkg := file.Name.Name
	var imports []string
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, "\"")
		imports = append(imports, path)
	}
	sort.Strings(imports)

	exported := []string{}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Name != nil && ast.IsExported(d.Name.Name) {
				name := d.Name.Name
				if d.Recv != nil && len(d.Recv.List) > 0 {
					recvType := nodeToString(d.Recv.List[0].Type)
					name = fmt.Sprintf("(%s).%s", recvType, name)
				}
				exported = append(exported, "func "+name)
			}
		case *ast.GenDecl:
			switch d.Tok {
			case token.TYPE:
				for _, s := range d.Specs {
					if ts, ok := s.(*ast.TypeSpec); ok {
						if ast.IsExported(ts.Name.Name) {
							exported = append(exported, "type "+ts.Name.Name)
						}
					}
				}
			case token.VAR:
				for _, s := range d.Specs {
					if vs, ok := s.(*ast.ValueSpec); ok {
						for _, n := range vs.Names {
							if ast.IsExported(n.Name) {
								exported = append(exported, "var "+n.Name)
							}
						}
					}
				}
			case token.CONST:
				for _, s := range d.Specs {
					if vs, ok := s.(*ast.ValueSpec); ok {
						for _, n := range vs.Names {
							if ast.IsExported(n.Name) {
								exported = append(exported, "const "+n.Name)
							}
						}
					}
				}
			}
		}
	}
	sort.Strings(exported)

	summary := fmt.Sprintf("Go file in package '%s' with %d imports and %d exported symbols.", pkg, len(imports), len(exported))
	exports := strings.Join(exported, "; ")
	refs := strings.Join(imports, ", ")
	return summary, exports, refs, nil
}

func filePackageSafe(f *ast.File) string {
	if f == nil || f.Name == nil {
		return "unknown"
	}
	return f.Name.Name
}

func nodeToString(n ast.Expr) string {
	switch x := n.(type) {
	case *ast.StarExpr:
		return "*" + nodeToString(x.X)
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return nodeToString(x.X) + "." + x.Sel.Name
	case *ast.IndexExpr:
		return nodeToString(x.X) + "[T]"
	case *ast.ArrayType:
		return "[]" + nodeToString(x.Elt)
	case *ast.MapType:
		return "map[" + nodeToString(x.Key) + "]" + nodeToString(x.Value)
	default:
		return "T"
	}
}

// Python (regex-based best-effort)
func analyzePythonFile(content, filename string) string {
	lines := strings.Split(content, "\n")
	classes := 0
	funcs := 0
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "class ") {
			classes++
		} else if strings.HasPrefix(t, "def ") {
			funcs++
		}
	}
	return fmt.Sprintf("Python module with %d classes and %d functions.", classes, funcs)
}

func extractPythonExports(content string) string {
	reDef := regexp.MustCompile(`(?m)^\s*def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	reCls := regexp.MustCompile(`(?m)^\s*class\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	names := map[string]bool{}
	for _, m := range reDef.FindAllStringSubmatch(content, -1) {
		if len(m) > 1 && !strings.HasPrefix(m[1], "_") {
			names["def "+m[1]] = true
		}
	}
	for _, m := range reCls.FindAllStringSubmatch(content, -1) {
		if len(m) > 1 && !strings.HasPrefix(m[1], "_") {
			names["class "+m[1]] = true
		}
	}
	var out []string
	for k := range names {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, "; ")
}

func extractPythonReferences(content string) string {
	re := regexp.MustCompile(`(?m)^\s*(?:from\s+([\w\.]+)\s+import|import\s+([\w\.]+))`)
	set := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(content, -1) {
		for i := 1; i < len(m); i++ {
			if m[i] != "" {
				set[m[i]] = true
			}
		}
	}
	var out []string
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

// JS/TS (regex-based best-effort)
func analyzeJSLikeFile(content, filename string) string {
	imports := regexp.MustCompile(`(?m)^\s*import\s+|require\(`).FindAllStringIndex(content, -1)
	exports := regexp.MustCompile(`(?m)^\s*export\s+`).FindAllStringIndex(content, -1)
	funcs := regexp.MustCompile(`(?m)^\s*(?:export\s+)?function\s+`).FindAllStringIndex(content, -1)
	return fmt.Sprintf("JS/TS file with %d imports, %d export statements, %d functions.", len(imports), len(exports), len(funcs))
}

func extractJSExports(content string) string {
	names := []string{}
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`(?m)^\s*export\s+function\s+([A-Za-z_][A-Za-z0-9_]*)`),
		regexp.MustCompile(`(?m)^\s*export\s+class\s+([A-Za-z_][A-Za-z0-9_]*)`),
		regexp.MustCompile(`(?m)^\s*export\s+(?:const|let|var)\s+([A-Za-z_][A-Za-z0-9_]*)`),
		regexp.MustCompile(`(?m)^\s*module\.exports\s*=\s*([A-Za-z_][A-Za-z0-9_]*)`),
		regexp.MustCompile(`(?m)^\s*exports\.([A-Za-z_][A-Za-z0-9_]*)\s*=`),
	} {
		ms := re.FindAllStringSubmatch(content, -1)
		for _, m := range ms {
			if len(m) > 1 {
				names = append(names, m[1])
			}
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	for i, n := range names {
		names[i] = "export " + n
	}
	return strings.Join(names, "; ")
}

func extractJSReferences(content string) string {
	set := map[string]bool{}
	re1 := regexp.MustCompile(`(?m)^\s*import\s+.*?from\s+['\"]([^'\"]+)['\"]`)
	re2 := regexp.MustCompile(`require\(['\"]([^'\"]+)['\"]\)`)
	for _, m := range re1.FindAllStringSubmatch(content, -1) {
		if len(m) > 1 {
			set[m[1]] = true
		}
	}
	for _, m := range re2.FindAllStringSubmatch(content, -1) {
		if len(m) > 1 {
			set[m[1]] = true
		}
	}
	var out []string
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

func analyzeJSONFile(content, filename string) string {
	re := regexp.MustCompile(`(?m)^[\s\t]*\"([^\"]+)\"\s*:`)
	keys := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(content, -1) {
		if len(m) > 1 {
			keys[m[1]] = true
		}
	}
	var k []string
	for key := range keys {
		k = append(k, key)
	}
	sort.Strings(k)
	if len(k) > 0 {
		return fmt.Sprintf("JSON with top-level keys: %s.", strings.Join(k, ", "))
	}
	lines := strings.Split(content, "\n")
	return fmt.Sprintf("JSON with %d lines.", len(lines))
}

func analyzeYAMLFile(content, filename string) string {
	re := regexp.MustCompile(`(?m)^([A-Za-z0-9_\-\.]+):\s`)
	keys := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(content, -1) {
		if len(m) > 1 {
			keys[m[1]] = true
		}
	}
	var out []string
	for k := range keys {
		out = append(out, k)
	}
	sort.Strings(out)
	if len(out) == 0 {
		lines := strings.Split(content, "\n")
		return fmt.Sprintf("YAML with %d lines.", len(lines))
	}
	return fmt.Sprintf("YAML with top-level keys: %s.", strings.Join(out, ", "))
}

func analyzeMarkdownFile(content, filename string) string {
	re := regexp.MustCompile(`(?m)^(#\s+.+|##\s+.+|###\s+.+)`)
	hs := []string{}
	for _, m := range re.FindAllString(content, -1) {
		hs = append(hs, strings.TrimSpace(m))
	}
	if len(hs) == 0 {
		lines := strings.Split(content, "\n")
		return fmt.Sprintf("Markdown file with %d lines.", len(lines))
	}
	if len(hs) > 5 {
		hs = hs[:5]
	}
	return "Markdown headings: " + strings.Join(hs, "; ")
}

func analyzeShellFile(content, filename string) string {
	funcs := regexp.MustCompile(`(?m)^(?:function\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*\(\)\s*\{`).FindAllStringSubmatch(content, -1)
	names := []string{}
	for _, m := range funcs {
		if len(m) > 1 {
			names = append(names, m[1])
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "Shell script."
	}
	return "Shell script with functions: " + strings.Join(names, ", ")
}

func extractShellExports(content string) string {
	funcs := regexp.MustCompile(`(?m)^(?:function\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*\(\)\s*\{`).FindAllStringSubmatch(content, -1)
	names := []string{}
	for _, m := range funcs {
		if len(m) > 1 {
			names = append(names, m[1])
		}
	}
	sort.Strings(names)
	for i, n := range names {
		names[i] = "func " + n
	}
	return strings.Join(names, "; ")
}

func extractShellReferences(content string) string {
	re := regexp.MustCompile(`(?m)^\s*(?:source|\.)\s+([^\s#]+)`)
	set := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(content, -1) {
		if len(m) > 1 {
			set[m[1]] = true
		}
	}
	var out []string
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

func analyzeHTMLFile(content, filename string) string {
	tags := []string{"div", "span", "script", "link", "meta", "img", "a", "section", "article", "header", "footer", "style"}
	counts := map[string]int{}
	for _, t := range tags {
		re := regexp.MustCompile("(?i)<" + t + "\\b")
		counts[t] = len(re.FindAllStringIndex(content, -1))
	}
	var parts []string
	for _, t := range tags {
		if counts[t] > 0 {
			parts = append(parts, fmt.Sprintf("%s:%d", t, counts[t]))
		}
	}
	if len(parts) == 0 {
		return "HTML file."
	}
	if len(parts) > 6 {
		parts = parts[:6]
	}
	return "HTML tags: " + strings.Join(parts, ", ")
}

func analyzeCSSFile(content, filename string) string {
	rules := regexp.MustCompile(`\{[^}]*\}`).FindAllStringIndex(content, -1)
	classes := regexp.MustCompile(`\.[A-Za-z_][A-Za-z0-9_-]*`).FindAllStringIndex(content, -1)
	ids := regexp.MustCompile(`#([A-Za-z_][A-Za-z0-9_-]*)`).FindAllStringIndex(content, -1)
	return fmt.Sprintf("CSS with %d rules, %d classes, %d ids.", len(rules), len(classes), len(ids))
}
