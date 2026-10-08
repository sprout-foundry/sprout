//go:build !js

package filediscovery

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func pathsOf(res IndexResult) []string {
	out := make([]string, len(res.Files))
	for i, f := range res.Files {
		out[i] = f.Path
	}
	return out
}

func TestBuildFileIndex_WalksTree(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main")
	writeFile(t, root, "src/app.ts", "export {}")
	writeFile(t, root, "src/components/Button.tsx", "export {}")
	writeFile(t, root, "docs/readme.md", "# hi")

	res := BuildFileIndex(root, DefaultIndexLimits())
	paths := pathsOf(res)
	if len(paths) != 4 {
		t.Fatalf("expected 4 files, got %d: %v", len(paths), paths)
	}
	for _, want := range []string{"main.go", "src/app.ts", "src/components/Button.tsx", "docs/readme.md"} {
		if !slices.Contains(paths, want) {
			t.Errorf("missing %q in %v", want, paths)
		}
	}
	if res.Truncated {
		t.Error("small tree must not be truncated")
	}
	// Paths are workspace-relative slash paths.
	for _, p := range paths {
		if filepath.IsAbs(p) || containsBackslash(p) {
			t.Errorf("path %q must be relative with forward slashes", p)
		}
	}
}

func containsBackslash(s string) bool {
	for _, r := range s {
		if r == '\\' {
			return true
		}
	}
	return false
}

func TestBuildFileIndex_SkipsBuiltinsAndGitignore(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "keep.go", "package main")
	writeFile(t, root, "node_modules/pkg/index.js", "x")
	writeFile(t, root, ".git/HEAD", "ref: x")
	writeFile(t, root, "dist/bundle.js", "x")
	writeFile(t, root, "generated/gen.go", "package gen")
	writeFile(t, root, ".gitignore", "generated/\n")

	res := BuildFileIndex(root, DefaultIndexLimits())
	paths := pathsOf(res)
	// .gitignore is a regular file and stays indexable; the ignored tree and
	// builtin-skip dirs must not appear.
	if !slices.Contains(paths, ".gitignore") || !slices.Contains(paths, "keep.go") {
		t.Fatalf("expected .gitignore + keep.go, got %v", paths)
	}
	for _, banned := range []string{"node_modules/pkg/index.js", ".git/HEAD", "dist/bundle.js", "generated/gen.go"} {
		if slices.Contains(paths, banned) {
			t.Errorf("%s should be pruned, got %v", banned, paths)
		}
	}
	if !slices.Contains(res.SkippedDirs, "node_modules") {
		t.Errorf("node_modules should be reported skipped, got %v", res.SkippedDirs)
	}
}

func TestBuildFileIndex_RespectsFileCap(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a/1.txt", "x")
	writeFile(t, root, "a/2.txt", "x")
	writeFile(t, root, "b/3.txt", "x")
	writeFile(t, root, "top.txt", "x")

	limits := DefaultIndexLimits()
	limits.MaxFiles = 2
	res := BuildFileIndex(root, limits)
	if len(res.Files) != 2 {
		t.Fatalf("expected cap of 2, got %d", len(res.Files))
	}
	if !res.Truncated {
		t.Error("expected truncated=true when the cap is hit")
	}
}

func TestBuildFileIndex_RespectsDepthCap(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "l1/l2/l3/l4/l5/l6/l7/l8/l9/deep.txt", "x")
	writeFile(t, root, "shallow.txt", "x")

	limits := DefaultIndexLimits()
	limits.MaxDepth = 3
	res := BuildFileIndex(root, limits)
	paths := pathsOf(res)
	if len(paths) != 1 || paths[0] != "shallow.txt" {
		t.Fatalf("expected only shallow.txt within depth 3, got %v", paths)
	}
}

func TestBuildFileIndex_EmptyRoot(t *testing.T) {
	root := t.TempDir()
	res := BuildFileIndex(root, DefaultIndexLimits())
	if len(res.Files) != 0 || res.Truncated {
		t.Errorf("empty root should index nothing, got %d files truncated=%v", len(res.Files), res.Truncated)
	}
}
