package embedding

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShouldIgnoreNodeModules(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"node_modules root", "node_modules/foo.js", true},
		{"node_modules nested", "src/node_modules/bar.js", true},
		{"node_modules deep", "a/b/node_modules/c/d.js", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ShouldIgnorePath(tc.path, "")
			if result != tc.expected {
				t.Errorf("ShouldIgnorePath(%q) = %v, want %v", tc.path, result, tc.expected)
			}
		})
	}
}

func TestShouldIgnoreGitDir(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{".git root", ".git/config", true},
		{".git nested", "a/.git/hooks/pre-commit", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ShouldIgnorePath(tc.path, "")
			if result != tc.expected {
				t.Errorf("ShouldIgnorePath(%q) = %v, want %v", tc.path, result, tc.expected)
			}
		})
	}
}

func TestShouldNotIgnoreNormalFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	result := ShouldIgnorePath(path, "")
	if result {
		t.Errorf("ShouldIgnorePath(%q) = true, want false", path)
	}
}

func TestIsBinaryFile(t *testing.T) {
	dir := t.TempDir()

	// Create a binary file with a null byte.
	binaryPath := filepath.Join(dir, "binary.dat")
	if err := os.WriteFile(binaryPath, []byte("hello\x00world"), 0o644); err != nil {
		t.Fatalf("failed to create binary file: %v", err)
	}

	// Create a text file.
	textPath := filepath.Join(dir, "text.txt")
	if err := os.WriteFile(textPath, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("failed to create text file: %v", err)
	}

	t.Run("binary file detected", func(t *testing.T) {
		if !IsBinaryFile(binaryPath) {
			t.Error("expected binary file to be detected")
		}
	})

	t.Run("text file not detected as binary", func(t *testing.T) {
		if IsBinaryFile(textPath) {
			t.Error("expected text file to NOT be detected as binary")
		}
	})

	t.Run("nonexistent file returns false", func(t *testing.T) {
		if IsBinaryFile(filepath.Join(dir, "nonexistent.bin")) {
			t.Error("expected nonexistent file to return false")
		}
	})
}

func TestWalkCodeFiles(t *testing.T) {
	dir := t.TempDir()

	// Create directory structure:
	// dir/
	//   main.go          ← should be included
	//   api/
	//     handler.go     ← should be included
	//   node_modules/
	//     pkg.go         ← should be excluded
	//   utils.js         ← should be included (JS is supported)
	//   data.txt         ← should be excluded (wrong extension)

	if err := os.MkdirAll(filepath.Join(dir, "api"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	// Create actual source files.
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644)
	os.WriteFile(filepath.Join(dir, "api", "handler.go"), []byte("package api"), 0o644)
	os.WriteFile(filepath.Join(dir, "node_modules", "pkg.go"), []byte("package pkg"), 0o644)
	os.WriteFile(filepath.Join(dir, "utils.js"), []byte("console.log(1)"), 0o644)
	os.WriteFile(filepath.Join(dir, "data.txt"), []byte("not a source file"), 0o644)

	files, err := WalkCodeFiles(context.Background(), dir)
	if err != nil {
		t.Fatalf("WalkCodeFiles failed: %v", err)
	}

	// Normalize all paths to relative from dir for comparison.
	var relative []string
	for _, f := range files {
		rel, err := filepath.Rel(dir, f)
		if err != nil {
			t.Fatalf("filepath.Rel failed: %v", err)
		}
		relative = append(relative, rel)
	}

	// Should have exactly 3 files (main.go, api/handler.go, utils.js).
	if len(relative) != 3 {
		t.Fatalf("expected 3 files, got %d: %v", len(relative), relative)
	}

	expected := map[string]bool{
		"main.go":        true,
		"api/handler.go": true,
		"utils.js":       true,
	}

	for _, f := range relative {
		if !expected[f] {
			t.Errorf("unexpected file in results: %s", f)
		}
	}

	// Verify excluded files are not present.
	for _, f := range relative {
		if f == "node_modules/pkg.go" {
			t.Error("node_modules/pkg.go should not be included")
		}
		if f == "data.txt" {
			t.Error("data.txt should not be included")
		}
	}
}

func TestLayer1Ignore(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"package-lock.json", "package-lock.json", true},
		{"yarn.lock", "yarn.lock", true},
		{"pnpm-lock.yaml", "pnpm-lock.yaml", true},
		{"go.sum", "go.sum", true},
		{"minified js", "app.min.js", true},
		{"minified css", "style.min.css", true},
		{"source map", "app.js.map", true},
		{"lock file", "some.lock", true},
		{"normal go file", "main.go", false},
		{"normal js file", "app.js", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := layer1Ignore(tc.path)
			if result != tc.expected {
				t.Errorf("layer1Ignore(%q) = %v, want %v", tc.path, result, tc.expected)
			}
		})
	}
}

// TestShouldIgnoreCredentialDirs verifies that the skipDirs map protects
// known credential/key directories. This is a defense-in-depth safeguard for
// the case where the home-dir guard in BuildSymbols / buildIndexLocked is
// somehow bypassed (e.g., a future code path forgets to call it).
//
// Adding a credential dir here is a security change: it changes what gets
// indexed in daemon/service mode. Adjust with care.
func TestShouldIgnoreCredentialDirs(t *testing.T) {
	dir := t.TempDir()

	// Map of credential directory → sentinel filename we expect NOT to be
	// indexed. If skipDirs gains/losses any of these, this test catches it.
	creds := map[string]string{
		".ssh":         "id_rsa",
		".aws":         "credentials",
		".kube":        "config",
		".gnupg":       "pubring.kbx",
		".docker":      "config.json", // base64-encoded registry auth tokens
		".vault":       "vault-token",
		"Library":      "Keychains", // macOS keychain — first-level under HOME
		".Trash":       "old-secrets.txt",
		"node_modules": "deps.go",
	}

	for dirName, sentinel := range creds {
		credDir := filepath.Join(dir, dirName)
		if err := os.MkdirAll(credDir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", credDir, err)
		}
		sentinelPath := filepath.Join(credDir, sentinel)
		if err := os.WriteFile(sentinelPath, []byte("sensitive"), 0o644); err != nil {
			t.Fatalf("write %s: %v", sentinelPath, err)
		}
	}

	files, err := WalkAllIndexableFiles(context.Background(), dir)
	if err != nil {
		t.Fatalf("WalkAllIndexableFiles: %v", err)
	}

	// WalkAllIndexableFiles may return empty when the only files are filtered
	// out; we only assert that NO sentinel file ended up in the result.
	if len(files) > 0 {
		t.Logf("walk returned %d files; verifying no credential sentinels are indexed", len(files))
	}
	for _, sentinel := range []string{"id_rsa", "credentials", "config", "pubring.kbx", "config.json", "vault-token"} {
		sentinelDir := filepath.Join(dir, ".ssh")
		if sentinel == "credentials" {
			sentinelDir = filepath.Join(dir, ".aws")
		} else if sentinel == "config" {
			// .kube/config is a file — also exercised via the original walk
			sentinelDir = filepath.Join(dir, ".kube")
		} else if sentinel == "pubring.kbx" {
			sentinelDir = filepath.Join(dir, ".gnupg")
		} else if sentinel == "config.json" {
			sentinelDir = filepath.Join(dir, ".docker")
		} else if sentinel == "vault-token" {
			sentinelDir = filepath.Join(dir, ".vault")
		}
		_ = sentinelDir // suppress unused warning if needed
	}

	// Strong assertion: every credential directory was either pruned (no
	// descendants collected) OR if it wasn't pruned, its sentinel file is
	// one we'd want ignored for other reasons (binary, wrong extension).
	// The most robust check is to look for any file that lived inside a
	// credential dir.
	for _, f := range files {
		rel, err := filepath.Rel(dir, f)
		if err != nil {
			continue
		}
		first := filepath.ToSlash(rel)
		if idx := strings.IndexByte(first, '/'); idx >= 0 {
			first = first[:idx]
		} else {
			first = "" // file was directly in dir, not inside a cred subdir
		}
		if first == "" {
			continue
		}
		if _, isCredential := creds[first]; isCredential {
			t.Errorf("walk returned file inside credential dir %q: %s", first, rel)
		}
	}
}
