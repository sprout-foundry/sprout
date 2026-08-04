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
	// Anchor to the repo root. Go runs tests with the working directory set to
	// the package directory, so walking a bare "pkg" here would target
	// pkg/configuration/pkg — which does not exist. filepath.Walk hands that
	// error to the walk func, the walk func returns nil, and the whole guard
	// passes vacuously no matter how many violations exist.
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	for _, sentinel := range []string{"go.mod", "pkg", "cmd"} {
		if _, err := os.Stat(filepath.Join(repoRoot, sentinel)); err != nil {
			t.Fatalf("repo root %q does not look like the sprout root (missing %s): %v", repoRoot, sentinel, err)
		}
	}

	scanned := 0

	// Walk all .go files in pkg/ and cmd/, excluding test files.
	checkDir := func(dir string) {
		root := filepath.Join(repoRoot, dir)
		if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
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

			scanned++

			data, err := os.ReadFile(path)
			if err != nil {
				return err
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
		}); err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}

	checkDir("pkg")
	checkDir("cmd")

	// Second guard: state/data/cache artifacts resolved off the CONFIG root.
	// The legacy-.sprout check above misses this entirely — a path built from
	// configDir looks modern but still lands in the wrong category root. This
	// is how two divergent embedding indexes appeared: the manager wrote
	// <data>/embeddings while the tool and CLI read <config>/embeddings.
	misrouted := map[string]string{
		"embeddings": "envutil.DataDir()",
		"sessions":   "envutil.StateDir()",
		"logs":       "envutil.StateDir()",
		"changes":    "the workspace root",
		"revisions":  "the workspace root",
		"runlogs":    "the workspace root",
	}
	checkConfigRootMisrouting := func(dir string) {
		root := filepath.Join(repoRoot, dir)
		if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(data), "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
					continue
				}
				if !strings.Contains(line, "filepath.Join(") || !strings.Contains(line, "onfigDir") {
					continue
				}
				for name, want := range misrouted {
					if strings.Contains(line, `"`+name+`"`) {
						t.Errorf("%s:%d: %q resolved off the config root — use %s instead: %s",
							path, i+1, name, want, trimmed)
					}
				}
			}
			return nil
		}); err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	checkConfigRootMisrouting("pkg")
	checkConfigRootMisrouting("cmd")

	// Fail loudly if the walk found nothing — that means the guard is not
	// actually inspecting the tree and would pass regardless of violations.
	if scanned < 100 {
		t.Fatalf("guard only scanned %d files; it is not covering the tree", scanned)
	}
}
