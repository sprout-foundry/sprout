//go:build !js

package webui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// IsProjectDirectory
// ---------------------------------------------------------------------------

func TestIsProjectDirectory_GitDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	isProject, markers := IsProjectDirectory(dir)
	if !isProject {
		t.Error("expected directory with .git to be detected as project")
	}
	if len(markers) != 1 || markers[0] != ".git" {
		t.Errorf("markers = %v, want [\".git\"]", markers)
	}
}

func TestIsProjectDirectory_GoMod(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/foo\n"), 0644); err != nil {
		t.Fatal(err)
	}

	isProject, markers := IsProjectDirectory(dir)
	if !isProject {
		t.Error("expected directory with go.mod to be detected as project")
	}
	if len(markers) != 1 || markers[0] != "go.mod" {
		t.Errorf("markers = %v, want [\"go.mod\"]", markers)
	}
}

func TestIsProjectDirectory_PackageJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	isProject, markers := IsProjectDirectory(dir)
	if !isProject {
		t.Error("expected directory with package.json to be detected as project")
	}
	if len(markers) != 1 || markers[0] != "package.json" {
		t.Errorf("markers = %v, want [\"package.json\"]", markers)
	}
}

func TestIsProjectDirectory_NoMarkers(t *testing.T) {
	dir := t.TempDir()

	isProject, markers := IsProjectDirectory(dir)
	if isProject {
		t.Error("expected empty directory to NOT be detected as project")
	}
	if markers != nil {
		t.Errorf("markers = %v, want nil", markers)
	}
}

func TestIsProjectDirectory_WeakMarkersOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	isProject, markers := IsProjectDirectory(dir)
	if isProject {
		t.Error("expected directory with only README.md (weight 30) to NOT be detected as project")
	}
	if len(markers) != 1 || markers[0] != "README.md" {
		t.Errorf("markers = %v, want [\"README.md\"]", markers)
	}
}

func TestIsProjectDirectory_TwoWeakMarkers(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".vscode"), 0755); err != nil {
		t.Fatal(err)
	}

	isProject, markers := IsProjectDirectory(dir)
	if !isProject {
		t.Error("expected directory with README.md + .vscode to be detected as project (two markers with weight >= 30)")
	}
	if len(markers) != 2 {
		t.Errorf("markers = %v, want 2 markers", markers)
	}
}

// ---------------------------------------------------------------------------
// FindNearestProjectRoot
// ---------------------------------------------------------------------------

func TestFindNearestProjectRoot_InProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	root, markers := FindNearestProjectRoot(dir)
	if root == "" {
		t.Fatal("expected to find project root")
	}
	if root != dir {
		t.Errorf("root = %q, want %q", root, dir)
	}
	if len(markers) != 1 || markers[0] != ".git" {
		t.Errorf("markers = %v, want [\".git\"]", markers)
	}
}

func TestFindNearestProjectRoot_Subdirectory(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	subDir := filepath.Join(projectDir, "src", "inner")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	root, _ := FindNearestProjectRoot(subDir)
	if root == "" {
		t.Fatal("expected to find project root from subdirectory")
	}
	if root != projectDir {
		t.Errorf("root = %q, want %q", root, projectDir)
	}
}

func TestFindNearestProjectRoot_NoProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "some_dir"), 0755); err != nil {
		t.Fatal(err)
	}

	// The function walks all the way to the filesystem root, so it can
	// legitimately discover project markers in an ancestor of our temp dir
	// (e.g. /tmp/package.json in some CI environments). The invariant we
	// actually care about is that no directory *inside* our test fixture
	// gets claimed as a project root, since we put no markers in any of them.
	startDir := filepath.Join(dir, "some_dir")
	root, _ := FindNearestProjectRoot(startDir)
	if root == "" {
		return // ideal case: ancestors are also clean
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("filepath.Abs(%q): %v", root, err)
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("filepath.Abs(%q): %v", dir, err)
	}
	if strings.HasPrefix(abs, absDir+string(filepath.Separator)) || abs == absDir {
		t.Errorf("root = %q is inside test fixture %q; expected the walk to find markers only outside the fixture or not at all", root, dir)
	}
}

// ---------------------------------------------------------------------------
// FindProjectsInDirectory
// ---------------------------------------------------------------------------

func TestFindProjectsInDirectory(t *testing.T) {
	base := t.TempDir()

	// Create two project subdirs
	for _, name := range []string{"proj-a", "proj-b"} {
		pDir := filepath.Join(base, name)
		if err := os.MkdirAll(pDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pDir, "go.mod"), []byte("module "+name+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Create a non-project subdir
	plain := filepath.Join(base, "not-a-project")
	if err := os.MkdirAll(plain, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plain, "notes.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	results := FindProjectsInDirectory(base, 1)
	if len(results) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(results))
	}

	names := make(map[string]bool)
	for _, r := range results {
		names[r.Name] = true
	}
	if !names["proj-a"] || !names["proj-b"] {
		t.Errorf("expected proj-a and proj-b, got %v", names)
	}
}

func TestFindProjectsInDirectory_MaxDepth(t *testing.T) {
	base := t.TempDir()

	// Create a project nested 3 levels deep
	deepProject := filepath.Join(base, "level1", "level2", "level3-project")
	if err := os.MkdirAll(deepProject, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deepProject, "go.mod"), []byte("module deep\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// maxDepth=1 should not find the deeply nested project
	results := FindProjectsInDirectory(base, 1)
	for _, r := range results {
		if r.Name == "level3-project" {
			t.Errorf("project at depth 3 should not be found with maxDepth=1")
		}
	}

	// maxDepth=3 should find it
	results = FindProjectsInDirectory(base, 3)
	found := false
	for _, r := range results {
		if r.Name == "level3-project" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find level3-project with maxDepth=3")
	}
}

func TestFindProjectsInDirectory_HiddenDirs(t *testing.T) {
	base := t.TempDir()

	// Create a hidden directory that looks like a project
	hiddenDir := filepath.Join(base, ".hidden-project")
	if err := os.MkdirAll(hiddenDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(hiddenDir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create a normal project directory
	normalDir := filepath.Join(base, "visible-project")
	if err := os.MkdirAll(normalDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(normalDir, "go.mod"), []byte("module visible\n"), 0644); err != nil {
		t.Fatal(err)
	}

	results := FindProjectsInDirectory(base, 1)
	for _, r := range results {
		if r.Name == ".hidden-project" {
			t.Error("hidden directory .hidden-project should be skipped")
		}
	}
	if len(results) != 1 || results[0].Name != "visible-project" {
		t.Errorf("expected only visible-project, got %d results: %v", len(results), results)
	}
}
