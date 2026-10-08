package filediscovery

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	ignore "github.com/sabhiram/go-gitignore"
)

// ignore-rule cache: compiling gitignore patterns dominates repeated
// listings (the palette crawl used to re-read + recompile per directory
// request). Keyed by workspace root; entries revalidate when either ignore
// file's mtime or size changes, so edits to .gitignore take effect without
// a restart.
type ignoreRulesKey struct {
	root string
}

type ignoreRulesEntry struct {
	rules      *ignore.GitIgnore
	gitMtimeNs int64
	gitSize    int64
	sproutMtm  int64
	sproutSize int64
}

var (
	ignoreRulesMu    sync.Mutex
	ignoreRulesCache = make(map[ignoreRulesKey]ignoreRulesEntry)
)

func statIgnoreFile(path string) (int64, int64) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0
	}
	return info.ModTime().UnixNano(), info.Size()
}

// GetIgnoreRules reads ignore files (.gitignore, .sprout/.ignore) and returns a gitignore object.
// Results are cached per root and revalidated against file mtimes, so the
// hot paths (/api/browse, /api/search, /api/file-index) don't re-read and
// recompile the rule set on every request.
func GetIgnoreRules(rootDir string) *ignore.GitIgnore {
	gitignorePath := filepath.Join(rootDir, ".gitignore")
	sproutIgnorePath := filepath.Join(rootDir, ".sprout", ".ignore")
	gitMtime, gitSize := statIgnoreFile(gitignorePath)
	sproutMtime, sproutSize := statIgnoreFile(sproutIgnorePath)

	ignoreRulesMu.Lock()
	defer ignoreRulesMu.Unlock()

	key := ignoreRulesKey{root: rootDir}
	if entry, ok := ignoreRulesCache[key]; ok {
		if entry.gitMtimeNs == gitMtime && entry.gitSize == gitSize &&
			entry.sproutMtm == sproutMtime && entry.sproutSize == sproutSize {
			return entry.rules
		}
	}

	var allRules []string

	// Read .gitignore
	if rules, err := readIgnoreFile(gitignorePath); err == nil {
		allRules = append(allRules, rules...)
	}

	// Read .sprout/.ignore
	if rules, err := readIgnoreFile(sproutIgnorePath); err == nil {
		allRules = append(allRules, rules...)
	}

	var compiled *ignore.GitIgnore
	if len(allRules) > 0 {
		compiled = ignore.CompileIgnoreLines(allRules...)
	}

	ignoreRulesCache[key] = ignoreRulesEntry{
		rules:      compiled,
		gitMtimeNs: gitMtime,
		gitSize:    gitSize,
		sproutMtm:  sproutMtime,
		sproutSize: sproutSize,
	}
	return compiled
}

// readIgnoreFile reads a single ignore file and returns its lines.
func readIgnoreFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open ignore file: %w", err)
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}
