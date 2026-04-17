package filediscovery

import (
	"os"
	"path/filepath"
	"testing"
)

// makeTree creates a temporary directory tree for testing and returns the root.
func makeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("makeTree mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("makeTree write: %v", err)
		}
	}
	return root
}

// --- GetIgnoreRules tests ---

func TestGetIgnoreRules_NoFiles(t *testing.T) {
	root := t.TempDir()
	got := GetIgnoreRules(root)
	if got != nil {
		t.Errorf("expected nil for empty dir, got %v", got)
	}
}

func TestGetIgnoreRules_Gitignore(t *testing.T) {
	root := makeTree(t, map[string]string{
		".gitignore": "*.log\nbuild/\n",
	})
	rules := GetIgnoreRules(root)
	if rules == nil {
		t.Fatal("expected non-nil rules")
	}
	if !rules.MatchesPath("app.log") {
		t.Error("expected app.log to be ignored")
	}
	if !rules.MatchesPath("build/output.js") {
		t.Error("expected build/output.js to be ignored")
	}
	if rules.MatchesPath("main.go") {
		t.Error("expected main.go NOT to be ignored")
	}
}

func TestGetIgnoreRules_LeditIgnore(t *testing.T) {
	root := makeTree(t, map[string]string{
		".ledit/.ignore": "secret.txt\n",
	})
	rules := GetIgnoreRules(root)
	if rules == nil {
		t.Fatal("expected non-nil rules")
	}
	if !rules.MatchesPath("secret.txt") {
		t.Error("expected secret.txt to be ignored via .ledit/.ignore")
	}
}

func TestGetIgnoreRules_BothFiles(t *testing.T) {
	root := makeTree(t, map[string]string{
		".gitignore":     "*.log\n",
		".ledit/.ignore": "dist/\n",
	})
	rules := GetIgnoreRules(root)
	if rules == nil {
		t.Fatal("expected non-nil rules")
	}
	if !rules.MatchesPath("app.log") {
		t.Error("expected app.log to be ignored (gitignore)")
	}
	if !rules.MatchesPath("dist/bundle.js") {
		t.Error("expected dist/bundle.js to be ignored (ledit ignore)")
	}
	if rules.MatchesPath("main.go") {
		t.Error("expected main.go NOT to be ignored")
	}
}

// --- discoverBasic tests (via DiscoverFilesRobust with UseShell=false) ---

func newFD() *FileDiscovery {
	return &FileDiscovery{}
}

func TestDiscoverBasic_ReturnsFiles(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go":       "package main",
		"util.go":       "package main",
		"README.md":     "hello",
		".hidden":       "secret",
		"sub/handler.go": "package sub",
	})

	fd := newFD()
	result := fd.discoverBasic(&DiscoveryOptions{
		MaxFiles: 100,
		RootPath: root,
	})

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if len(result.Files) == 0 {
		t.Fatal("expected at least one file")
	}
	// Hidden files should be excluded by default
	for _, f := range result.Files {
		if filepath.Base(f) == ".hidden" {
			t.Errorf("hidden file should not be returned: %s", f)
		}
	}
}

func TestDiscoverBasic_IncludeExtFilter(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go":   "package main",
		"style.css": "body {}",
		"README.md": "hello",
	})

	fd := newFD()
	result := fd.discoverBasic(&DiscoveryOptions{
		MaxFiles:    100,
		RootPath:    root,
		IncludeExts: []string{".go"},
	})

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	for _, f := range result.Files {
		if filepath.Ext(f) != ".go" {
			t.Errorf("expected only .go files, got %s", f)
		}
	}
}

func TestDiscoverBasic_ExcludeExtFilter(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go":   "package main",
		"style.css": "body {}",
	})

	fd := newFD()
	result := fd.discoverBasic(&DiscoveryOptions{
		MaxFiles:    100,
		RootPath:    root,
		ExcludeExts: []string{".css"},
	})

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	for _, f := range result.Files {
		if filepath.Ext(f) == ".css" {
			t.Errorf("excluded .css file appeared: %s", f)
		}
	}
}

func TestDiscoverBasic_ExcludeDirs(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go":            "package main",
		"node_modules/lib.js": "exports = {}",
	})

	fd := newFD()
	result := fd.discoverBasic(&DiscoveryOptions{
		MaxFiles:    100,
		RootPath:    root,
		ExcludeDirs: []string{"node_modules"},
	})

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	for _, f := range result.Files {
		if containsPathSegment(filepath.ToSlash(f), "node_modules") {
			t.Errorf("node_modules file should be excluded: %s", f)
		}
	}
}

func TestDiscoverBasic_MaxFiles(t *testing.T) {
	files := map[string]string{}
	for i := 0; i < 20; i++ {
		files[filepath.Join("src", filepath.FromSlash("f"+string(rune('a'+i))+".go"))] = "package src"
	}
	root := makeTree(t, files)

	fd := newFD()
	result := fd.discoverBasic(&DiscoveryOptions{
		MaxFiles: 5,
		RootPath: root,
	})

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if len(result.Files) > 5 {
		t.Errorf("expected at most 5 files, got %d", len(result.Files))
	}
}

func isUnderRoot(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && !filepath.IsAbs(rel)
}

func containsPathSegment(path, segment string) bool {
	start := 0
	slashed := filepath.ToSlash(path)
	for i := 0; i <= len(slashed); i++ {
		if i == len(slashed) || slashed[i] == '/' {
			if slashed[start:i] == segment {
				return true
			}
			start = i + 1
		}
	}
	return false
}

// --- DiscoverFilesRobust smoke test (UseShell=false to avoid shell deps) ---

func TestDiscoverFilesRobust_Basic(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go": "package main",
		"util.go": "package main",
	})

	fd := newFD()
	result := fd.DiscoverFilesRobust("main function", &DiscoveryOptions{
		MaxFiles:   10,
		UseSymbols: false,
		UseShell:   false,
		RootPath:   root,
	})

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if len(result.Files) == 0 {
		t.Fatal("expected at least one file")
	}
	if result.Method != "basic" {
		t.Errorf("expected method 'basic', got %q", result.Method)
	}
}

func TestDiscoverFilesRobust_DefaultOptions(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go": "package main",
	})

	// Change to the temp dir so RootPath="" resolves to it
	old, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Skip("cannot chdir to temp dir")
	}
	defer os.Chdir(old)

	fd := newFD()
	result := fd.DiscoverFilesRobust("something", nil)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}
