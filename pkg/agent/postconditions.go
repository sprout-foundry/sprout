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
	return true, "all inferred patterns present"
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
