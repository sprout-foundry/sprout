package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	repoMapMaxFileSize = 32 * 1024 // 32KB per file
	repoMapTokenBudget = 1024       // target ~1024 tokens
	repoMapMaxFiles    = 200        // max files to include
	repoMapCharBudget  = repoMapTokenBudget * 4
)

// Regex patterns for symbol extraction (top-level declarations).
var (
	goFuncRe    = regexp.MustCompile(`^\s*func\s+(?:\([^)]*\)\s+)?(\w+)`)
	goTypeRe    = regexp.MustCompile(`^\s*type\s+(\w+)\s+(struct|interface)\b`)
	goVarRe     = regexp.MustCompile(`^\s*var\s+(\w+)`)
	goConstRe   = regexp.MustCompile(`^\s*const\s+(\w+)`)
	tsFuncRe    = regexp.MustCompile(`^\s*(?:export\s+)?(?:async\s+)?function\s*(?:<[^>]*>\s*)?(\w+)`)
	tsClassRe   = regexp.MustCompile(`^\s*(?:export\s+)?class\s+(\w+)`)
	tsIfRe      = regexp.MustCompile(`^\s*(?:export\s+)?interface\s+(\w+)`)
	tsTypeRe    = regexp.MustCompile(`^\s*(?:export\s+)?type\s+(\w+)`)
	tsConstRe   = regexp.MustCompile(`^\s*(?:export\s+)?(?:const|let|var)\s+(\w+)`)
	pyFuncRe    = regexp.MustCompile(`^\s*(?:async\s+)?def\s+(\w+)`)
	pyClassRe   = regexp.MustCompile(`^\s*class\s+(\w+)`)
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
// lightweight, AST-like overview of the codebase.  For each supported source
// file it extracts top-level symbols (functions, types, interfaces, classes)
// using simple regex patterns. Output is truncated to ~1024 tokens.
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

		content, err := os.ReadFile(f.absPath)
		if err != nil {
			continue
		}
		if len(content) > repoMapMaxFileSize {
			content = content[:repoMapMaxFileSize]
		}
		if isBinaryContent(content) {
			continue
		}

		symbols := extractSymbols(f.ext, string(content))
		if len(symbols) == 0 {
			continue
		}

		section := "\n### " + f.relPath + "\n"
		for _, sym := range symbols {
			section += "- " + sym + "\n"
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

func extractSymbols(ext string, content string) []string {
	var symbols []string
	for _, line := range strings.Split(content, "\n") {
		switch ext {
		case ".go":
			symbols = appendSymbols(symbols, extractGoSymbols(line))
		case ".ts", ".tsx", ".js", ".jsx":
			symbols = appendSymbols(symbols, extractTSSymbols(line))
		case ".py":
			symbols = appendSymbols(symbols, extractPySymbols(line))
		}
	}
	return symbols
}

func extractGoSymbols(line string) []string {
	if m := goFuncRe.FindStringSubmatch(line); m != nil {
		return []string{"func " + m[1]}
	}
	if m := goTypeRe.FindStringSubmatch(line); m != nil {
		return []string{"type " + m[1] + " " + m[2]}
	}
	if m := goVarRe.FindStringSubmatch(line); m != nil {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "var ") {
			return []string{"var " + m[1]}
		}
	}
	if m := goConstRe.FindStringSubmatch(line); m != nil {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "const ") {
			return []string{"const " + m[1]}
		}
	}
	return nil
}

func extractTSSymbols(line string) []string {
	t := strings.TrimSpace(line)
	if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "*") {
		return nil
	}
	if m := tsFuncRe.FindStringSubmatch(line); m != nil {
		return []string{"function " + m[1]}
	}
	if m := tsClassRe.FindStringSubmatch(line); m != nil {
		return []string{"class " + m[1]}
	}
	if m := tsIfRe.FindStringSubmatch(line); m != nil {
		return []string{"interface " + m[1]}
	}
	if m := tsTypeRe.FindStringSubmatch(line); m != nil {
		return []string{"type " + m[1]}
	}
	if m := tsConstRe.FindStringSubmatch(line); m != nil {
		if strings.HasPrefix(t, "export") || strings.HasPrefix(t, "const ") ||
			strings.HasPrefix(t, "let ") || strings.HasPrefix(t, "var ") {
			return []string{"const " + m[1]}
		}
	}
	return nil
}

func extractPySymbols(line string) []string {
	t := strings.TrimSpace(line)
	if strings.HasPrefix(t, "#") {
		return nil
	}
	if m := pyFuncRe.FindStringSubmatch(line); m != nil {
		return []string{"def " + m[1]}
	}
	if m := pyClassRe.FindStringSubmatch(line); m != nil {
		return []string{"class " + m[1]}
	}
	return nil
}

func appendSymbols(base []string, newSyms []string) []string {
	for _, s := range newSyms {
		found := false
		for _, e := range base {
			if e == s {
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
