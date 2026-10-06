package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// RegistryEntry mirrors the TypeScript CloudEndpoint object literal from the
// cloud endpoint registry (webui/src/services/cloudEndpointRegistry/).
type RegistryEntry struct {
	Path       string
	Methods    []string
	Category   string
	IsPrefix   bool
	SourceFile string
}

var (
	registryPathRe       = regexp.MustCompile(`path:\s*'([^']*)'`)
	registryCategoryRe   = regexp.MustCompile(`category:\s*'([^']*)'`)
	registryMethodsRe    = regexp.MustCompile(`methods:\s*\[([^\]]*)\]`)
	registryIsPrefixRe   = regexp.MustCompile(`isPrefix:\s*(true|false)`)
	registryStringItemRe = regexp.MustCompile(`'([^']*)'`)
)

// parseRegistryFiles parses every .ts file under dir and returns the
// CloudEndpoint entries in deterministic order (sorted by path, then prefix
// flag, then category). The registry files are hand-maintained, uniform,
// line-structured object literals, so a tolerant brace-depth scan plus field
// regexes is used instead of a full TypeScript parse. Only top-level entry
// objects are captured; nested objects (e.g. syntheticResponse) sit one brace
// level deeper and are not returned as separate entries.
func parseRegistryFiles(dir string) ([]RegistryEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read registry dir %s: %w", dir, err)
	}
	var all []RegistryEntry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ts") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(full) // #nosec G703 -- resolves within the repo's registry endpoints dir
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", full, err)
		}
		for _, block := range extractEntryObjects(string(data)) {
			re := RegistryEntry{SourceFile: e.Name()}
			if m := registryPathRe.FindStringSubmatch(block); m != nil {
				re.Path = m[1]
			}
			if m := registryCategoryRe.FindStringSubmatch(block); m != nil {
				re.Category = m[1]
			}
			if m := registryMethodsRe.FindStringSubmatch(block); m != nil {
				re.Methods = splitMethods(m[1])
			}
			if m := registryIsPrefixRe.FindStringSubmatch(block); m != nil && m[1] == "true" {
				re.IsPrefix = true
			}
			if re.Path == "" {
				continue
			}
			all = append(all, re)
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Path != all[j].Path {
			return all[i].Path < all[j].Path
		}
		if all[i].IsPrefix != all[j].IsPrefix {
			return all[j].IsPrefix // exact entries sort before prefix entries
		}
		if all[i].Category != all[j].Category {
			return all[i].Category < all[j].Category
		}
		return all[i].SourceFile < all[j].SourceFile
	})
	return all, nil
}

// extractEntryObjects returns the text of each top-level object literal in
// src, using brace-depth tracking.
func extractEntryObjects(src string) []string {
	var out []string
	depth := 0
	start := -1
	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			depth--
			if depth == 0 && start >= 0 {
				out = append(out, src[start:i+1])
				start = -1
			}
		}
	}
	return out
}

// splitMethods turns the inner content of a methods: [...] literal into a
// clean, ordered, de-duplicated method list.
func splitMethods(inner string) []string {
	var raw []string
	for _, m := range registryStringItemRe.FindAllStringSubmatch(inner, -1) {
		raw = append(raw, strings.TrimSpace(m[1]))
	}
	return normalizeMethods(raw)
}

// registryMethodsForPath returns the registry methods for a route path,
// preferring the most specific covering entry (an exact entry beats a prefix
// entry; a longer prefix beats a shorter one).
func registryMethodsForPath(path string, registry []RegistryEntry) ([]string, bool) {
	var exact []string
	bestPrefix := ""
	var bestPrefixMethods []string
	foundPrefix := false
	for _, e := range registry {
		if !e.IsPrefix && path == e.Path {
			exact = e.Methods
			break
		}
	}
	if exact != nil {
		return normalizeMethods(exact), true
	}
	for _, e := range registry {
		if e.IsPrefix && strings.HasPrefix(path, e.Path) && len(e.Path) > len(bestPrefix) {
			bestPrefix = e.Path
			bestPrefixMethods = e.Methods
			foundPrefix = true
		}
	}
	if foundPrefix {
		return normalizeMethods(bestPrefixMethods), true
	}
	return nil, false
}
