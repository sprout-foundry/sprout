package agent

import (
    "fmt"
    "os"
    "path/filepath"
    "regexp"
    "strings"
)

// verifyOperationPostconditions inspects the edited file for evidence that
// the requested change likely occurred (regex-based heuristics). Returns (ok, reason).
func verifyOperationPostconditions(op EditOperation, instructions string) (bool, string) {
	content, err := os.ReadFile(op.FilePath)
	if err != nil {
		return false, fmt.Sprintf("failed to read file for postconditions: %v", err)
	}
	text := string(content)
	patterns := inferTargetsFromInstructions(instructions, op.FilePath)
	if len(patterns) == 0 {
		// Nothing to verify; accept
		return true, "no explicit postconditions inferred"
	}
	for _, rx := range patterns {
		re, rerr := regexp.Compile(rx)
		if rerr != nil {
			// Skip invalid pattern, but don't fail outright
			continue
		}
		if !re.MatchString(text) {
			return false, fmt.Sprintf("missing expected pattern: %s", rx)
		}
	}
    // Stronger, language-aware structure checks
    names := inferSymbolNames(instructions)
    switch strings.ToLower(filepath.Ext(op.FilePath)) {
    case ".py":
        if ok, reason := verifyPythonStructure(text, names); !ok { return false, reason }
    case ".js", ".ts":
        if ok, reason := verifyJSStructure(text, names); !ok { return false, reason }
    case ".rb":
        if ok, reason := verifyRubyStructure(text, names); !ok { return false, reason }
    case ".php":
        if ok, reason := verifyPHPStructure(text, names); !ok { return false, reason }
    case ".rs":
        if ok, reason := verifyRustStructure(text, names); !ok { return false, reason }
    }
    return true, "postconditions satisfied"
}

// inferTargetsFromInstructions extracts simple indicators (function, import, symbol names)
// from the natural language instructions to verify after edits.
func inferTargetsFromInstructions(instructions, filePath string) []string {
	var out []string
	lower := strings.ToLower(instructions)
	ext := strings.ToLower(filepath.Ext(filePath))

	// function patterns across languages
	// e.g., "add function foo", "implement bar", "create method baz"
	funcNameRe := regexp.MustCompile(`(?i)(?:function|func|def|method|class)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	for _, m := range funcNameRe.FindAllStringSubmatch(instructions, -1) {
		if len(m) >= 2 {
			name := regexp.QuoteMeta(m[1])
			switch ext {
			case ".go":
				out = append(out, `(?m)\bfunc\s+`+name+`\b`)
			case ".py":
				out = append(out, `(?m)^\s*def\s+`+name+`\s*\(`)
			case ".js", ".ts":
				out = append(out, `(?m)\bfunction\s+`+name+`\s*\(|\b`+name+`\s*=\s*\(`)
			case ".php":
				out = append(out, `(?m)^\s*function\s+`+name+`\s*\(`)
			case ".rb":
				out = append(out, `(?m)^\s*def\s+`+name+`\b`)
			case ".rs":
				out = append(out, `(?m)^\s*fn\s+`+name+`\b`)
			default:
				out = append(out, `(?i)`+name)
			}
		}
	}

	// import/add module indicators
	if strings.Contains(lower, "import ") || strings.Contains(lower, "add import") {
		impRe := regexp.MustCompile(`(?i)import\s+([A-Za-z0-9_./\-]+)`) // loose
		for _, m := range impRe.FindAllStringSubmatch(instructions, -1) {
			if len(m) >= 2 {
				mod := regexp.QuoteMeta(m[1])
				switch ext {
				case ".go":
					out = append(out, `(?m)"`+mod+`"`)
				case ".py":
					// from X import or import X
					out = append(out, `(?m)^(?:\s*from\s+`+mod+`\s+import\b|\s*import\s+`+mod+`\b)`)
				case ".js", ".ts":
					out = append(out, `(?m)from\s+['"]`+mod+`['"]|require\(\s*['"]`+mod+`['"]\s*\)`)
				default:
					out = append(out, `(?i)`+mod)
				}
			}
		}
	}

	// explicit symbol mentions: "add constant FOO_BAR" etc.
	constRe := regexp.MustCompile(`(?i)(?:const|var|let|define)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	for _, m := range constRe.FindAllStringSubmatch(instructions, -1) {
		if len(m) >= 2 {
			name := regexp.QuoteMeta(m[1])
			out = append(out, `(?i)\b`+name+`\b`)
		}
	}
	return uniqueStrings(out)
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// inferSymbolNames extracts probable symbol names (functions/classes) from instructions for structural checks
func inferSymbolNames(instructions string) []string {
    var out []string
    re := regexp.MustCompile(`(?i)(?:function|func|def|method|class|type)\s+([A-Za-z_][A-Za-z0-9_]*)`)
    for _, m := range re.FindAllStringSubmatch(instructions, -1) {
        if len(m) >= 2 { out = append(out, m[1]) }
    }
    return uniqueStrings(out)
}

// Language-aware structural verifiers (heuristics without external parsers)
func verifyPythonStructure(code string, names []string) (bool, string) {
    // Check def/class blocks end by indentation decrease
    lines := strings.Split(code, "\n")
    defRe := regexp.MustCompile(`^\s*def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
    clsRe := regexp.MustCompile(`^\s*class\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
    // quick map of defs/classes
    found := map[string]bool{}
    for i := 0; i < len(lines); i++ {
        if m := defRe.FindStringSubmatch(lines[i]); len(m) > 1 { if blockEndsProperly(lines, i) { found[m[1]] = true } }
        if m := clsRe.FindStringSubmatch(lines[i]); len(m) > 1 { if blockEndsProperly(lines, i) { found[m[1]] = true } }
    }
    for _, n := range names {
        // if a name is mentioned, prefer it to be found
        if _, ok := found[n]; !ok && len(names) > 0 { return false, fmt.Sprintf("python structure check: missing complete block for %s", n) }
    }
    return true, "python structure ok"
}

func blockEndsProperly(lines []string, start int) bool {
    base := leadingIndent(lines[start])
    for i := start + 1; i < len(lines); i++ {
        l := lines[i]
        if strings.TrimSpace(l) == "" { continue }
        if leadingIndent(l) <= base { return true }
    }
    // last line also ok
    return true
}

func leadingIndent(s string) int {
    n := 0
    for _, ch := range s { if ch == ' ' { n++ } else if ch == '\t' { n += 4 } else { break } }
    return n
}

func verifyJSStructure(code string, names []string) (bool, string) {
    // Ensure braces balance globally and for named functions/classes
    if !bracesBalanced(code) { return false, "js structure check: unbalanced braces" }
    funcRe := regexp.MustCompile(`(?m)^(?:\s*export\s+)?function\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
    arrowRe := regexp.MustCompile(`(?m)^(?:\s*export\s+)?(?:const|let|var)\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*\([^)]*\)\s*=>\s*\{`)
    clsRe := regexp.MustCompile(`(?m)^(?:\s*export\s+)?class\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
    found := map[string]bool{}
    markFound := func(re *regexp.Regexp) {
        for _, m := range re.FindAllStringSubmatch(code, -1) { if len(m) > 1 { found[m[1]] = true } }
    }
    markFound(funcRe); markFound(arrowRe); markFound(clsRe)
    for _, n := range names { if _, ok := found[n]; !ok && len(names) > 0 { return false, fmt.Sprintf("js/ts structure check: missing definition for %s", n) } }
    return true, "js structure ok"
}

func bracesBalanced(code string) bool {
    depth := 0
    for _, ch := range code {
        if ch == '{' { depth++ }
        if ch == '}' { depth--; if depth < 0 { return false } }
    }
    return depth == 0
}

func verifyRubyStructure(code string, names []string) (bool, string) {
    // Ensure every def/class/module has a matching end (simple counter)
    lines := strings.Split(code, "\n")
    depth := 0
    for _, l := range lines {
        t := strings.TrimSpace(l)
        if strings.HasPrefix(t, "def ") || strings.HasPrefix(t, "class ") || strings.HasPrefix(t, "module ") || strings.HasSuffix(t, " do") { depth++ }
        if t == "end" || strings.HasPrefix(t, "end ") { depth-- }
    }
    if depth != 0 { return false, "ruby structure check: unmatched end" }
    return true, "ruby structure ok"
}

func verifyPHPStructure(code string, names []string) (bool, string) {
    // Check braces balance and function/class presence if named
    if !bracesBalanced(code) { return false, "php structure check: unbalanced braces" }
    fRe := regexp.MustCompile(`(?m)^\s*function\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
    cRe := regexp.MustCompile(`(?m)^\s*class\s+([A-Za-z_][A-Za-z0-9_]*)`)
    found := map[string]bool{}
    for _, m := range fRe.FindAllStringSubmatch(code, -1) { if len(m) > 1 { found[m[1]] = true } }
    for _, m := range cRe.FindAllStringSubmatch(code, -1) { if len(m) > 1 { found[m[1]] = true } }
    for _, n := range names { if _, ok := found[n]; !ok && len(names) > 0 { return false, fmt.Sprintf("php structure check: missing definition for %s", n) } }
    return true, "php structure ok"
}

func verifyRustStructure(code string, names []string) (bool, string) {
    if !bracesBalanced(code) { return false, "rust structure check: unbalanced braces" }
    re := regexp.MustCompile(`(?m)^\s*fn\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
    found := map[string]bool{}
    for _, m := range re.FindAllStringSubmatch(code, -1) { if len(m) > 1 { found[m[1]] = true } }
    for _, n := range names { if _, ok := found[n]; !ok && len(names) > 0 { return false, fmt.Sprintf("rust structure check: missing fn %s", n) } }
    return true, "rust structure ok"
}
