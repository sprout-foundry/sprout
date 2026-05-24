//go:build js && wasm

// Tests for the WASM JS bridge helpers. Run via:
//
//	GOOS=js GOARCH=wasm go test \
//	  -exec "$(go env GOROOT)/lib/wasm/go_js_wasm_exec" \
//	  ./cmd/wasm/
//
// Most of the WASM bridge surface depends on js.Value and a live JS host,
// which is hard to fake. These tests pin only the pure-Go logic that
// matters for correctness: memory-name sanitization (security), record→JS
// shaping (UI contract), and the linear-scan helpers.

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

func TestSaveMemoryToDisk_RejectsPathTraversal(t *testing.T) {
	cases := []string{"../escape", "..", ".", "", "foo/bar", "foo\\bar", "../../etc/passwd"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			err := saveMemoryToDisk(name, "content")
			if err == nil {
				t.Errorf("saveMemoryToDisk(%q) should have rejected the name", name)
				return
			}
			if !strings.Contains(err.Error(), "invalid memory name") &&
				!strings.Contains(err.Error(), "memory directory unavailable") {
				t.Errorf("error for %q should be sanitization-flavored, got %v", name, err)
			}
		})
	}
}

func TestDeleteMemoryFromDisk_RejectsPathTraversal(t *testing.T) {
	for _, name := range []string{"../escape", "..", ".", "", "foo/bar"} {
		t.Run(name, func(t *testing.T) {
			err := deleteMemoryFromDisk(name)
			if err == nil {
				t.Errorf("deleteMemoryFromDisk(%q) should have rejected the name", name)
			}
		})
	}
}

func TestIndexOfID(t *testing.T) {
	records := []embedding.VectorRecord{
		{ID: "alpha"},
		{ID: "beta"},
		{ID: "gamma"},
	}
	cases := []struct {
		id   string
		want int
	}{
		{"alpha", 0},
		{"beta", 1},
		{"gamma", 2},
		{"missing", -1},
		{"", -1},
	}
	for _, c := range cases {
		got := indexOfID(records, c.id)
		if got != c.want {
			t.Errorf("indexOfID(%q) = %d, want %d", c.id, got, c.want)
		}
	}
}

func TestTurnRecordToJS_StripsEmbeddingAndPropagatesMetadata(t *testing.T) {
	now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	rec := embedding.VectorRecord{
		ID:        "turn-1",
		Signature: "hello world",
		IndexedAt: now,
		Type:      "conversation_turn",
		Embedding: []float32{0.1, 0.2, 0.3}, // should NOT make it into the JS payload
		Metadata: map[string]interface{}{
			"sessionId":         "sess-abc",
			"turnNumber":        3,
			"workingDir":        "/home/user/proj",
			"duration":          1.5,
			"tokenUsage":        450,
			"actionableSummary": "Said hi",
			"filesTouched":      []string{"main.go"},
		},
	}
	got := turnRecordToJS(rec)

	// Required fields
	if got["id"] != "turn-1" {
		t.Errorf("id = %v", got["id"])
	}
	if got["userPrompt"] != "hello world" {
		t.Errorf("userPrompt = %v", got["userPrompt"])
	}
	if got["indexedAt"] != now.Format(time.RFC3339Nano) {
		t.Errorf("indexedAt = %v", got["indexedAt"])
	}

	// Metadata propagation
	if got["sessionId"] != "sess-abc" {
		t.Errorf("sessionId = %v", got["sessionId"])
	}
	if got["turnNumber"] != 3 {
		t.Errorf("turnNumber = %v", got["turnNumber"])
	}
	if got["workingDir"] != "/home/user/proj" {
		t.Errorf("workingDir = %v", got["workingDir"])
	}
	if got["actionableSummary"] != "Said hi" {
		t.Errorf("actionableSummary = %v", got["actionableSummary"])
	}

	// Embedding must not leak — it's large and useless to the browser side.
	if _, present := got["embedding"]; present {
		t.Error("turnRecordToJS leaked the embedding vector into the JS payload")
	}

	// Deleted flag absent by default; only present when explicitly set
	if _, present := got["deleted"]; present {
		t.Error("deleted flag should be absent when metadata.deleted is unset")
	}
}

func TestTurnRecordToJS_DeletedFlagPropagates(t *testing.T) {
	rec := embedding.VectorRecord{
		ID:        "turn-x",
		Signature: "deleted thing",
		Metadata: map[string]interface{}{
			"sessionId": "sess",
			"deleted":   true,
		},
	}
	got := turnRecordToJS(rec)
	if got["deleted"] != true {
		t.Errorf("deleted should be true, got %v", got["deleted"])
	}
}

func TestTurnRecordToJS_NilMetadataIsSafe(t *testing.T) {
	rec := embedding.VectorRecord{ID: "turn-y", Signature: "no metadata"}
	got := turnRecordToJS(rec)
	if got["id"] != "turn-y" {
		t.Errorf("id = %v", got["id"])
	}
	// Should not panic on nil Metadata; should also not invent fields.
	for _, key := range []string{"sessionId", "workingDir", "duration", "tokenUsage"} {
		if _, present := got[key]; present {
			t.Errorf("unexpected key %q present with nil metadata", key)
		}
	}
}

// ── Workspace Walk Tests ───────────────────────────────────────

// setupWorkspaceTree creates a temp directory with a mix of code files,
// indexable non-code files, and ignored directories/files so we can
// assert that the walk functions collect exactly what they should.
func setupWorkspaceTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// Code files (should be returned by WalkCodeFiles)
	codeFiles := map[string]string{
		"main.go":         "package main",
		"app.ts":          "import React",
		"util.js":         "export default {}",
		"component.tsx":   "<div/>",
		"style.jsx":       "export default () => {}",
		"module.mjs":      "import { x } from 'y'",
		"script.py":       "import sys",
		"src/lib.go":      "package src",
		"src/handler.ts":  "const handler = () => {}",
	}

	// Non-code indexable files (should only appear in WalkAllIndexableFiles)
	indexableFiles := map[string]string{
		"README.md":   "# Project",
		"config.yaml": "key: value",
		"Makefile":    "build:\n\techo ok",
		"Dockerfile":  "FROM scratch",
		".gitignore":  "*.tmp",
		"notes.txt":   "hello",
		"setup.sh":    "#!/bin/sh",
	}

	// Plant files inside ignored directories that MUST NOT appear in results.
	create := func(relPath, content string) {
		path := filepath.Join(root, relPath)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("failed to create %s: %v", path, err)
		}
	}

	for rel, content := range codeFiles {
		create(rel, content)
	}
	for rel, content := range indexableFiles {
		create(rel, content)
	}

	// Plant files inside ignored directories that MUST NOT appear in results.
	ignoredFiles := map[string]string{
		".git/config":          "[core]",
		".git/objects/00":     "blob",
		"node_modules/pkg/index.js": "module.exports={}",
		"vendor/std/lib.go":    "package std",
		"dist/bundle.js":       "eval('x')",
		"dist/output.js":       "eval('y')",
		"build/main.js":        "console.log('build')",
		"__pycache__/mod.pyc":  "\x00\x01",
		".venv/lib/dep.py":     "pass",
		".tox/test/script.sh":  "#!/bin/sh",
		".next/build.js":       "next build",
		".nuxt/index.ts":       "nuxt",
		"coverage/report.js":   "cov",
		".cache/data.go":       "cached",
		".gradle/build.gradle": "gradle",
		".mvn/wrapper.java":    "mvn",
		"target/class.java":    "javac",
		".idea/workspace.xml":  "<project>",
		".vscode/settings.json": "{}",
		".terraform/main.tf":   "resource",
		".sprout/run.log":      "run",
		".ledit/revisions.md":  "rev",
		".agent-i/session.md":  "agent",
	}
	for rel, content := range ignoredFiles {
		create(rel, content)
	}

	return root
}

// TestWalkCodeFiles collects only code-extension files and prunes skipDirs.
func TestWalkCodeFiles(t *testing.T) {
	root := setupWorkspaceTree(t)
	ctx := context.Background()

	files, err := embedding.WalkCodeFiles(ctx, root)
	if err != nil {
		t.Fatalf("WalkCodeFiles error: %v", err)
	}

	// Normalize paths for consistent assertion (relative to root).
	rels := make([]string, 0, len(files))
	for _, f := range files {
		r, err := filepath.Rel(root, f)
		if err != nil {
			t.Fatalf("Rel(%q, %q): %v", root, f, err)
		}
		rels = append(rels, r)
	}

	// Must contain every code file we planted.
	expectedCode := []string{
		"main.go",
		"app.ts",
		"util.js",
		"component.tsx",
		"style.jsx",
		"module.mjs",
		"script.py",
		"src/lib.go",
		"src/handler.ts",
	}
	for _, e := range expectedCode {
		if !containsPath(rels, e) {
			t.Errorf("WalkCodeFiles missing expected file %q (got %v)", e, rels)
		}
	}

	// Must NOT contain any non-code indexable files.
	shouldNotHave := []string{
		"README.md", "config.yaml", "Makefile", "Dockerfile",
		".gitignore", "notes.txt", "setup.sh",
	}
	for _, bad := range shouldNotHave {
		if containsPath(rels, bad) {
			t.Errorf("WalkCodeFiles should NOT contain %q", bad)
		}
	}

	// Must NOT contain anything from ignored directories.
	ignoredPrefixes := []string{
		".git/", "node_modules/", "vendor/", "dist/",
		"build/", "__pycache__/", ".venv/", ".tox/",
		".next/", ".nuxt/", "coverage/", ".cache/",
	}
	for _, pfx := range ignoredPrefixes {
		for _, r := range rels {
			if strings.HasPrefix(r, pfx) {
				t.Errorf("WalkCodeFiles returned ignored path %q", r)
			}
		}
	}
}

// TestWalkAllIndexableFiles returns code + non-code indexable files.
func TestWalkAllIndexableFiles(t *testing.T) {
	root := setupWorkspaceTree(t)
	ctx := context.Background()

	files, err := embedding.WalkAllIndexableFiles(ctx, root)
	if err != nil {
		t.Fatalf("WalkAllIndexableFiles error: %v", err)
	}

	rels := make([]string, 0, len(files))
	for _, f := range files {
		r, err := filepath.Rel(root, f)
		if err != nil {
			t.Fatalf("Rel(%q, %q): %v", root, f, err)
		}
		rels = append(rels, r)
	}

	// Should contain ALL code files.
	expectedCode := []string{
		"main.go", "app.ts", "util.js", "component.tsx",
		"style.jsx", "module.mjs", "script.py",
		"src/lib.go", "src/handler.ts",
	}
	for _, e := range expectedCode {
		if !containsPath(rels, e) {
			t.Errorf("WalkAllIndexableFiles missing code file %q", e)
		}
	}

	// Should also contain non-code indexable files.
	expectedIndexable := []string{
		"README.md", "config.yaml", "Makefile", "Dockerfile",
		".gitignore", "notes.txt", "setup.sh",
	}
	for _, e := range expectedIndexable {
		if !containsPath(rels, e) {
			t.Errorf("WalkAllIndexableFiles missing indexable file %q", e)
		}
	}

	// Must still exclude ignored directories.
	ignoredPrefixes := []string{
		".git/", "node_modules/", "vendor/", "dist/",
		"build/", "__pycache__/", ".venv/", ".tox/",
		".next/", ".nuxt/", "coverage/", ".cache/",
	}
	for _, pfx := range ignoredPrefixes {
		for _, r := range rels {
			if strings.HasPrefix(r, pfx) {
				t.Errorf("WalkAllIndexableFiles returned ignored path %q", r)
			}
		}
	}
}

// TestWalkCodeFiles_NonexistentRoot verifies that WalkCodeFiles returns an
// empty result (not an error) when the root directory doesn't exist.
// filepath.WalkDir calls the callback with err != nil for a bad root;
// walkFiles treats this as a skip and returns whatever files it collected.
func TestWalkCodeFiles_NonexistentRoot(t *testing.T) {
	ctx := context.Background()
	files, err := embedding.WalkCodeFiles(ctx, "/nonexistent/path/that/does/not/exist")
	if err != nil {
		t.Fatalf("unexpected error for nonexistent root: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected empty result, got %d files", len(files))
	}
}

// TestWalkAllIndexableFiles_NonexistentRoot mirrors the above.
func TestWalkAllIndexableFiles_NonexistentRoot(t *testing.T) {
	ctx := context.Background()
	files, err := embedding.WalkAllIndexableFiles(ctx, "/nonexistent/path/that/does/not/exist")
	if err != nil {
		t.Fatalf("unexpected error for nonexistent root: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected empty result, got %d files", len(files))
	}
}

// TestShouldIgnorePath verifies the ignore logic for common paths.
func TestShouldIgnorePath(t *testing.T) {
	root := t.TempDir()

	cases := []struct {
		rel    string // relative to root
		want   bool
		label  string
	}{
		// Ignored directories
		{".git/HEAD", true, "git root"},
		{".git/config", true, "git config"},
		{"node_modules/foo/bar.js", true, "node_modules"},
		{"node_modules/pkg/index.js", true, "node_modules index"},
		{"vendor/std/lib.go", true, "vendor"},
		{"dist/bundle.js", true, "dist"},
		{"build/output.js", true, "build"},
		{"__pycache__/mod.pyc", true, "pycache"},
		{".venv/lib/dep.py", true, "venv"},
		{".tox/test/script.sh", true, "tox"},
		{".next/build.js", true, "next"},
		{".nuxt/index.ts", true, "nuxt"},
		{"coverage/report.js", true, "coverage"},
		{".cache/data.go", true, "cache"},
		{".idea/workspace.xml", true, "idea"},
		{".vscode/settings.json", true, "vscode"},
		{".terraform/main.tf", true, "terraform"},
		{".sprout/run.log", true, "sprout"},

		// Lock files and minified files
		{"package-lock.json", true, "package-lock.json"},
		{"yarn.lock", true, "yarn.lock"},
		{"pnpm-lock.yaml", true, "pnpm-lock.yaml"},
		{"go.sum", true, "go.sum"},
		{"lib.min.js", true, "minified js"},
		{"app.min.css", true, "minified css"},
		{"bundle.js.map", true, "source map"},

		// Valid code files should NOT be ignored.
		{"src/app.go", false, "go source"},
		{"src/handler.ts", false, "typescript"},
		{"util.js", false, "javascript"},
		{"component.tsx", false, "tsx"},
		{"script.py", false, "python"},

		// Non-code indexable files should NOT be ignored.
		{"README.md", false, "markdown"},
		{"config.yaml", false, "yaml"},
		{"Makefile", false, "Makefile"},
		{"Dockerfile", false, "Dockerfile"},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			path := filepath.Join(root, c.rel)
			// Ensure the parent dir exists so ShouldIgnorePath can stat it.
			os.MkdirAll(filepath.Dir(path), 0o755)
			os.WriteFile(path, []byte("dummy"), 0o644)
			got := embedding.ShouldIgnorePath(path, root)
			if got != c.want {
				t.Errorf("ShouldIgnorePath(%q, %q) = %v, want %v", c.rel, root, got, c.want)
			}
		})
	}
}

// TestWorkspaceJSResultShape verifies that the data structure the JS
// bridge would return (files, count, root) is shaped correctly.
func TestWorkspaceJSResultShape(t *testing.T) {
	root := setupWorkspaceTree(t)
	ctx := context.Background()

	files, err := embedding.WalkCodeFiles(ctx, root)
	if err != nil {
		t.Fatalf("WalkCodeFiles error: %v", err)
	}

	// Simulate what the JS bridge constructs as its return value.
	result := map[string]interface{}{
		"files": files,
		"count": len(files),
		"root":  root,
	}

	// Verify the shape matches the JS API contract.
	if result["root"] != root {
		t.Errorf("result.root = %q, want %q", result["root"], root)
	}
	count, ok := result["count"].(int)
	if !ok {
		t.Fatal("result.count is not int")
	}
	if count != len(files) {
		t.Errorf("result.count = %d, want %d", count, len(files))
	}
	fileList, ok := result["files"].([]string)
	if !ok {
		t.Fatal("result.files is not []string")
	}
	if len(fileList) != count {
		t.Errorf("result.files length %d != result.count %d", len(fileList), count)
	}

	// Same check for WalkAllIndexableFiles
	allFiles, err := embedding.WalkAllIndexableFiles(ctx, root)
	if err != nil {
		t.Fatalf("WalkAllIndexableFiles error: %v", err)
	}
	allResult := map[string]interface{}{
		"files": allFiles,
		"count": len(allFiles),
		"root":  root,
	}

	if allResult["root"] != root {
		t.Errorf("allResult.root = %q, want %q", allResult["root"], root)
	}
	allCount, ok := allResult["count"].(int)
	if !ok {
		t.Fatal("allResult.count is not int")
	}
	if allCount != len(allFiles) {
		t.Errorf("allResult.count = %d, want %d", allCount, len(allFiles))
	}

	// WalkAllIndexableFiles should always return >= WalkCodeFiles
	if allCount < count {
		t.Errorf("WalkAllIndexableFiles returned fewer files than WalkCodeFiles (%d < %d)", allCount, count)
	}
}

// containsPath checks whether a relative path string exists in the given slice.
func containsPath(paths []string, target string) bool {
	for _, p := range paths {
		if p == target {
			return true
		}
	}
	return false
}
