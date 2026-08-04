package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoLegacyHomeDotSprout is a CI guard: no non-test Go file should
// construct a path via filepath.Join(home..., ".sprout") for user-level
// state/cache/data. Workspace-level .sprout paths are fine.
//
// This catches regressions where a new file resolves a user-level path
// to the old ~/.sprout location instead of the SP-133 category roots.
func TestNoLegacyHomeDotSprout(t *testing.T) {
	// Walk all .go files in pkg/ and cmd/, excluding test files.
	checkDir := func(dir string) {
		_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			if strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if strings.Contains(path, "vendor") {
				return nil
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			content := string(data)

			// Skip comments about the .sprout dir (documentation).
			// We're looking for code patterns, not comments.
			lines := strings.Split(content, "\n")
			for i, line := range lines {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
					continue
				}
				// Flag: filepath.Join(home..., ".sprout") for user-level paths
				if strings.Contains(line, "filepath.Join(") &&
					strings.Contains(line, `".sprout"`) &&
					(strings.Contains(line, "home") || strings.Contains(line, "HOME") || strings.Contains(line, "homeDir")) {
					// Allow workspace-level paths (relative ".sprout" under cwd/workspace)
					if strings.Contains(line, `cwd`) || strings.Contains(line, `workspace`) || strings.Contains(line, `rootDir`) || strings.Contains(line, `projectRoot`) || strings.Contains(line, `gitRoot`) || strings.Contains(line, `baseDir`) {
						continue
					}
					t.Errorf("%s:%d: user-level filepath.Join(home..., \".sprout\") — use envutil.StateDir()/CacheDir()/ConfigDir() instead: %s",
						path, i+1, trimmed)
				}
			}
			return nil
		})
	}

	checkDir("pkg")
	checkDir("cmd")
}
