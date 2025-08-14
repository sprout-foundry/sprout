package playbooks

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

func dedupeStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// startsWithGoComment reports whether a file begins with a comment
func startsWithGoComment(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	content := string(b)
	lines := strings.Split(content, "\n")
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*") {
			return true
		}
		return false
	}
	return false
}

// Utility helpers for discovery
func findGoFilesLimited(limit int) []string {
	var files []string
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
		if strings.HasSuffix(path, ".go") {
			files = append(files, filepath.ToSlash(path))
			if len(files) >= limit {
				return errors.New("limit reached")
			}
		}
		return nil
	})
	if len(files) > limit {
		files = files[:limit]
	}
	return files
}

func listGoFilesBySizeDesc(limit int) []string {
	type fsz struct {
		p string
		s int64
	}
	var arr []fsz
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
		if strings.HasSuffix(path, ".go") {
			if info, err := os.Stat(path); err == nil {
				arr = append(arr, fsz{p: filepath.ToSlash(path), s: info.Size()})
			}
		}
		return nil
	})
	sort.Slice(arr, func(i, j int) bool { return arr[i].s > arr[j].s })
	if len(arr) > limit {
		arr = arr[:limit]
	}
	res := make([]string, 0, len(arr))
	for _, a := range arr {
		res = append(res, a.p)
	}
	return res
}

func parseSymbolNameFromIntent(intent string) string {
	// Look for simple symbol name patterns after common verbs
	re := regexp.MustCompile(`(?i)(rename|refactor|extract)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	if m := re.FindStringSubmatch(intent); len(m) == 3 {
		return m[2]
	}
	return ""
}

// Type/field rename parsing
func parseTypeRenameFromIntent(intent string) (oldType, newType string) {
	re := regexp.MustCompile(`(?i)(?:type|struct|interface)?\s*rename\s+([A-Za-z_][A-Za-z0-9_]*)\s+to\s+([A-Za-z_][A-Za-z0-9_]*)`)
	if m := re.FindStringSubmatch(intent); len(m) == 3 {
		return m[1], m[2]
	}
	re2 := regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\s*->\s*([A-Za-z_][A-Za-z0-9_]*)\b`)
	if m := re2.FindStringSubmatch(intent); len(m) == 3 {
		return m[1], m[2]
	}
	return "", ""
}

func parseFieldRenameFromIntent(intent string) (oldField, newField string) {
	re := regexp.MustCompile(`(?i)field\s+([A-Za-z_][A-Za-z0-9_]*)\s+to\s+([A-Za-z_][A-Za-z0-9_]*)`)
	if m := re.FindStringSubmatch(intent); len(m) == 3 {
		return m[1], m[2]
	}
	re2 := regexp.MustCompile(`(?i)rename\s+field\s+([A-Za-z_][A-Za-z0-9_]*)\s+to\s+([A-Za-z_][A-Za-z0-9_]*)`)
	if m := re2.FindStringSubmatch(intent); len(m) == 3 {
		return m[1], m[2]
	}
	return "", ""
}

// File path helpers
func extractFilePathsFromIntent(intent string, limit int) []string {
	var res []string
	// Match path-like tokens with extensions
	re := regexp.MustCompile(`([A-Za-z0-9_./\\\\\-]+\.[A-Za-z0-9_]+)`) // simple
	for _, m := range re.FindAllString(intent, -1) {
		res = append(res, filepath.ToSlash(m))
		if len(res) >= limit {
			break
		}
	}
	return dedupeStrings(res)
}

func parseFileRenameFromIntent(intent string) (from string, to string) {
	// Patterns: rename a/b.go to c/d.go  | a/b.go -> c/d.go  | mv a/b.go c/d.go
	re1 := regexp.MustCompile(`(?i)rename\s+([A-Za-z0-9_./\\\\\-]+)\s+to\s+([A-Za-z0-9_./\\\\\-]+)`) // rename X to Y
	if m := re1.FindStringSubmatch(intent); len(m) == 3 {
		return filepath.ToSlash(m[1]), filepath.ToSlash(m[2])
	}
	re2 := regexp.MustCompile(`([A-Za-z0-9_./\\\\\-]+)\s*->\s*([A-Za-z0-9_./\\\\\-]+)`) // X -> Y
	if m := re2.FindStringSubmatch(intent); len(m) == 3 {
		return filepath.ToSlash(m[1]), filepath.ToSlash(m[2])
	}
	re3 := regexp.MustCompile(`(?i)\bmv\s+([A-Za-z0-9_./\\\\\-]+)\s+([A-Za-z0-9_./\\\\\-]+)`) // mv X Y
	if m := re3.FindStringSubmatch(intent); len(m) == 3 {
		return filepath.ToSlash(m[1]), filepath.ToSlash(m[2])
	}
	return "", ""
}

func findFilesMentioningString(substr string, limit int) []string {
	var results []string
	if substr == "" {
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
		if strings.Contains(string(b), substr) {
			results = append(results, filepath.ToSlash(path))
			if len(results) >= limit {
				return errors.New("limit")
			}
		}
		return nil
	})
	return dedupeStrings(results)
}
