package playbooks

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Dependency/API helpers
func parseGoModuleFromIntent(intent string) (module string, version string) {
	// Try to extract patterns like: module@v1.2.3 or mention in quotes
	lo := intent
	re := regexp.MustCompile(`([a-zA-Z0-9_./\-]+)@([vV]?\d+\.[\d]+(\.[\d]+)?)`)
	if m := re.FindStringSubmatch(lo); len(m) >= 3 {
		return m[1], m[2]
	}
	// Try go get module or upgrade module mentions
	re2 := regexp.MustCompile(`go\s+get\s+([a-zA-Z0-9_./\-]+)(?:@([vV]?\d+\.[\d]+(\.[\d]+)?))?`)
	if m := re2.FindStringSubmatch(lo); len(m) >= 2 {
		mod := m[1]
		ver := ""
		if len(m) >= 3 {
			ver = m[2]
		}
		return mod, ver
	}
	return "", ""
}

func findFilesImportingModule(module string) []string {
	var results []string
	if module == "" {
		return results
	}
	_ = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "assets" || name == "debug" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(string(b), module) {
			results = append(results, filepath.ToSlash(path))
		}
		return nil
	})
	return dedupeStrings(results)
}

func parseAPIRenameFromIntent(intent string) (oldName, newName string) {
	// Handle "rename Old to New"
	re := regexp.MustCompile(`(?i)rename\s+([A-Za-z0-9_]+)\s+to\s+([A-Za-z0-9_]+)`)
	if m := re.FindStringSubmatch(intent); len(m) == 3 {
		return m[1], m[2]
	}
	// Handle "Old -> New"
	re2 := regexp.MustCompile(`\b([A-Za-z0-9_]+)\s*->\s*([A-Za-z0-9_]+)\b`)
	if m := re2.FindStringSubmatch(intent); len(m) == 3 {
		return m[1], m[2]
	}
	return "", ""
}

func findFilesWithSymbol(symbol string) []string {
	var results []string
	if symbol == "" {
		return results
	}
	_ = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "assets" || name == "debug" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(b)
		if strings.Contains(content, symbol) {
			results = append(results, filepath.ToSlash(path))
		}
		return nil
	})
	return dedupeStrings(results)
}

// perf/security candidate discovery
func findPerfHotCandidates(userIntent string) []string {
	var results []string
	// If the intent mentions a symbol, try finding it
	if sym := parseSymbolNameFromIntent(userIntent); sym != "" {
		results = append(results, findFilesWithSymbol(sym)...)
	}
	// Heuristics: look for hotspots (many appends/allocations/tight loops)
	_ = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "assets" || name == "debug" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		s := string(b)
		score := 0
		// simple scoring
		score += strings.Count(s, "append(")
		score += strings.Count(s, "make(")
		score += strings.Count(s, "for ")
		if strings.Contains(s, "pprof") {
			score += 5
		}
		if score >= 8 { // heuristic threshold
			results = append(results, filepath.ToSlash(path))
		}
		return nil
	})
	return dedupeStrings(results)
}

func findSecurityRiskCandidates() []string {
	var results []string
	weakRe := []*regexp.Regexp{
		regexp.MustCompile(`\bmd5\.`),
		regexp.MustCompile(`\bsha1\.`),
		regexp.MustCompile(`http://`),
		regexp.MustCompile(`InsecureSkipVerify\s*:\s*true`),
		regexp.MustCompile(`(?i)password`),
		regexp.MustCompile(`(?i)secret`),
		regexp.MustCompile(`(?i)token`),
	}
	_ = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "assets" || name == "debug" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		s := string(b)
		for _, re := range weakRe {
			if re.FindStringIndex(s) != nil {
				results = append(results, filepath.ToSlash(path))
				break
			}
		}
		return nil
	})
	return dedupeStrings(results)
}

// Test/CI helpers
func findSourceFilesWithoutTests(limit int) []string {
	var results []string
	hasTest := map[string]bool{}
	// First record test files
	_ = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "assets" || name == "debug" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			base := strings.TrimSuffix(filepath.ToSlash(path), "_test.go") + ".go"
			hasTest[base] = true
		}
		return nil
	})
	// Now find source files without tests
	_ = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "assets" || name == "debug" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			p := filepath.ToSlash(path)
			if !hasTest[p] {
				results = append(results, p)
				if len(results) >= limit {
					return errors.New("limit")
				}
			}
		}
		return nil
	})
	return dedupeStrings(results)
}

func listCIConfigFiles(limit int) []string {
	var res []string
	candidates := []string{
		".github/workflows", ".github/workflows/ci.yml", ".github/workflows/test.yml",
		".travis.yml", ".circleci/config.yml", "Jenkinsfile", "azure-pipelines.yml",
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil {
			if st.IsDir() {
				// List yml files inside
				_ = filepath.WalkDir(c, func(path string, d fs.DirEntry, err error) error {
					if err != nil {
						return nil
					}
					if d.IsDir() {
						return nil
					}
					if strings.HasSuffix(path, ".yml") || strings.HasSuffix(path, ".yaml") {
						res = append(res, filepath.ToSlash(path))
						if len(res) >= limit {
							return errors.New("limit")
						}
					}
					return nil
				})
			} else {
				res = append(res, filepath.ToSlash(c))
			}
		}
		if len(res) >= limit {
			break
		}
	}
	return dedupeStrings(res)
}
