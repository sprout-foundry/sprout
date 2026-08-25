// Package filediscovery: discovery strategies (split from filediscovery.go)

package filediscovery

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/index"
)

// discoverBasic provides basic file listing
func (fd *FileDiscovery) discoverBasic(options *DiscoveryOptions) *FileResult {
	root := options.RootPath
	if root == "" {
		root = "."
	}

	// Check if root directory exists
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return &FileResult{Error: fmt.Errorf("directory does not exist: %s", root)}
	}

	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		if info.IsDir() {
			// Skip excluded directories
			for _, exclude := range options.ExcludeDirs {
				if strings.Contains(path, exclude) {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Skip hidden files unless requested
		if !options.IncludeHidden && strings.HasPrefix(info.Name(), ".") {
			return nil
		}

		// Check file extensions
		if len(options.IncludeExts) > 0 {
			ext := filepath.Ext(path)
			found := false
			for _, includeExt := range options.IncludeExts {
				if ext == includeExt {
					found = true
					break
				}
			}
			if !found {
				return nil
			}
		}

		// Exclude file extensions
		if len(options.ExcludeExts) > 0 {
			ext := filepath.Ext(path)
			for _, excludeExt := range options.ExcludeExts {
				if ext == excludeExt {
					return nil
				}
			}
		}

		files = append(files, path)
		return nil
	})

	if err != nil {
		return &FileResult{Error: fmt.Errorf("failed to walk directory: %w", err)}
	}

	result := fd.applyFiltersAndLimits(files, options)
	result.TotalFiles = len(files)

	return result
}

// rerankWithSymbols reranks files based on symbol index overlap
func (fd *FileDiscovery) rerankWithSymbols(files []string, userIntent string) []string {
	root, _ := os.Getwd()
	symHits := map[string]int{}

	if idx, err := index.BuildSymbols(root); err == nil && idx != nil {
		tokens := strings.Fields(userIntent)
		for _, fs := range index.SearchSymbolFiles(idx, tokens) {
			symHits[fs.File] += len(fs.Symbols)
		}
	}

	if len(files) > 0 {
		type scoredFile struct {
			file  string
			score int
		}

		var scored []scoredFile
		for _, f := range files {
			rel, err := filepath.Rel(root, f)
			if err != nil {
				rel = f
			}
			s := symHits[filepath.ToSlash(rel)]
			scored = append(scored, scoredFile{file: f, score: s})
		}

		// Sort by score descending, then by filename ascending
		sort.Slice(scored, func(i, j int) bool {
			if scored[i].score == scored[j].score {
				return scored[i].file < scored[j].file
			}
			return scored[i].score > scored[j].score
		})

		result := make([]string, len(scored))
		for i, s := range scored {
			result[i] = s.file
		}

		return result
	}

	return files
}

// extractSearchTerms extracts meaningful search terms from user intent
func (fd *FileDiscovery) extractSearchTerms(userIntent string) []string {
	words := strings.Fields(strings.ToLower(userIntent))
	var terms []string

	// Common words to skip
	skipWords := map[string]bool{
		"the": true, "a": true, "an": true, "and": true, "or": true,
		"but": true, "in": true, "on": true, "at": true, "to": true,
		"for": true, "of": true, "with": true, "by": true, "is": true,
		"are": true, "was": true, "were": true, "be": true, "been": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "must": true, "can": true, "i": true,
		"you": true, "he": true, "she": true, "it": true, "we": true, "they": true,
		"this": true, "that": true, "these": true, "those": true,
		"find": true, "search": true, "grep": true, "look": true, "locate": true,
	}

	for _, word := range words {
		word = strings.Trim(word, ".,!?;:\"'()[]{}")
		if len(word) > 2 && !skipWords[word] {
			terms = append(terms, word)
		}
	}

	return terms
}
