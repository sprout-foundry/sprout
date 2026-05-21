package embedding

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// BuildManifest tests
// =============================================================================

func TestLoadManifest_Nonexistent(t *testing.T) {
	m, err := LoadManifest("/nonexistent/path/manifest.json")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if m != nil {
		t.Fatalf("expected nil manifest, got %+v", m)
	}
}

func TestLoadManifest_ReadError(t *testing.T) {
	dir := t.TempDir()
	// Create a file that cannot be unmarshalled as JSON.
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, []byte("{ invalid json"), 0o644); err != nil {
		t.Fatalf("failed to create bad manifest: %v", err)
	}

	m, err := LoadManifest(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if m != nil {
		t.Fatalf("expected nil manifest on error, got %+v", m)
	}
}

func TestSaveAndLoadManifest_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	original := &BuildManifest{
		Files: map[string]int64{
			"a.go": 1000,
			"b.go": 2000,
		},
		ModelHash: "test-hash",
	}

	if err := SaveManifest(path, original); err != nil {
		t.Fatalf("failed to save manifest: %v", err)
	}

	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("failed to load manifest: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil manifest")
	}

	if loaded.ModelHash != original.ModelHash {
		t.Errorf("model hash: got %q, want %q", loaded.ModelHash, original.ModelHash)
	}
	if len(loaded.Files) != len(original.Files) {
		t.Fatalf("file count: got %d, want %d", len(loaded.Files), len(original.Files))
	}
	for k, v := range original.Files {
		if loaded.Files[k] != v {
			t.Errorf("file %q: got %d, want %d", k, loaded.Files[k], v)
		}
	}
}

func TestSaveManifest_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "manifest.json")

	m := &BuildManifest{
		Files:     map[string]int64{"a.go": 1},
		ModelHash: "h",
	}

	if err := SaveManifest(path, m); err != nil {
		t.Fatalf("failed to save manifest: %v", err)
	}

	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}
	if loaded == nil || loaded.ModelHash != "h" {
		t.Fatalf("round-trip failed: loaded=%v", loaded)
	}
}

// =============================================================================
// CheckFileChanged tests
// =============================================================================

func TestCheckFileChanged_Unchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	content := []byte("package main\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}
	mtime := info.ModTime().UnixNano()

	changed, err := CheckFileChanged(path, mtime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Error("expected file to be unchanged")
	}
}

func TestCheckFileChanged_Changed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	content := []byte("package main\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	// Wait a bit to ensure mtime changes
	time.Sleep(10 * time.Millisecond)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}
	currentMtime := info.ModTime().UnixNano()

	// Use a different mtime
	changed, err := CheckFileChanged(path, currentMtime+1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Error("expected file to be changed")
	}
}

func TestCheckFileChanged_DoesNotExist(t *testing.T) {
	changed, err := CheckFileChanged("/nonexistent/file.go", 12345)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Error("expected non-existent file to NOT be reported as changed")
	}
}

// =============================================================================
// DiffManifest tests
// =============================================================================

func TestDiffManifest_NoManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	diff, err := DiffManifest(context.Background(), nil, "hash", dir, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}
	if len(diff.ChangedFiles) != 1 {
		t.Errorf("expected 1 changed file, got %d", len(diff.ChangedFiles))
	}
	if len(diff.UnchangedFiles) != 0 {
		t.Errorf("expected 0 unchanged files, got %d", len(diff.UnchangedFiles))
	}
	if diff.ManifestInvalidated {
		t.Error("expected manifest not to be invalidated on first build")
	}
}

func TestDiffManifest_ModelHashChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	manifest := &BuildManifest{
		Files:     map[string]int64{path: 1000},
		ModelHash: "old-hash",
	}

	diff, err := DiffManifest(context.Background(), manifest, "new-hash", dir, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !diff.ManifestInvalidated {
		t.Error("expected manifest to be invalidated when model hash changes")
	}
}

func TestDiffManifest_UnchangedFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}
	mtime := info.ModTime().UnixNano()

	manifest := &BuildManifest{
		Files:     map[string]int64{path: mtime},
		ModelHash: "same-hash",
	}

	diff, err := DiffManifest(context.Background(), manifest, "same-hash", dir, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(diff.UnchangedFiles) != 1 {
		t.Errorf("expected 1 unchanged file, got %d", len(diff.UnchangedFiles))
	}
	if len(diff.ChangedFiles) != 0 {
		t.Errorf("expected 0 changed files, got %d", len(diff.ChangedFiles))
	}
	if len(diff.DeletedFiles) != 0 {
		t.Errorf("expected 0 deleted files, got %d", len(diff.DeletedFiles))
	}
}

func TestDiffManifest_DeletedFiles(t *testing.T) {
	dir := t.TempDir()

	manifest := &BuildManifest{
		Files: map[string]int64{
			filepath.Join(dir, "test.go"):      1000,
			filepath.Join(dir, "deleted.go"):   2000,
			filepath.Join(dir, "another.go"):   3000,
		},
		ModelHash: "same-hash",
	}

	// Only test.go exists
	testPath := filepath.Join(dir, "test.go")
	if err := os.WriteFile(testPath, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	info, err := os.Stat(testPath)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}
	manifest.Files[testPath] = info.ModTime().UnixNano() // match mtime

	diff, err := DiffManifest(context.Background(), manifest, "same-hash", dir, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(diff.DeletedFiles) != 2 {
		t.Errorf("expected 2 deleted files, got %d", len(diff.DeletedFiles))
	}
}

func TestDiffManifest_ChangedFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	manifest := &BuildManifest{
		Files:     map[string]int64{path: 0}, // wrong mtime
		ModelHash: "same-hash",
	}

	diff, err := DiffManifest(context.Background(), manifest, "same-hash", dir, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(diff.ChangedFiles) != 1 {
		t.Errorf("expected 1 changed file, got %d", len(diff.ChangedFiles))
	}
}

// =============================================================================
// BuildManifestFromFiles tests
// =============================================================================

func TestBuildManifestFromFiles(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "a.go")
	f2 := filepath.Join(dir, "b.go")
	for _, f := range []string{f1, f2} {
		if err := os.WriteFile(f, []byte("package main\n"), 0o644); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
	}

	manifest := BuildManifestFromFiles([]string{f1, f2}, "test-hash")

	if manifest.ModelHash != "test-hash" {
		t.Errorf("model hash: got %q, want %q", manifest.ModelHash, "test-hash")
	}
	if len(manifest.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(manifest.Files))
	}
	for _, f := range []string{f1, f2} {
		if mtime := manifest.Files[f]; mtime == 0 {
			t.Errorf("expected non-zero mtime for %s", f)
		}
	}
}

// =============================================================================
// BuildIndex with manifest integration tests
// =============================================================================

func TestBuildIndex_WithManifest(t *testing.T) {
	dir := t.TempDir()

	// Create two Go files
	for _, name := range []string{"a", "b"} {
		src := fmt.Sprintf(`package main

// Func%s does something.
func Func%s() string {
	return "%s"
}
`, name, strings.ToUpper(name), name)
		if err := os.WriteFile(filepath.Join(dir, name+".go"), []byte(src), 0o644); err != nil {
			t.Fatalf("failed to create file %s: %v", name, err)
		}
	}

	provider := newMockProvider(3)
	store, err := NewJSONLFileStore(filepath.Join(dir, "index.jsonl"), "mock-hash")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	manifestPath := filepath.Join(dir, ".index.jsonl.manifest.json")
	opts := IndexOptions{
		BatchSize:      16,
		MaxBodyLen:     500,
		ManifestPath:   manifestPath,
		IndexFileLevel: false,
	}
	idx := NewIndexManager(provider, store, opts)
	ctx := context.Background()

	// First build: no manifest, should parse all files.
	stats1, err := idx.BuildIndex(ctx, dir)
	if err != nil {
		t.Fatalf("first BuildIndex failed: %v", err)
	}
	if stats1.FilesProcessed != 2 {
		t.Errorf("first build: expected 2 files processed, got %d", stats1.FilesProcessed)
	}
	if stats1.UnitsEmbedded < 2 {
		t.Errorf("first build: expected at least 2 units embedded, got %d", stats1.UnitsEmbedded)
	}

	// Verify manifest was saved.
	m, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("failed to load manifest after first build: %v", err)
	}
	if m == nil {
		t.Fatal("manifest should exist after first build")
	}
	if len(m.Files) != 2 {
		t.Errorf("manifest should have 2 files, got %d", len(m.Files))
	}
	if m.ModelHash != "mock-model-hash" {
		t.Errorf("manifest model hash: got %q, want %q", m.ModelHash, "mock-model-hash")
	}

	// Second build: manifest exists, nothing changed — should skip parsing.
	time.Sleep(10 * time.Millisecond) // ensure current time is different from file mtimes
	stats2, err := idx.BuildIndex(ctx, dir)
	if err != nil {
		t.Fatalf("second BuildIndex failed: %v", err)
	}
	if stats2.FilesProcessed != 0 {
		t.Errorf("second build: expected 0 files processed (all unchanged, skipped by manifest), got %d", stats2.FilesProcessed)
	}
	if stats2.UnitsEmbedded != 0 {
		t.Errorf("second build: expected 0 units embedded (no changes), got %d", stats2.UnitsEmbedded)
	}

	// Store should still have the same number of records.
	initialSize := store.Size()
	if initialSize == 0 {
		t.Fatal("store should have records after first build")
	}
}

func TestBuildIndex_ManifestModelHashChange(t *testing.T) {
	dir := t.TempDir()

	src := `package main

func Hello() string {
	return "world"
}
`
	if err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	provider := newMockProvider(3)
	store, err := NewJSONLFileStore(filepath.Join(dir, "index.jsonl"), "mock-hash")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	manifestPath := filepath.Join(dir, ".index.jsonl.manifest.json")
	opts := IndexOptions{
		BatchSize:      16,
		MaxBodyLen:     500,
		ManifestPath:   manifestPath,
		IndexFileLevel: false,
	}
	idx := NewIndexManager(provider, store, opts)
	ctx := context.Background()

	// First build.
	_, err = idx.BuildIndex(ctx, dir)
	if err != nil {
		t.Fatalf("first BuildIndex failed: %v", err)
	}

	// Simulate model hash change by writing a manifest with a different hash.
	m, err := LoadManifest(manifestPath)
	if err != nil || m == nil {
		t.Fatal("manifest should exist after first build")
	}
	m.ModelHash = "old-hash"
	if err := SaveManifest(manifestPath, m); err != nil {
		t.Fatalf("failed to rewrite manifest: %v", err)
	}

	// Second build should re-embed everything (model hash mismatch).
	stats, err := idx.BuildIndex(ctx, dir)
	if err != nil {
		t.Fatalf("second BuildIndex failed: %v", err)
	}
	// After model hash change, all files should be re-embedded.
	if stats.UnitsEmbedded == 0 {
		t.Error("expected units to be re-embedded after model hash change")
	}
}
