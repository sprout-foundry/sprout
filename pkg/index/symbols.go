package index

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Symbol struct {
	Name string `json:"name"`
	Kind string `json:"kind"` // func, class, type, method
}

type FileSymbols struct {
	File    string   `json:"file"`
	Symbols []Symbol `json:"symbols"`
}

type SymbolIndex struct {
	Files []FileSymbols `json:"files"`
}

// BuildSymbols scans the workspace root for source files and extracts simple symbols via regex
func BuildSymbols(root string) (*SymbolIndex, error) {
	var files []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info == nil || info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".go", ".py", ".js", ".ts", ".rb", ".php", ".rs", ".java":
			files = append(files, path)
		}
		return nil
	})

	idx := &SymbolIndex{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		content := string(b)
		ext := strings.ToLower(filepath.Ext(f))
		symbols := extractSymbols(ext, content)
		if len(symbols) > 0 {
			rel := f
			if r, err := filepath.Rel(root, f); err == nil {
				rel = r
			}
			idx.Files = append(idx.Files, FileSymbols{File: filepath.ToSlash(rel), Symbols: symbols})
		}
	}
	// persist to .ledit/symbols.json
	_ = os.MkdirAll(filepath.Join(root, ".ledit"), 0755)
	outPath := filepath.Join(root, ".ledit", "symbols.json")
	if f, err := os.Create(outPath); err == nil {
		_ = json.NewEncoder(f).Encode(idx)
		_ = f.Close()
	}
	return idx, nil
}

func extractSymbols(ext, content string) []Symbol {
	var out []Symbol
	add := func(kind, name string) {
		if name != "" {
			out = append(out, Symbol{Name: name, Kind: kind})
		}
	}
	switch ext {
	case ".go":
		reFunc := regexp.MustCompile(`(?m)^\s*func\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
		for _, m := range reFunc.FindAllStringSubmatch(content, -1) {
			add("func", m[1])
		}
		reType := regexp.MustCompile(`(?m)^\s*type\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
		for _, m := range reType.FindAllStringSubmatch(content, -1) {
			add("type", m[1])
		}
	case ".py":
		reDef := regexp.MustCompile(`(?m)^\s*def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
		for _, m := range reDef.FindAllStringSubmatch(content, -1) {
			add("func", m[1])
		}
		reCls := regexp.MustCompile(`(?m)^\s*class\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
		for _, m := range reCls.FindAllStringSubmatch(content, -1) {
			add("class", m[1])
		}
	case ".js", ".ts":
		reFunc := regexp.MustCompile(`(?m)\bfunction\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(|\b([A-Za-z_][A-Za-z0-9_]*)\s*=\s*\(`)
		for _, m := range reFunc.FindAllStringSubmatch(content, -1) {
			name := m[1]
			if name == "" {
				name = m[2]
			}
			add("func", name)
		}
		reCls := regexp.MustCompile(`(?m)\bclass\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
		for _, m := range reCls.FindAllStringSubmatch(content, -1) {
			add("class", m[1])
		}
	case ".rb":
		reDef := regexp.MustCompile(`(?m)^\s*def\s+([A-Za-z_][A-Za-z0-9_!?]*)`)
		for _, m := range reDef.FindAllStringSubmatch(content, -1) {
			add("func", m[1])
		}
		reCls := regexp.MustCompile(`(?m)^\s*class\s+([A-Za-z_][A-Za-z0-9_:]*)`)
		for _, m := range reCls.FindAllStringSubmatch(content, -1) {
			add("class", m[1])
		}
	case ".php":
		reDef := regexp.MustCompile(`(?m)^\s*function\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
		for _, m := range reDef.FindAllStringSubmatch(content, -1) {
			add("func", m[1])
		}
		reCls := regexp.MustCompile(`(?m)^\s*class\s+([A-Za-z_][A-Za-z0-9_]*)`)
		for _, m := range reCls.FindAllStringSubmatch(content, -1) {
			add("class", m[1])
		}
	case ".rs":
		reFn := regexp.MustCompile(`(?m)^\s*fn\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
		for _, m := range reFn.FindAllStringSubmatch(content, -1) {
			add("func", m[1])
		}
	case ".java":
		reCls := regexp.MustCompile(`(?m)\bclass\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
		for _, m := range reCls.FindAllStringSubmatch(content, -1) {
			add("class", m[1])
		}
		reMeth := regexp.MustCompile(`(?m)\b([A-Za-z_][A-Za-z0-9_]*)\s+([A-Za-z_][A-Za-z0-9_]*)\s*\([^;]*\)\s*\{`)
		for _, m := range reMeth.FindAllStringSubmatch(content, -1) {
			add("method", m[2])
		}
	}
	return out
}

// SearchSymbols returns files whose symbols match any of the provided tokens (case-insensitive)
func SearchSymbols(idx *SymbolIndex, tokens []string) []string {
	var out []string
	tokSet := map[string]bool{}
	for _, t := range tokens {
		t = strings.ToLower(strings.TrimSpace(t))
		if len(t) >= 3 {
			tokSet[t] = true
		}
	}
	for _, fs := range idx.Files {
		match := false
		for _, s := range fs.Symbols {
			name := strings.ToLower(s.Name)
			for t := range tokSet {
				if strings.Contains(name, t) {
					match = true
					break
				}
			}
			if match {
				break
			}
		}
		if match {
			out = append(out, fs.File)
		}
	}
	return out
}
